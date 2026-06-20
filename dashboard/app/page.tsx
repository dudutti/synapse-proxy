"use client";

import { useSession, signOut } from "next-auth/react";
import { useRouter } from "next/navigation";
import { useEffect, useState, useRef } from "react";
import useSWR from "swr";
import Link from "next/link";
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend, PieChart, Pie, Cell, ComposedChart, Area } from "recharts";
import { motion, useMotionValue, useTransform, animate, AnimatePresence } from "framer-motion";
import { LogOut, Settings, Activity, Sparkles, Database, Clock, Info, PlayCircle, Square, X } from "lucide-react";
import ParticleBackground from "@/components/ParticleBackground";
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import { LiveTelemetryGrouped, LiveRequest } from "@/components/LiveTelemetryGrouped";
import HeaderNav from "@/components/HeaderNav";

const formatJSON = (payload: string) => {
  if (!payload) return "";
  try {
    return JSON.stringify(JSON.parse(payload), null, 2);
  } catch {
    return payload;
  }
};

/**
 * AnimatedNumber: smooth tween between the previous value and the new one.
 * Uses Framer Motion's `useMotionValue` + `animate` so the number rolls up
 * smoothly even when several SSE events arrive in quick succession.
 */
function AnimatedNumber({
  value,
  format = (v: number) => v.toLocaleString(),
  duration = 0.8,
}: {
  value: number;
  format?: (v: number) => string;
  duration?: number;
}) {
  const mv = useMotionValue(value);
  const display = useTransform(mv, (latest) => format(latest));
  useEffect(() => {
    const controls = animate(mv, value, { duration, ease: "easeOut" });
    return () => controls.stop();
  }, [value, duration, mv]);
  return <motion.span>{display}</motion.span>;
}

/**
 * LiveIndicator: a small pulsing dot that flashes every time the SSE
 * pushes a new telemetry event. Pure CSS animation, no state.
 */
function LiveIndicator({ active }: { active: boolean }) {
  return (
    <span className="relative inline-flex h-2 w-2">
      <AnimatePresence>
        {active && (
          <motion.span
            key="ping"
            initial={{ scale: 1, opacity: 0.75 }}
            animate={{ scale: 2.4, opacity: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.8, ease: "easeOut" }}
            className="absolute inline-flex h-full w-full rounded-full bg-emerald-400"
          />
        )}
      </AnimatePresence>
      <span className={`relative inline-flex h-2 w-2 rounded-full ${active ? "bg-emerald-400" : "bg-zinc-500"}`} />
    </span>
  );
}

export default function Dashboard() {
  const { data: session, status } = useSession();
  const router = useRouter();
  const [data, setData] = useState<any[]>([]);
  const [logs, setLogs] = useState<any[]>([]);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [daysFilter, setDaysFilter] = useState<number>(0);
  const [totals, setTotals] = useState({
    sent: { total: 0, input: 0, output: 0 },
    optimized: { total: 0, input: 0, output: 0 }
  });
  // Auto-refetch token - bumped on every poll cycle to force
  // AnimatedNumber to re-render smoothly without flicker.
  const [pollTick, setPollTick] = useState(0);
  // Hold the active EventSource so we can close it on filter change.
  const eventSourceRef = useRef<EventSource | null>(null);
  // Track which RequestLog IDs we have already pushed to the log table
  // (avoids double-counting when SSE delivers the same event twice on
  // reconnect or rapid filter change).
  const seenLogIds = useRef<Set<string>>(new Set());
  const [cacheDist, setCacheDist] = useState({ MISS: 0, L1: 0, L2: 0, L3: 0 });
  const [cacheByProvider, setCacheByProvider] = useState<Record<string, { creation: number; read: number; hit: number; miss: number; count: number }>>({});
  const [cacheHitRateByProvider, setCacheHitRateByProvider] = useState<Record<string, number>>({});
  const [measuredSavings, setMeasuredSavings] = useState<{ l1L2Hits: number; l3Compressions: number }>({ l1L2Hits: 0, l3Compressions: 0 });
  const [opportunitySavings, setOpportunitySavings] = useState<{ highCacheReadProviders: string[] }>({ highCacheReadProviders: [] });

  // Per-class savings breakdown (4 classes: inputFresh, cacheRead, cacheCreation, output)
  type SavingsByClass = { inputFresh: number; cacheRead: number; cacheCreation: number; output: number };
  const [totalSavingsByClass, setTotalSavingsByClass] = useState<SavingsByClass>({ inputFresh: 0, cacheRead: 0, cacheCreation: 0, output: 0 });
  const [savingsByClassByProvider, setSavingsByClassByProvider] = useState<Record<string, SavingsByClass>>({});
  const [totalSavingsReal, setTotalSavingsReal] = useState(0);

  const [isRecording, setIsRecording] = useState(false);
  const [sessionStartTime, setSessionStartTime] = useState<number | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [sessionResult, setSessionResult] = useState<any>(null);

  // New states for Modal
  const [showConfigModal, setShowConfigModal] = useState(false);
  const [keysToRestore, setKeysToRestore] = useState<string[]>([]);

  // States for Key Filter and Diff Modal
  const [apiKeys, setApiKeys] = useState<any[]>([]);
  const [selectedKey, setSelectedKey] = useState<string>("");
  const [diffLog, setDiffLog] = useState<any>(null);

  const handleRecordToggle = async () => {
    if (!isRecording) {
      setShowConfigModal(true);
    } else {
      setIsRecording(false);
      const end = Date.now();

      // Stop the proxy-side session tag. This removes the
      // synapse:session:vk:<vk> keys from Redis so subsequent
      // requests are no longer tagged with this session id.
      if (sessionId) {
        await fetch('/api/sessions/record', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enable: false, sessionId })
        });
      }

      // Stop Benchmark mode if it was enabled
      if (keysToRestore.length > 0) {
        await fetch('/api/keys/session-benchmark', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enable: false, keyIds: keysToRestore })
        });
        setKeysToRestore([]);
      }

      // Fetch the aggregated session stats. The apiKeyId
      // filter restricts to the current user's keys; the
      // sessionId filter restricts to the rows tagged during
      // this recording window.
      const res = await fetch(`/api/analytics/session?start=${sessionStartTime}&end=${end}${sessionId ? `&sessionId=${encodeURIComponent(sessionId)}` : ""}`);
      if (res.ok) {
        const data = await res.json();
        setSessionResult(data);
      }
    }
  };

  const startRecording = async () => {
    setShowConfigModal(false);

    // 1. Ask the dashboard API to tag the user's virtual keys
    //    with a fresh session id (writes synapse:session:vk:<vk>
    //    in Redis). The Go proxy reads this on every request and
    //    tags the resulting RequestLog row. This is the
    //    "server-side recording" path: any agent using these keys
    //    gets recorded transparently, no client changes required.
    const newSessionId = `session-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const recRes = await fetch('/api/sessions/record', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enable: true, sessionId: newSessionId })
    });
    if (!recRes.ok) {
      const err = await recRes.json().catch(() => ({}));
      alert(`Failed to start recording: ${err.error || recRes.statusText}`);
      return;
    }
    const recData = await recRes.json();
    setSessionId(recData.sessionId || newSessionId);

    // Benchmark Mode is now decoupled from the Record Session
    // toggle. If the user wants an AI-scored comparison between
    // Synapse Proxy and the original provider, they enable Benchmark
    // Mode from the top menu. It doubles the upstream token
    // spend, so it is its own deliberate toggle. Recording alone
    // is free (only the per-request token counts are stored, not
    // an extra control request).

    setIsRecording(true);
    setSessionStartTime(Date.now());
    setSessionResult(null);
  };

  useEffect(() => {
    if (status === "authenticated") {
      // /api/keys is now loaded via useSWR below; this effect is a
      // no-op kept to avoid touching the rest of the file.
    }
  }, [status]);

  // Keyboard shortcuts for the Session Summary modal. Escape
  // closes it; this is the only way to dismiss the modal when the
  // close (X) button is scrolled out of view on tall sessions
  // (e.g. a 4-turn conversation with full Recharts graphs).
  useEffect(() => {
    if (!sessionResult) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setSessionResult(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [sessionResult]);

  // SWR-backed keys fetch. The cache is shared across client-side
  // route transitions, so navigating away and back to the home page
  // serves the previous list instantly while SWR revalidates in the
  // background. The /api/keys payload is small (<10 keys per user)
  // so the cache hit is essentially free.
  const { data: swrKeys } = useSWR<any[]>(
    status === "authenticated" ? "/api/keys" : null
  );
  useEffect(() => {
    if (swrKeys) setApiKeys(swrKeys || []);
  }, [swrKeys]);

  // SWR-backed initial fetch of /api/analytics. The key includes
  // page/daysFilter/selectedKey so the cache is per-filter, and
  // SWR serves the previous response instantly on remount while
  // revalidating in the background. The SSE stream and the 5s
  // polling below continue to push updates to local state.
  //
  // IMPORTANT: when SWR revalidates (e.g. on focus, on reconnect,
  // or when the underlying data changes), we must NOT replace the
  // local `logs` state wholesale — that would wipe out the rows
  // the SSE pushed in the meantime, which the SWR response (a
  // 30s-old snapshot) doesn't yet know about. The user sees this
  // as "I clicked on a row, then everything got reclassified".
  //
  // Instead we MERGE: keep every row we already have locally
  // (tracked via seenLogIds so we don't double-count), and add
  // any new rows from the SWR response. SWR still drives the
  // chart/totals revalidation, only the row list is append-only.
  const analyticsKey = status === "authenticated"
    ? `/api/analytics?page=${page}&limit=100&days=${daysFilter}${selectedKey ? `&keyId=${selectedKey}` : ""}`
    : null;
  const { data: swrAnalytics } = useSWR<any>(analyticsKey);
  useEffect(() => {
    if (!swrAnalytics) return;
    const d = swrAnalytics;
    if (d.chartData) setData(d.chartData);
    if (d.logs && Array.isArray(d.logs)) {
      // Merge new SWR rows into existing local state without
      // dropping rows that the SSE pushed in the meantime. We
      // dedupe by id using seenLogIds so the same row from SWR
      // and SSE doesn't appear twice.
      setLogs((prev) => {
        const merged = prev.slice();
        for (const row of d.logs) {
          if (row && row.id) {
            if (!seenLogIds.current.has(row.id)) {
              seenLogIds.current.add(row.id);
              merged.unshift(row);
            }
          }
        }
        // Cap at 5000 rows so we don't grow unbounded if the SSE
        // is fast and the page is open for a long time.
        return merged.slice(0, 5000);
      });
    }
    if (d.pagination) setTotalPages(d.pagination.totalPages);
    if (d.totalTokensSent !== undefined) {
      setTotals({ sent: d.totalTokensSent, optimized: d.totalTokensOptimized });
    }
    if (d.cacheHitDistribution) setCacheDist(d.cacheHitDistribution);
    if (d.cacheByProvider) setCacheByProvider(d.cacheByProvider);
    if (d.cacheHitRateByProvider) setCacheHitRateByProvider(d.cacheHitRateByProvider);
    if (d.measuredSavings) setMeasuredSavings(d.measuredSavings);
    if (d.opportunitySavings) setOpportunitySavings(d.opportunitySavings);
    if (d.totalSavingsByClass) setTotalSavingsByClass(d.totalSavingsByClass);
    if (d.savingsByClassByProvider) setSavingsByClassByProvider(d.savingsByClassByProvider);
    if (typeof d.totalSavingsReal === 'number') setTotalSavingsReal(d.totalSavingsReal);
  }, [swrAnalytics]);

  useEffect(() => {
    if (status === "unauthenticated") {
      router.push("/login");
    } else if (status === "authenticated") {
      const keyQuery = selectedKey ? `&keyId=${selectedKey}` : "";
      // The initial /api/analytics fetch is now handled by useSWR
      // below, which keeps the response in a shared cache so that
      // navigating away and back to the home page shows the numbers
      // instantly while a revalidation runs in the background.
      // The local `setData`/`setLogs`/etc. fallbacks below the SWR
      // effect are still the source of truth: SWR just primes the
      // cache and the live updates (SSE + 5s polling) keep them in
      // sync.

      // Tear down any previous SSE before opening a new one. Without this
      // guard, a fast filter change can leave the previous connection open
      // and events leak between event sources.
      if (eventSourceRef.current) {
        try { eventSourceRef.current.close(); } catch {}
      }

      // Setup SSE Stream for live log feed (Live Telemetry table only).
      // CRITICAL: created once per filter change, deps must NOT include
      // anything that changes on every render or the connection will be
      // re-created on each setState and silently drop events.
      const eventSource = new EventSource(`/api/analytics/stream${selectedKey ? `?keyId=${selectedKey}` : ''}`);
      eventSourceRef.current = eventSource;

      eventSource.onmessage = (event) => {
        try {
          const newLog = JSON.parse(event.data);
          if (newLog.type === "connected") return;
          if (newLog.id && seenLogIds.current.has(newLog.id)) return;
          if (newLog.id) seenLogIds.current.add(newLog.id);
          setLogs(prevLogs => [newLog, ...prevLogs].slice(0, 5000));
        } catch (e) {}
      };

      // Fallback polling: refetch /api/analytics every 5s to refresh the
      // headline counters (Total Value Saved, Tokens Sent, etc.) without
      // waiting for the user to change filter. Cheap DB query, ~50ms on
      // the dashboard container. With 1000 users this is 200 req/s on
      // Postgres - fine for a service of this size.
      const pollId = window.setInterval(async () => {
        try {
          const r = await fetch(`/api/analytics?page=${page}&limit=10&days=${daysFilter}${keyQuery}`);
          if (r.ok) {
            const d = await r.json();
            if (d.chartData) setData(d.chartData);
            if (d.pagination) setTotalPages(d.pagination.totalPages);
            if (d.totalTokensSent !== undefined) {
              setTotals({ sent: d.totalTokensSent, optimized: d.totalTokensOptimized });
            }
            if (d.cacheHitDistribution) setCacheDist(d.cacheHitDistribution);
            if (d.cacheByProvider) setCacheByProvider(d.cacheByProvider);
            if (d.cacheHitRateByProvider) setCacheHitRateByProvider(d.cacheHitRateByProvider);
            if (d.measuredSavings) setMeasuredSavings(d.measuredSavings);
            if (d.opportunitySavings) setOpportunitySavings(d.opportunitySavings);
            if (d.totalSavingsByClass) setTotalSavingsByClass(d.totalSavingsByClass);
            if (typeof d.totalSavingsReal === 'number') {
              setTotalSavingsReal(d.totalSavingsReal);
            }
            setPollTick(t => t + 1);
          }
        } catch {}
      }, 5000);

      return () => {
        eventSource.close();
        eventSourceRef.current = null;
        window.clearInterval(pollId);
      };
    }
  }, [status, page, daysFilter, selectedKey]);

  if (status === "loading" || status === "unauthenticated") {
    return <div className="min-h-screen flex items-center justify-center bg-[#050505] text-white">Loading...</div>;
  }

  // Total Value Saved = somme des 4 classes (input frais + cache_read + cache_creation + output).
  // C'est le même chiffre que "Total réel" dans la section "Détail des économies" plus bas,
  // donc cohérent bout en bout. Avant on utilisait la diff costWithout-costWith du chartData
  // (qui ignorait les classes cache) â†’ d'où les $22 actuels. Avec le seed MiniMax seedé et
  // le calcul par classe, ce chiffre va baisser (plus exact, plus petit).
  const totalSaved = totalSavingsReal;

  const percentSaved = totals.sent.total > 0
    ? (((totals.sent.total - totals.optimized.total) / totals.sent.total) * 100).toFixed(1)
    : "0.0";

  const pieData = [
    { name: 'L1 Cache (exact)', value: cacheDist.L1, fill: '#3b82f6' },
    { name: 'L2 Cache (semantic)', value: cacheDist.L2, fill: '#10b981' },
    { name: 'L3 Standard (compressed)', value: cacheDist.L3, fill: '#a855f7' },
    { name: 'Standard Routing (no opt)', value: cacheDist.MISS, fill: '#334155' },
  ].filter(d => d.value > 0);

  const containerVars = {
    hidden: { opacity: 0 },
    show: {
      opacity: 1,
      transition: { staggerChildren: 0.1 }
    }
  };

  const itemVars = {
    hidden: { opacity: 0, y: 20 },
    show: { opacity: 1, y: 0, transition: { duration: 0.5, ease: "easeOut" } }
  };

  return (
    <div className="min-h-screen bg-[#050505] text-white p-8 font-sans relative overflow-hidden">
      <ParticleBackground />

      <motion.div 
        variants={containerVars}
        initial="hidden"
        animate="show"
        className="max-w-7xl mx-auto relative z-10"
      >
        <motion.header variants={itemVars} className="mb-10 flex justify-between items-center bg-white/5 border border-white/10 p-6 rounded-2xl backdrop-blur-xl shadow-2xl relative z-50">
          <div className="flex items-center gap-4">
            <div className="w-12 h-12 rounded-full bg-[#0f0f11] border border-white/10 shadow-[0_0_20px_rgba(52,211,153,0.4)] ring-1 ring-emerald-500/30 overflow-hidden flex items-center justify-center">
              {/* Translate-y moves the image physically down to center the icon */}
              <img src="/logo01.png" alt="Synapse Proxy Icon" className="w-[150%] h-[150%] object-cover max-w-none translate-y-1.5" />
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-white">Synapse Proxy <span className="text-emerald-400">Enterprise</span></h1>
              <p className="text-gray-400 text-sm">{session?.user?.email}</p>
            </div>
          </div>

          <HeaderNav />

          <div className="flex gap-3 items-center">
            <button 
              onClick={handleRecordToggle}
              className={`flex items-center gap-2 px-5 py-2.5 rounded-xl transition-all border text-sm font-bold ${
                isRecording 
                  ? "bg-red-500/10 text-red-500 border-red-500/30 animate-pulse shadow-[0_0_20px_rgba(239,68,68,0.2)]" 
                  : "bg-[#0a0a0a] text-gray-400 border-white/5 hover:border-white/10 hover:text-white hover:bg-white/5"
              }`}
            >
              {isRecording ? <><Square className="w-4 h-4 fill-current" /> Stop Recording</> : <><PlayCircle className="w-4 h-4" /> Record Session</>}
            </button>
            <Link href="/playground" className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-emerald-500 hover:bg-emerald-400 transition-all shadow-[0_0_20px_rgba(16,185,129,0.2)] hover:shadow-[0_0_30px_rgba(16,185,129,0.4)] text-sm font-bold text-black hover:scale-[1.02] active:scale-95">
              <Database className="w-4 h-4" /> Playground
            </Link>
            <Link href="/benchmark" className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-[#0a0a0a] hover:bg-white/5 transition-all border border-white/5 hover:border-white/10 text-sm font-bold text-gray-400 hover:text-white">
              <Activity className="w-4 h-4" /> Benchmark
            </Link>
            <Link href="/settings" className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-[#0a0a0a] hover:bg-white/5 transition-all border border-white/5 hover:border-white/10 text-sm font-bold text-gray-400 hover:text-white">
              <Settings className="w-4 h-4" /> Settings
            </Link>
            <button 
              onClick={() => signOut()}
              className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-[#0a0a0a] hover:bg-red-500/10 transition-all border border-white/5 hover:border-red-500/30 text-sm font-bold text-gray-500 hover:text-red-400"
            >
              <LogOut className="w-4 h-4" /> Sign Out
            </button>
          </div>
        </motion.header>

        <motion.div variants={itemVars} className="grid grid-cols-1 lg:grid-cols-3 gap-8 mb-8">
          {/* Value Saved Card */}
          <div className="lg:col-span-2 relative overflow-hidden bg-black/40 border border-white/10 rounded-3xl p-10 backdrop-blur-xl shadow-2xl flex flex-col justify-center">
            <div className="flex justify-between items-start mb-2 relative z-10">
              <h2 className="text-lg text-gray-400 uppercase tracking-widest font-bold">Total Value Saved</h2>
              <div className="flex gap-4">
                <div className="flex bg-black/50 border border-white/10 rounded-lg p-1">
                  <select 
                    className="bg-transparent text-gray-400 text-xs font-bold outline-none px-2 cursor-pointer hover:text-white"
                    value={selectedKey}
                    onChange={(e) => { setSelectedKey(e.target.value); setPage(1); }}
                  >
                    <option value="">All Keys</option>
                    {apiKeys.map(k => (
                      <option key={k.id} value={k.id} className="bg-black text-white">{k.virtualKey}</option>
                    ))}
                  </select>
                </div>
                <div className="flex bg-black/50 border border-white/10 rounded-lg p-1">
                  {[
                    { label: "24h", value: 1 },
                    { label: "7d", value: 7 },
                    { label: "30d", value: 30 },
                    { label: "All", value: 0 },
                  ].map(f => (
                    <button
                      key={f.label}
                      onClick={() => { setDaysFilter(f.value); setPage(1); }}
                      className={`px-3 py-1 text-xs font-bold rounded-md transition ${daysFilter === f.value ? 'bg-emerald-500 text-black' : 'text-gray-400 hover:text-white hover:bg-white/5'}`}
                    >
                      {f.label}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            <div className="flex items-center gap-2 text-[10px] text-gray-500 uppercase tracking-widest font-bold mb-1 relative z-10">
              <span className="relative inline-flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-400" />
              </span>
              <span>Live {"\u00b7"} auto-refresh 5s</span>
            </div>
            <div className="text-7xl font-black text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 via-teal-300 to-cyan-500 relative z-10">
              <AnimatedNumber
                value={totalSaved}
                format={(v) => "$" + v.toLocaleString(undefined, { minimumFractionDigits: 4, maximumFractionDigits: 6 })}
              />
            </div>
            <div className="text-[10px] text-gray-500 mt-1 text-center">
              Somme des 4 classes (input frais, cache_read, cache_creation, output).
              Pour les requêtes antérieures au déploiement du calcul par classe, le chiffre affiché est 0.
            </div>

            <div className="mt-10 flex gap-12">
              <div>
                <div className="text-gray-500 text-xs uppercase tracking-wider font-bold mb-1">Tokens Sent</div>
                <div className="text-3xl font-mono text-gray-200">
                  <AnimatedNumber value={totals.sent.total} />
                </div>
                <div className="text-[10px] text-gray-500 mt-1 font-mono">
                  {totals.sent.input.toLocaleString()} IN / {totals.sent.output.toLocaleString()} OUT
                </div>
              </div>
              <div>
                <div className="text-emerald-500/80 text-xs uppercase tracking-wider font-bold mb-1">Tokens Saved</div>
                <div className="text-3xl font-mono text-emerald-400 flex items-center gap-3">
                  <AnimatedNumber value={Math.max(0, totals.sent.total - totals.optimized.total)} />
                  <span className="text-sm text-emerald-300 bg-emerald-900/40 px-3 py-1 rounded-full border border-emerald-500/20">
                    {percentSaved}%
                  </span>
                </div>
                <div className="text-[10px] text-emerald-500/60 mt-1 font-mono">
                  {totals.sent.total > 0 ? (totals.sent.input - totals.optimized.input).toLocaleString() : 0} IN / {totals.sent.total > 0 ? (totals.sent.output - totals.optimized.output).toLocaleString() : 0} OUT
                </div>
              </div>
            </div>
          </div>

          {/* Cache Hit Ratio Card */}
          <div className="bg-black/40 border border-white/10 rounded-3xl p-8 backdrop-blur-xl shadow-2xl flex flex-col items-center justify-center relative">
            <div className="absolute top-4 right-4 group cursor-help">
              <Info className="w-5 h-5 text-gray-500 hover:text-emerald-400 transition" />
              <div className="absolute right-0 top-6 w-64 p-4 bg-black/90 border border-white/10 rounded-xl shadow-2xl text-xs text-gray-300 opacity-0 group-hover:opacity-100 transition pointer-events-none z-50">
                <div className="mb-2"><strong className="text-white">L0 (API Call)</strong>: Sent to provider.</div>
                <div className="mb-2"><strong className="text-blue-400">L1 (Exact)</strong>: 100% hash match, zero cost.</div>
                <div className="mb-2"><strong className="text-emerald-400">L2 (Semantic)</strong>: Similar intent, high savings.</div>
                <div><strong className="text-purple-400">L3 (Sub-graph)</strong>: Prompt compressed, partial savings.</div>
              </div>
            </div>
            <h2 className="text-sm text-gray-400 uppercase tracking-widest font-bold mb-6 self-start w-full">Cache Hit Ratio</h2>
            <div className="h-48 w-full relative">
              {pieData.length > 0 ? (
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie
                      data={pieData}
                      cx="50%"
                      cy="50%"
                      innerRadius={60}
                      outerRadius={80}
                      paddingAngle={5}
                      dataKey="value"
                      stroke="none"
                    >
                      {pieData.map((entry, index) => (
                        <Cell key={`cell-${index}`} fill={entry.fill} />
                      ))}
                    </Pie>
                    <Tooltip 
                      contentStyle={{ backgroundColor: '#000', borderColor: '#333', borderRadius: '8px' }}
                      itemStyle={{ color: '#fff' }}
                    />
                  </PieChart>
                </ResponsiveContainer>
              ) : (
                <div className="absolute inset-0 flex items-center justify-center text-gray-600 text-sm">No data yet</div>
              )}
              <div className="absolute inset-0 flex items-center justify-center pointer-events-none flex-col">
                <span className="text-2xl font-black text-white">
                  {(() => {
                    const total = cacheDist.MISS + cacheDist.L1 + cacheDist.L2 + cacheDist.L3;
                    return total > 0
                      ? (((cacheDist.L1 + cacheDist.L2 + cacheDist.L3) / total) * 100).toFixed(0)
                      : 0;
                  })()}%
                </span>
                <span className="text-[10px] text-gray-500 mt-1">optimisé</span>
              </div>
            </div>
            <div className="mt-4 flex flex-wrap justify-center gap-3 text-xs">
              {pieData.map(d => (
                <div key={d.name} className="flex items-center gap-1.5 text-gray-400">
                  <div className="w-2.5 h-2.5 rounded-full" style={{ backgroundColor: d.fill }} />
                  {d.name}
                </div>
              ))}
            </div>
          </div>
        </motion.div>

        <motion.div variants={itemVars} className="grid grid-cols-1 lg:grid-cols-2 gap-8">
          {/* Line Chart */}
          <section className="bg-black/40 p-6 rounded-3xl border border-white/10 backdrop-blur-xl shadow-2xl">
            <h3 className="text-sm text-gray-400 uppercase tracking-widest font-bold mb-2">Coût cumulé & économies par classe</h3>
            <p className="text-[10px] text-gray-500 mb-4">
              Lignes du haut : coût original vs coût payé. Aires empilées en bas : économies fragmentées par classe de token.
            </p>
            <div className="h-80 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <ComposedChart data={data}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#ffffff10" vertical={false} />
                  <XAxis dataKey="day" stroke="#888" tick={{ fill: '#888' }} axisLine={false} tickLine={false} />
                  <YAxis yAxisId="left" stroke="#888" tick={{ fill: '#888' }} tickFormatter={(val) => `$${val}`} axisLine={false} tickLine={false} />
                  <YAxis yAxisId="right" orientation="right" stroke="#10b981" tick={{ fill: '#10b981' }} tickFormatter={(val) => `${(val * 100).toFixed(0)}%`} axisLine={false} tickLine={false} domain={[0, 1]} />
                  <Tooltip
                    contentStyle={{ backgroundColor: '#000', borderColor: '#333', borderRadius: '8px' }}
                    itemStyle={{ color: '#fff' }}
                    formatter={(value: any, name: string) => {
                      if (name.includes('Cache read rate') || name.includes('Hit rate')) return `${(Number(value) * 100).toFixed(1)}%`;
                      if (typeof value === 'number') return `$${value.toFixed(4)}`;
                      return value;
                    }}
                  />
                  <Legend iconType="circle" wrapperStyle={{ paddingTop: '20px' }} />
                  <Line yAxisId="left" type="monotone" dataKey="costWithout" name="Original Cost" stroke="#ef4444" strokeWidth={3} dot={false} />
                  <Line yAxisId="left" type="monotone" dataKey="costWith" name="Synapse Proxy Cost" stroke="#10b981" strokeWidth={3} dot={false} />
                  <Area yAxisId="left" type="monotone" dataKey="savingsInputFresh" name="Savings: input frais" stackId="savings" stroke="#10b981" fill="#10b981" fillOpacity={0.4} />
                  <Area yAxisId="left" type="monotone" dataKey="savingsCacheRead" name="Savings: cache_read" stackId="savings" stroke="#34d399" fill="#34d399" fillOpacity={0.4} />
                  <Area yAxisId="left" type="monotone" dataKey="savingsCacheCreation" name="Savings: cache_creation" stackId="savings" stroke="#fb923c" fill="#fb923c" fillOpacity={0.4} />
                  <Area yAxisId="left" type="monotone" dataKey="savingsOutput" name="Savings: output" stackId="savings" stroke="#a855f7" fill="#a855f7" fillOpacity={0.4} />
                  <Line yAxisId="right" type="monotone" dataKey="cacheReadRate" name="Cache read rate" stroke="#facc15" strokeWidth={2} dot={{ r: 3, fill: '#facc15' }} />
                </ComposedChart>
              </ResponsiveContainer>
            </div>
          </section>

          <section className="bg-black/40 p-6 rounded-3xl border border-white/10 backdrop-blur-xl shadow-2xl flex flex-col min-h-[420px] max-h-[80vh]">
            <div className="flex justify-between items-center mb-6">
              <h3 className="text-sm text-gray-400 uppercase tracking-widest font-bold">Live Telemetry</h3>
              <a
                href="/api/analytics/export"
                download
                className="text-xs bg-white/10 hover:bg-white/20 transition px-3 py-1.5 rounded-lg border border-white/10 text-white font-medium flex items-center gap-2"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path><polyline points="7 10 12 15 17 10"></polyline><line x1="12" y1="15" x2="12" y1="3"></line></svg>
                Export CSV
              </a>
            </div>
            <div className="relative flex-1 min-h-0 overflow-y-auto overflow-x-auto pr-1">
              <LiveTelemetryGrouped
                records={logs as LiveRequest[]}
                initialGroupBy="agent"
                onSnapshot={async (groupKey, snapshot) => {
                  // The LiveTelemetryGrouped component prepends a
                  // scope prefix to the key ("session:" or "agent:")
                  // to keep group IDs from colliding across scopes.
                  // The /api/analytics/session endpoint expects
                  // the raw session id (or conv signature), so we
                  // strip the prefix here.
                  const rawKey = groupKey.replace(/^(session|agent|model|legacy):/, "");
                  setSessionId(snapshot.groupBy === "session" ? rawKey : null);
                  setSessionStartTime(snapshot.startedAt);

                  try {
                    // Server-side filter:
                    //   - "session" tab: filter by sessionId. The
                    //     endpoint matches both rows with explicit
                    //     sessionId === rawKey AND rows with empty
                    //     sessionId but matching convSignature ===
                    //     rawKey (so the turn-0 row of a multiturn
                    //     conversation is included).
                    //   - "agent" / "model" tab: no server filter,
                    //     the response would otherwise pull in
                    //     every request in the time window (5s by
                    //     default in the bucket, but the window can
                    //     be wider). We post-filter client-side
                    //     against snapshot.requests so the modal
                    //     shows exactly the rows the bucket has.
                    const sessionFilter = snapshot.groupBy === "session"
                      ? `&sessionId=${encodeURIComponent(rawKey)}`
                      : "";
                    const res = await fetch(
                      `/api/analytics/session?start=${snapshot.startedAt}&end=${snapshot.endedAt}${sessionFilter}`
                    );
                    if (!res.ok) return;
                    const data = await res.json();

                    // Client-side post-filter for agent / model:
                    // the Session Summary modal must show ONLY the
                    // rows that were in the bucket the user clicked.
                    // Otherwise a "curl" agent bucket with 47 rows
                    // would open a modal listing the 200 other rows
                    // the user did not ask about.
                    if (snapshot.groupBy === "agent" || snapshot.groupBy === "model") {
                      const bucketIds = new Set(
                        snapshot.requests.map((r) => r.id)
                      );
                      data.requests = (data.requests || []).filter(
                        (r: any) => bucketIds.has(r.id)
                      );
                      // Also fix aggregates that depend on the
                      // filtered subset, so the counters match what
                      // the modal actually displays.
                      data.totalRequests = data.requests.length;
                      let saved = 0;
                      let origTotal = 0;
                      let optTotal = 0;
                      const dist: Record<string, number> = {};
                      for (const r of data.requests) {
                        saved += (r.promptTokensOrig - r.promptTokensOpt) +
                                 (r.completionTokensOrig - r.completionTokensOpt);
                        origTotal += (r.promptTokensOrig || 0) + (r.completionTokensOrig || 0);
                        optTotal  += (r.promptTokensOpt  || 0) + (r.completionTokensOpt  || 0);
                        const lvl = r.cacheLevel || "MISS";
                        dist[lvl] = (dist[lvl] || 0) + 1;
                      }
                      data.tokens = {
                        original: { total: origTotal, input: 0, output: 0 },
                        optimized: { total: optTotal,  input: 0, output: 0 },
                      };
                      data.cacheHitDistribution = dist;
                    }

                    setSessionResult(data);
                  } catch (e) {
                    console.error("Failed to load session snapshot", e);
                  }
                }}
              />
            </div>
          </section>
        </motion.div>

        {/* Détail des économies par classe (fragmenté) */}
        <motion.div variants={itemVars} className="mt-8">
          <section className="bg-black/40 p-6 rounded-3xl border border-white/10 backdrop-blur-xl shadow-2xl">
            <div className="flex items-center justify-between mb-6">
              <h3 className="text-sm text-gray-400 uppercase tracking-widest font-bold">Détail des économies (par classe de token)</h3>
              <span
                title="Les 4 classes de tokens sont pricées différemment. Sur Anthropic, un token cache_read coûte 0.1x de l'input standard, donc nos savings par classe peuvent varier de \u00b110% selon la composition exacte du payload."
                className="text-[10px] text-gray-500 cursor-help"
              >
                ℹ️ Comment on calcule
              </span>
            </div>

            {totalSavingsReal === 0 && totalSavingsByClass.inputFresh === 0 && totalSavingsByClass.cacheRead === 0 && totalSavingsByClass.cacheCreation === 0 && totalSavingsByClass.output === 0 ? (
              <div className="text-center text-gray-600 text-sm py-6">
                Aucune économie fragmentée enregistrée. Les requêtes effectuées après ce déploiement afficheront leur détail par classe (input frais, cache_read, cache_creation, output).
              </div>
            ) : (
              <>
                {/* Total breakdown: 4 columns + total */}
                <div className="grid grid-cols-2 md:grid-cols-5 gap-3 mb-6">
                  <div className="bg-white/5 border border-white/10 rounded-xl p-3">
                    <div className="text-[10px] uppercase tracking-widest text-gray-500 font-bold mb-1">Input frais</div>
                    <div className="font-mono text-emerald-400 text-sm">${totalSavingsByClass.inputFresh.toFixed(4)}</div>
                    <div className="text-[10px] text-gray-600 mt-1">tokens sans cache</div>
                  </div>
                  <div className="bg-white/5 border border-white/10 rounded-xl p-3">
                    <div className="text-[10px] uppercase tracking-widest text-emerald-500 font-bold mb-1">Cache read</div>
                    <div className="font-mono text-emerald-400 text-sm">${totalSavingsByClass.cacheRead.toFixed(4)}</div>
                    <div className="text-[10px] text-gray-600 mt-1">cache_hit (0.1x input)</div>
                  </div>
                  <div className={`bg-white/5 border ${totalSavingsByClass.cacheCreation < 0 ? 'border-red-500/30' : 'border-white/10'} rounded-xl p-3`}>
                    <div className="text-[10px] uppercase tracking-widest text-orange-400 font-bold mb-1">Cache creation</div>
                    <div className={`font-mono text-sm ${totalSavingsByClass.cacheCreation < 0 ? 'text-red-400' : 'text-orange-400'}`}>
                      {totalSavingsByClass.cacheCreation >= 0 ? '$' : '-$'}{Math.abs(totalSavingsByClass.cacheCreation).toFixed(4)}
                    </div>
                    <div className="text-[10px] text-gray-600 mt-1">write 1.25-2x input</div>
                  </div>
                  <div className="bg-white/5 border border-white/10 rounded-xl p-3">
                    <div className="text-[10px] uppercase tracking-widest text-purple-400 font-bold mb-1">Output</div>
                    <div className="font-mono text-purple-400 text-sm">${totalSavingsByClass.output.toFixed(4)}</div>
                    <div className="text-[10px] text-gray-600 mt-1">tokens de réponse</div>
                  </div>
                  <div className="bg-emerald-500/10 border border-emerald-500/30 rounded-xl p-3 col-span-2 md:col-span-1">
                    <div className="text-[10px] uppercase tracking-widest text-emerald-300 font-bold mb-1">Total réel</div>
                    <div className="font-mono text-emerald-300 text-base font-bold">${totalSavingsReal.toFixed(4)}</div>
                    <div className="text-[10px] text-gray-600 mt-1">somme des 4 classes</div>
                  </div>
                </div>

                {/* Par provider */}
                {Object.keys(savingsByClassByProvider).length > 0 && (
                  <div>
                    <div className="text-[10px] uppercase tracking-widest text-gray-500 font-bold mb-3">Par provider</div>
                    <div className="overflow-x-auto">
                      <table className="w-full text-xs">
                        <thead className="text-gray-500 border-b border-white/10">
                          <tr>
                            <th className="py-2 text-left font-medium">Provider</th>
                            <th className="py-2 text-right font-medium text-emerald-500">Input frais</th>
                            <th className="py-2 text-right font-medium text-emerald-500">Cache read</th>
                            <th className="py-2 text-right font-medium text-orange-400">Cache creation</th>
                            <th className="py-2 text-right font-medium text-purple-400">Output</th>
                            <th className="py-2 text-right font-medium">Total</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-white/5 font-mono">
                          {Object.entries(savingsByClassByProvider)
                            .filter(([, s]) => Math.abs(s.inputFresh + s.cacheRead + s.cacheCreation + s.output) > 0.0001)
                            .sort(([, a], [, b]) => (b.inputFresh + b.cacheRead + b.cacheCreation + b.output) - (a.inputFresh + a.cacheRead + a.cacheCreation + a.output))
                            .map(([prov, s]) => {
                              const total = s.inputFresh + s.cacheRead + s.cacheCreation + s.output;
                              return (
                                <tr key={prov} className="hover:bg-white/[0.02]">
                                  <td className="py-2 text-gray-300">{prov}</td>
                                  <td className="py-2 text-right text-emerald-400">${s.inputFresh.toFixed(4)}</td>
                                  <td className="py-2 text-right text-emerald-400">${s.cacheRead.toFixed(4)}</td>
                                  <td className={`py-2 text-right ${s.cacheCreation < 0 ? 'text-red-400' : 'text-orange-400'}`}>
                                    {s.cacheCreation >= 0 ? '$' : '-$'}{Math.abs(s.cacheCreation).toFixed(4)}
                                  </td>
                                  <td className="py-2 text-right text-purple-400">${s.output.toFixed(4)}</td>
                                  <td className="py-2 text-right text-white font-bold">${total.toFixed(4)}</td>
                                </tr>
                              );
                            })}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}

                {/* Honesty caveat */}
                <div className="mt-4 text-[10px] text-gray-500 leading-relaxed border-t border-white/5 pt-3">
                  <strong className="text-gray-400">Honnêteté du calcul :</strong> chaque requête est approximée comme suit {"\u2014"} on connaît le total de tokens input sauvés (promptOrig - promptOpt) et la proportion de cache_read/cache_creation dans le payload ORIGINAL. On applique cette même proportion aux tokens sauvés. <strong>Cache creation peut être négatif</strong> : si L3 a supprimé du texte qui aurait été écrit en cache (coûte 1.25-2x l'input), c'est un gain ; si L3 a invalidé le cache provider en modifiant le payload, c'est une perte.
                </div>
              </>
            )}
          </section>
        </motion.div>

        {/* Prompt Cache par provider */}
        <motion.div variants={itemVars} className="mt-8">
          <section className="bg-black/40 p-6 rounded-3xl border border-white/10 backdrop-blur-xl shadow-2xl">
            <h3 className="text-sm text-gray-400 uppercase tracking-widest font-bold mb-6">Prompt Cache par provider</h3>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
              {Object.entries(cacheByProvider).filter(([, s]) => s.creation + s.read + s.hit + s.miss > 0).length === 0 ? (
                <div className="col-span-full text-center text-gray-600 text-sm py-6">No prompt cache data yet</div>
              ) : (
                Object.entries(cacheByProvider)
                  .filter(([, s]) => s.creation + s.read + s.hit + s.miss > 0)
                  .map(([prov, s]) => {
                    const rate = cacheHitRateByProvider[prov] ?? 0;
                    const ratePct = (rate * 100).toFixed(1);
                    const rateColor = rate >= 0.7 ? 'text-emerald-400' : rate >= 0.3 ? 'text-yellow-400' : 'text-red-400';
                    return (
                      <div key={prov} className="bg-white/5 border border-white/10 rounded-2xl p-4">
                        <div className="flex items-center justify-between mb-3">
                          <div className="text-xs uppercase tracking-widest text-gray-400 font-bold">{prov}</div>
                          <div className={`text-sm font-mono font-bold ${rateColor}`}>{ratePct}%</div>
                        </div>
                        <div className="space-y-1.5 text-xs">
                          {s.read > 0 && (
                            <div className="flex justify-between">
                              <span className="text-gray-500">Cache read</span>
                              <span className="font-mono text-emerald-400">{s.read.toLocaleString()}</span>
                            </div>
                          )}
                          {s.creation > 0 && (
                            <div className="flex justify-between">
                              <span className="text-gray-500">Cache creation</span>
                              <span className="font-mono text-orange-400">{s.creation.toLocaleString()}</span>
                            </div>
                          )}
                          {s.hit > 0 && (
                            <div className="flex justify-between">
                              <span className="text-gray-500">Cache hit</span>
                              <span className="font-mono text-emerald-400">{s.hit.toLocaleString()}</span>
                            </div>
                          )}
                          {s.miss > 0 && (
                            <div className="flex justify-between">
                              <span className="text-gray-500">Cache miss</span>
                              <span className="font-mono text-gray-400">{s.miss.toLocaleString()}</span>
                            </div>
                          )}
                        </div>
                        <div className="mt-3 pt-3 border-t border-white/5 flex justify-between text-[10px] text-gray-500">
                          <span>{s.count} requests</span>
                          <span className={rateColor}>hit rate</span>
                        </div>
                      </div>
                    );
                  })
              )}
            </div>

            {/* Measured vs Opportunity breakdown */}
            <div className="mt-6 grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="bg-emerald-500/5 border border-emerald-500/20 rounded-xl p-4">
                <div className="text-[10px] uppercase tracking-widest text-emerald-400 font-bold mb-2">Mesuré (interventions réelles)</div>
                <div className="text-xs text-gray-400 space-y-1">
                  <div className="flex justify-between">
                    <span>Hits cache L1 + L2</span>
                    <span className="font-mono text-emerald-400">{measuredSavings.l1L2Hits}</span>
                  </div>
                  <div className="flex justify-between">
                    <span>Compressions L3</span>
                    <span className="font-mono text-emerald-400">{measuredSavings.l3Compressions}</span>
                  </div>
                </div>
              </div>
              {opportunitySavings.highCacheReadProviders.length > 0 && (
                <div className="bg-orange-500/5 border border-orange-500/20 rounded-xl p-4">
                  <div className="text-[10px] uppercase tracking-widest text-orange-400 font-bold mb-2">Opportunité (à activer)</div>
                  <div className="text-xs text-gray-400">
                    Providers avec cache_read &gt; 4x cache_creation (risque L3 d'invalider le prefix) :
                    <div className="mt-1 font-mono text-orange-300">
                      {opportunitySavings.highCacheReadProviders.join(', ')}
                    </div>
                    <div className="mt-2 text-[10px] text-gray-500">
                      Recommandation : envisager de désactiver L3 par défaut sur ces providers.
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* Honesty caveats */}
            <div className="mt-4 text-[10px] text-gray-500 leading-relaxed border-t border-white/5 pt-3">
              <strong className="text-gray-400">Comment on calcule ce chiffre :</strong> Les valeurs ci-dessus mesurent ce qu'Synapse Proxy a effectivement économisé (cache L1/L2 + compression L3).
              Le prix réel d'un token cache_read est 0.1x de l'input standard (Anthropic), donc les économies affichées sur des workloads à fort cache_read sont <strong>surestimées</strong> par le pricing actuel.
              Le L3 peut augmenter le coût net sur des workloads Anthropic multi-turn en invalidant leur cache prompt. Sur abonnement flat, ces valeurs représentent de la capacité libérée, pas un remboursement.
              {cacheHitRateByProvider && Object.values(cacheHitRateByProvider).some(r => r === -1) && (
                <span className="block mt-1 text-yellow-500">
                  Hit rate masqué pour les providers avec &lt; 30 requêtes (cohort trop petite).
                </span>
              )}
            </div>
          </section>
        </motion.div>
      </motion.div>

      {showConfigModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div className="bg-[#111] border border-white/10 rounded-3xl p-8 max-w-md w-full relative">
            <button 
              onClick={() => setShowConfigModal(false)}
              className="absolute top-4 right-4 text-gray-500 hover:text-white"
            >
              <X className="w-6 h-6" />
            </button>
            <h2 className="text-2xl font-bold mb-4 text-white flex items-center gap-3">
              <PlayCircle className="text-red-500 w-6 h-6" />
              Start Recording
            </h2>
            <p className="text-sm text-gray-400 mb-6">
              Tag every incoming agent request with a session id for the
              duration of this recording. You will be able to revisit
              the per-class savings, agent breakdown, and cost impact
              of this session from <code className="text-emerald-300">Admin â†’ Session History</code>.
            </p>

            <div className="p-4 rounded-xl bg-black/40 border border-white/5 mb-6 text-xs text-zinc-400 space-y-2">
              <p>
                <span className="text-zinc-300 font-bold">What gets recorded:</span>{" "}
                token counts (orig vs optimized), per-class savings, cache
                hit rate, latency, agent id, model. Full prompts and
                responses are only kept if Zero-Log is off.
              </p>
              <p>
                <span className="text-zinc-300 font-bold">Upstream cost:</span>{" "}
                <span className="text-emerald-300">unchanged</span> {"\u2014"} the
                session recording tag is free. No extra requests are
                fired to your provider.
              </p>
              <p>
                <span className="text-zinc-300 font-bold">Benchmark Mode is separate.</span>{" "}
                If you want an AI-scored comparison between Synapse Proxy and
                the original provider, enable Benchmark Mode from the
                top menu before starting the recording. Note that
                Benchmark Mode <em>doubles</em> your upstream token spend
                while active.
              </p>
            </div>

            <button
              onClick={startRecording}
              className="w-full py-3 bg-red-500/20 hover:bg-red-500/30 border border-red-500/50 text-red-400 font-bold rounded-xl transition flex items-center justify-center gap-2"
            >
              <PlayCircle className="w-5 h-5" />
              Start Session Recording
            </button>
          </div>
        </div>
      )}

      {sessionResult && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div className="bg-[#111] border border-white/10 rounded-3xl p-8 max-w-3xl w-full relative my-8 max-h-[90vh] overflow-y-auto">
            <button
              onClick={() => setSessionResult(null)}
              className="absolute top-4 right-4 text-gray-500 hover:text-white"
            >
              <X className="w-6 h-6" />
            </button>
            <h2 className="text-2xl font-bold mb-6 text-white flex items-center gap-3">
              <Activity className="text-emerald-400" />
              Session Summary
              {sessionId && (
                <span className="ml-auto text-xs font-mono text-gray-500 bg-white/5 px-2 py-1 rounded">
                  {sessionId}
                </span>
              )}
            </h2>

            <div className="space-y-4">
              {/* --- Top stats: duration, requests, hit rate --- */}
              <div className="grid grid-cols-3 gap-3">
                <div className="flex flex-col p-3 bg-white/5 rounded-xl">
                  <span className="text-gray-500 text-[10px] uppercase tracking-wider font-bold">Duration</span>
                  <span className="font-mono font-bold text-2xl text-white">{(sessionResult.durationMs / 1000).toFixed(1)}s</span>
                </div>
                <div className="flex flex-col p-3 bg-white/5 rounded-xl">
                  <span className="text-gray-500 text-[10px] uppercase tracking-wider font-bold">Total Requests</span>
                  <span className="font-mono font-bold text-2xl text-white">{sessionResult.totalRequests}</span>
                </div>
                <div className="flex flex-col p-3 bg-white/5 rounded-xl">
                  <span className="text-gray-500 text-[10px] uppercase tracking-wider font-bold">Hit Rate</span>
                  <span className="font-mono font-bold text-2xl text-emerald-400">
                    {sessionResult.totalRequests > 0
                      ? (((sessionResult.totalRequests - (sessionResult.cacheHitDistribution?.MISS || 0)) / sessionResult.totalRequests) * 100).toFixed(1)
                      : "0.0"}%
                  </span>
                </div>
              </div>

              {/* --- Cache hit distribution (L0/L1/L2/L3/LOOP/MISS) --- */}
              <div className="grid grid-cols-6 gap-2">
                <div className="bg-zinc-700/40 p-2 rounded-lg text-center border border-white/5">
                  <div className="text-[10px] text-zinc-400 uppercase font-bold">MISS</div>
                  <div className="font-mono text-zinc-200 text-sm font-bold">{sessionResult.cacheHitDistribution?.MISS || 0}</div>
                </div>
                <div className="bg-cyan-500/20 p-2 rounded-lg text-center border border-cyan-500/30">
                  <div className="text-[10px] text-cyan-300 uppercase font-bold">L0 Coalesced</div>
                  <div className="font-mono text-cyan-300 text-sm font-bold">{sessionResult.cacheHitDistribution?.L0 || 0}</div>
                </div>
                <div className="bg-blue-500/20 p-2 rounded-lg text-center border border-blue-500/30">
                  <div className="text-[10px] text-blue-300 uppercase font-bold">L1 Hit</div>
                  <div className="font-mono text-blue-300 text-sm font-bold">{sessionResult.cacheHitDistribution?.L1 || 0}</div>
                </div>
                <div className="bg-emerald-500/20 p-2 rounded-lg text-center border border-emerald-500/30">
                  <div className="text-[10px] text-emerald-300 uppercase font-bold">L2 Hit</div>
                  <div className="font-mono text-emerald-300 text-sm font-bold">{sessionResult.cacheHitDistribution?.L2 || 0}</div>
                </div>
                <div className="bg-purple-500/20 p-2 rounded-lg text-center border border-purple-500/30">
                  <div className="text-[10px] text-purple-300 uppercase font-bold">L3 Hit</div>
                  <div className="font-mono text-purple-300 text-sm font-bold">{sessionResult.cacheHitDistribution?.L3 || 0}</div>
                </div>
                <div className="bg-amber-500/20 p-2 rounded-lg text-center border border-amber-500/30">
                  <div className="text-[10px] text-amber-300 uppercase font-bold">LOOP</div>
                  <div className="font-mono text-amber-300 text-sm font-bold">{sessionResult.cacheHitDistribution?.LOOP || 0}</div>
                </div>
              </div>

              {/* --- Tokens: original vs optimized --- */}
              <div className="grid grid-cols-2 gap-3">
                <div className="flex justify-between items-center p-3 bg-white/5 rounded-xl">
                  <span className="text-gray-400 text-sm">Original Tokens</span>
                  <div className="text-right">
                    <div className="font-mono font-bold text-gray-300">{sessionResult.tokens.original.total.toLocaleString()}</div>
                    <div className="text-[10px] text-gray-500 font-mono">
                      {sessionResult.tokens.original.input.toLocaleString()} in / {sessionResult.tokens.original.output.toLocaleString()} out
                    </div>
                  </div>
                </div>
                <div className="flex justify-between items-center p-3 bg-white/5 rounded-xl border border-emerald-500/30">
                  <span className="text-gray-400 text-sm">Optimized Tokens</span>
                  <div className="text-right">
                    <div className="font-mono font-bold text-emerald-400">{sessionResult.tokens.optimized.total.toLocaleString()}</div>
                    <div className="text-[10px] text-emerald-500/70 font-mono">
                      {sessionResult.tokens.optimized.input.toLocaleString()} in / {sessionResult.tokens.optimized.output.toLocaleString()} out
                    </div>
                  </div>
                </div>
              </div>

              {/* --- Tokens Purged (Saved) with percentage --- */}
              <div className="flex flex-col p-3 bg-emerald-500/10 rounded-xl border border-emerald-500/50">
                <div className="flex justify-between items-center mb-2">
                  <span className="text-emerald-400 text-sm font-bold">Tokens Purged (Saved)</span>
                  <div className="flex items-center gap-2">
                    <span className="font-mono font-bold text-emerald-400">
                      {(sessionResult.tokens.original.total - sessionResult.tokens.optimized.total).toLocaleString()}
                    </span>
                    <span className="text-xs bg-emerald-500/20 text-emerald-300 px-2 py-0.5 rounded-full">
                      {sessionResult.tokens.original.total > 0 ? (((sessionResult.tokens.original.total - sessionResult.tokens.optimized.total) / sessionResult.tokens.original.total) * 100).toFixed(1) : "0.0"}%
                    </span>
                  </div>
                </div>
                <div className="flex justify-between text-xs text-emerald-500/70 border-t border-emerald-500/20 pt-2 mt-1">
                  <span>Input: {(sessionResult.tokens.original.input - sessionResult.tokens.optimized.input).toLocaleString()}</span>
                  <span>Output: {(sessionResult.tokens.original.output - sessionResult.tokens.optimized.output).toLocaleString()}</span>
                </div>
              </div>

              {/* --- Cache tokens (Anthropic-style) --- */}
              {sessionResult.cacheTokens && (
                <div className="grid grid-cols-4 gap-2">
                  <div className="p-2 bg-white/5 rounded-lg text-center border border-white/5">
                    <div className="text-[10px] text-gray-500 uppercase font-bold">Cache Write</div>
                    <div className="font-mono text-white text-xs">{(sessionResult.cacheTokens.creation || 0).toLocaleString()}</div>
                  </div>
                  <div className="p-2 bg-white/5 rounded-lg text-center border border-white/5">
                    <div className="text-[10px] text-gray-500 uppercase font-bold">Cache Read</div>
                    <div className="font-mono text-white text-xs">{(sessionResult.cacheTokens.read || 0).toLocaleString()}</div>
                  </div>
                  <div className="p-2 bg-white/5 rounded-lg text-center border border-white/5">
                    <div className="text-[10px] text-gray-500 uppercase font-bold">Cache Hit</div>
                    <div className="font-mono text-white text-xs">{(sessionResult.cacheTokens.hit || 0).toLocaleString()}</div>
                  </div>
                  <div className="p-2 bg-white/5 rounded-lg text-center border border-white/5">
                    <div className="text-[10px] text-gray-500 uppercase font-bold">Cache Miss</div>
                    <div className="font-mono text-white text-xs">{(sessionResult.cacheTokens.miss || 0).toLocaleString()}</div>
                  </div>
                </div>
              )}

              {/* --- Per-class savings breakdown (input fresh / cache read / cache creation / output) --- */}
              {sessionResult.savingsByClass && (
                <div className="p-3 bg-white/5 rounded-xl">
                  <div className="text-[10px] text-gray-500 uppercase tracking-wider font-bold mb-2">Per-Class Savings</div>
                  <div className="grid grid-cols-4 gap-2">
                    <div className="text-center">
                      <div className="text-[10px] text-emerald-400">input fresh</div>
                      <div className="font-mono text-emerald-300 text-xs">${(sessionResult.savingsByClass.inputFresh || 0).toFixed(6)}</div>
                    </div>
                    <div className="text-center">
                      <div className="text-[10px] text-blue-400">cache read</div>
                      <div className="font-mono text-blue-300 text-xs">${(sessionResult.savingsByClass.cacheRead || 0).toFixed(6)}</div>
                    </div>
                    <div className="text-center">
                      <div className="text-[10px] text-purple-400">cache creation</div>
                      <div className="font-mono text-purple-300 text-xs">${(sessionResult.savingsByClass.cacheCreation || 0).toFixed(6)}</div>
                    </div>
                    <div className="text-center">
                      <div className="text-[10px] text-amber-400">output</div>
                      <div className="font-mono text-amber-300 text-xs">${(sessionResult.savingsByClass.output || 0).toFixed(6)}</div>
                    </div>
                  </div>
                </div>
              )}

              {/* --- Per-provider breakdown --- */}
              {sessionResult.byProvider && Object.keys(sessionResult.byProvider).length > 0 && (
                <div className="p-3 bg-white/5 rounded-xl">
                  <div className="text-[10px] text-gray-500 uppercase tracking-wider font-bold mb-2">By Provider</div>
                  {Object.entries(sessionResult.byProvider).map(([prov, stats]: [string, any]) => (
                    <div key={prov} className="flex justify-between items-center py-1.5 border-b border-white/5 last:border-0">
                      <span className="text-sm text-gray-300 font-mono">{prov}</span>
                      <div className="flex items-center gap-3 text-xs">
                        <span className="text-gray-500">{stats.requests} req</span>
                        <span className="text-emerald-400">${(stats.costSaved || 0).toFixed(6)}</span>
                        <span className="text-blue-400">{(stats.cacheHits || 0)} cache hits</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {/* --- Per-agent breakdown --- */}
              {sessionResult.byAgent && Object.keys(sessionResult.byAgent).length > 0 && (
                <div className="p-3 bg-white/5 rounded-xl">
                  <div className="text-[10px] text-gray-500 uppercase tracking-wider font-bold mb-2">By Agent</div>
                  {Object.entries(sessionResult.byAgent).map(([agentID, stats]: [string, any]) => (
                    <div key={agentID} className="flex justify-between items-center py-1.5 border-b border-white/5 last:border-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm text-gray-300 font-mono">{agentID || "unknown"}</span>
                        {stats.label && stats.label !== agentID && (
                          <span className="text-[10px] text-gray-500">{stats.label}</span>
                        )}
                      </div>
                      <div className="flex items-center gap-3 text-xs">
                        <span className="text-gray-500">{stats.requests} req</span>
                        <span className="text-emerald-400">${(stats.costSaved || 0).toFixed(6)}</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {/* --- Costs: with/without Synapse Proxy + saved --- */}
              {sessionResult.costs && (
                <div className="grid grid-cols-3 gap-3">
                  <div className="flex flex-col p-3 bg-red-500/10 rounded-xl border border-red-500/30">
                    <span className="text-red-300 text-[10px] uppercase tracking-wider font-bold">Without Synapse Proxy</span>
                    <span className="font-mono font-bold text-xl text-red-400">${(sessionResult.costs.withoutCache || 0).toFixed(6)}</span>
                  </div>
                  <div className="flex flex-col p-3 bg-zinc-700/30 rounded-xl border border-white/5">
                    <span className="text-zinc-400 text-[10px] uppercase tracking-wider font-bold">With Synapse Proxy</span>
                    <span className="font-mono font-bold text-xl text-zinc-300">${(sessionResult.costs.withCache || 0).toFixed(6)}</span>
                  </div>
                  <div className="flex flex-col p-3 bg-gradient-to-br from-emerald-500/20 to-teal-500/20 rounded-xl border border-emerald-500/50">
                    <span className="text-emerald-300 text-[10px] uppercase tracking-wider font-bold">Net Cash Saved</span>
                    <span className="font-mono font-bold text-xl text-emerald-400">${(sessionResult.costs.saved || 0).toFixed(6)}</span>
                  </div>
                </div>
              )}

              {/* --- Top 10 most expensive requests (debug) --- */}
              {sessionResult.topExpensive && sessionResult.topExpensive.length > 0 && (
                <div className="p-3 bg-white/5 rounded-xl">
                  <div className="text-[10px] text-gray-500 uppercase tracking-wider font-bold mb-2">Top Requests by Savings</div>
                  <div className="max-h-48 overflow-y-auto">
                    {sessionResult.topExpensive.map((r: any, i: number) => (
                      <div key={r.id} className="flex justify-between items-center py-1.5 border-b border-white/5 last:border-0 text-xs">
                        <div className="flex items-center gap-2 min-w-0 flex-1">
                          <span className="text-gray-500 font-mono w-4">{i + 1}.</span>
                          <span className="text-emerald-300 font-mono">{r.model}</span>
                          <span className={`px-1.5 py-0.5 rounded text-[10px] ${
                            r.cacheLevel === "L0"
                              ? "bg-cyan-500/20 text-cyan-300"
                              : r.cacheLevel === "L1"
                              ? "bg-blue-500/20 text-blue-300"
                              : r.cacheLevel === "L2"
                              ? "bg-emerald-500/20 text-emerald-300"
                              : r.cacheLevel === "L3" || r.cacheLevel === "LOOP"
                              ? "bg-purple-500/20 text-purple-300"
                              : "bg-zinc-700/40 text-zinc-300"
                          }`}>{r.cacheLevel}</span>
                          <span className="text-gray-500 truncate">{r.agentId || ""}</span>
                        </div>
                        <div className="flex items-center gap-2 font-mono">
                          <span className="text-gray-400">{(r.promptTokensOrig || 0).toLocaleString()}/{r.completionTokensOrig || 0}</span>
                          <span className="text-emerald-400">${(r.costSaved || 0).toFixed(6)}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* --- Average latency --- */}

              {/* --- NEW: OBSERVABILITY ANALYTICS --- */}
              {sessionResult.requests && sessionResult.requests.length > 0 && (
                <div className="mt-8 space-y-6 border-t border-white/10 pt-8">
                  <div className="flex items-center justify-between mb-4">
                    <h3 className="text-xl font-bold text-white flex items-center gap-2">
                      <Activity className="w-5 h-5 text-emerald-400" /> Observability & Replay
                    </h3>
                    <a href={`/api/analytics/export?sessionId=${sessionId}`} target="_blank" rel="noreferrer" className="flex items-center gap-2 px-4 py-2 bg-indigo-500/20 text-indigo-300 hover:bg-indigo-500/30 border border-indigo-500/30 rounded-lg text-xs font-bold transition-colors">
                      <Database className="w-4 h-4" /> Export Dataset (JSONL)
                    </a>
                  </div>

                  {/* Context Window Graph */}
                  <div className="bg-black/30 border border-white/5 p-5 rounded-2xl">
                    <h4 className="text-sm font-bold text-gray-300 mb-4 flex items-center gap-2">
                      <Sparkles className="w-4 h-4 text-emerald-400" /> Context Window: Original vs L3 Compressed
                    </h4>
                    <div className="h-48 w-full text-xs">
                      <ResponsiveContainer width="100%" height="100%">
                        <ComposedChart data={sessionResult.requests.map((r: any, i: number) => ({ index: i+1, original: r.promptTokensOrig, optimized: r.promptTokensOpt }))}>
                          <CartesianGrid strokeDasharray="3 3" stroke="#ffffff10" vertical={false} />
                          <XAxis dataKey="index" stroke="#ffffff40" tick={{ fill: '#ffffff60' }} />
                          <YAxis stroke="#ffffff40" tick={{ fill: '#ffffff60' }} />
                          <Tooltip 
                            contentStyle={{ backgroundColor: '#0f0f0f', borderColor: '#ffffff20', borderRadius: '12px' }}
                            itemStyle={{ fontWeight: 'bold' }}
                          />
                          <Legend />
                          <Area type="monotone" dataKey="original" name="Original Context" fill="#ef444420" stroke="#ef4444" strokeWidth={2} />
                          <Line type="monotone" dataKey="optimized" name="L3 Compressed" stroke="#10b981" strokeWidth={3} dot={{ fill: '#10b981', r: 4 }} activeDot={{ r: 6 }} />
                        </ComposedChart>
                      </ResponsiveContainer>
                    </div>
                  </div>

                  {/* System Prompt Diff */}
                  {sessionResult.requests.length > 1 && sessionResult.requests[0].systemPrompt && sessionResult.requests[sessionResult.requests.length-1].systemPrompt !== sessionResult.requests[0].systemPrompt && (
                    <div className="bg-black/30 border border-white/5 p-5 rounded-2xl">
                      <h4 className="text-sm font-bold text-gray-300 mb-4 flex items-center gap-2">
                        <Info className="w-4 h-4 text-purple-400" /> System Prompt Evolution (Diff)
                      </h4>
                      <div className="rounded-lg overflow-hidden border border-white/10 text-xs">
                        <ReactDiffViewer 
                          oldValue={sessionResult.requests[0].systemPrompt} 
                          newValue={sessionResult.requests[sessionResult.requests.length-1].systemPrompt} 
                          splitView={true}
                          useDarkTheme={true}
                          hideLineNumbers={true}
                        />
                      </div>
                    </div>
                  )}

                  {/* Timeline & Tool Calls */}
                  <div className="bg-black/30 border border-white/5 p-5 rounded-2xl">
                    <h4 className="text-sm font-bold text-gray-300 mb-4 flex items-center gap-2">
                      <Clock className="w-4 h-4 text-blue-400" /> Agent Flow Timeline
                    </h4>
                    <div className="relative border-l border-white/10 ml-3 pl-6 space-y-6">
                      {sessionResult.requests.map((r: any, i: number) => (
                        <div key={i} className="relative">
                          <div className="absolute -left-[31px] top-1 w-3 h-3 rounded-full bg-emerald-500 ring-4 ring-[#0f0f0f]" />
                          <div className="flex items-center gap-2 mb-1">
                            <span className="text-xs font-bold text-white">Step {i+1}</span>
                            <span className="text-[10px] text-gray-500">{new Date(r.ts).toLocaleTimeString()}</span>
                            <span className={`text-[10px] px-1.5 py-0.5 rounded font-bold ${r.cacheLevel !== 'NONE' ? 'bg-emerald-500/20 text-emerald-400' : 'bg-white/5 text-gray-400'}`}>
                              {r.cacheLevel !== 'NONE' ? `Cache: ${r.cacheLevel}` : 'Provider'}
                            </span>
                            <span className="text-[10px] text-gray-400 font-mono">{r.durationMs}ms</span>
                          </div>
                          
                          {/* Tool Calls inside the request */}
                          {r.toolCalls && r.toolCalls.length > 0 && (
                            <div className="mt-2 space-y-2">
                              {r.toolCalls.map((tc: any, tIdx: number) => (
                                <div key={tIdx} className="bg-black/50 border border-indigo-500/20 p-3 rounded-lg">
                                  <div className="text-xs font-bold text-indigo-300 flex items-center gap-1.5 mb-1">
                                    <Sparkles className="w-3 h-3" /> Tool Call: {tc.function?.name}
                                  </div>
                                  <pre className="text-[10px] text-gray-400 overflow-x-auto">
                                    {tc.function?.arguments}
                                  </pre>
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              )}

              {sessionResult.avgDurationMs !== undefined && (
                <div className="flex justify-between items-center px-3 py-2 text-xs text-gray-500 border-t border-white/5">
                  <span>Avg latency per request</span>
                  <span className="font-mono">{sessionResult.avgDurationMs.toFixed(0)}ms</span>
                </div>
              )}
            </div>

            <p className="text-xs text-gray-500 text-center mt-6">
              Compare this exact trajectory with Synapse Proxy vs Without.
            </p>
          </div>
        </div>
      )}

      {diffLog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div className="bg-[#0a0a0a] border border-white/10 rounded-3xl p-6 max-w-6xl w-full h-[80vh] flex flex-col relative">
            <button 
              onClick={() => setDiffLog(null)}
              className="absolute top-4 right-4 text-gray-500 hover:text-white transition bg-white/5 p-2 rounded-full"
            >
              <X className="w-5 h-5" />
            </button>
            <div className="mb-4">
              <h2 className="text-xl font-bold text-white flex items-center gap-2">
                <Database className="text-purple-400 w-5 h-5" /> 
                Telemetry X-Ray (L3 Compression Diff)
              </h2>
              <p className="text-sm text-gray-400">View exactly what tokens were purged from this request.</p>
            </div>
            <div className="flex-1 overflow-auto border border-white/10 rounded-xl bg-[#0d0d0d] p-1">
              <ReactDiffViewer
                oldValue={formatJSON(diffLog.originalPayload)}
                newValue={formatJSON(diffLog.optimizedPayload)}
                splitView={false}
                useDarkTheme={true}
                compareMethod={DiffMethod.WORDS}
                hideLineNumbers={false}
                styles={{
                  variables: {
                    dark: {
                      diffViewerBackground: 'transparent',
                      diffViewerColor: '#a3a3a3',
                      addedBackground: '#042b15',
                      addedColor: '#4ade80',
                      removedBackground: '#3f0f14',
                      removedColor: '#f87171',
                      wordAddedBackground: '#065f2c',
                      wordRemovedBackground: '#7f1d1d',
                      codeFoldBackground: '#1a1a1a',
                      emptyLineBackground: 'transparent',
                      gutterBackground: '#000000',
                      gutterBackgroundDark: '#111111',
                      highlightBackground: '#2d2d2d',
                      highlightGutterBackground: '#2d2d2d',
                      codeFoldContentColor: '#737373',
                    }
                  }
                }}
              />
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
