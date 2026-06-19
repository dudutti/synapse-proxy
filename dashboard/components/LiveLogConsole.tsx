"use client";

import React, { useEffect, useRef, useState, useCallback, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Terminal, Pause, Play, Trash2, Filter, AlertTriangle, Zap, Database } from "lucide-react";

// Live log console — SSE-fed terminal-style display of every
// RequestLog in real time. Auto-scrolls to bottom unless the user has
// scrolled up (then a "↓ new logs" pill appears).
//
// Filters in the toolbar:
//   - Cache level (multi-select): NONE / L0 / L1 / L2 / L3 / LOOP
//   - Agent (text search, debounced)
//   - Min cost saved (slider, cents)
//
// Pause stops polling but keeps the buffer so you can scroll freely.
// Clear wipes the buffer (does not affect the server-side RequestLog).

type LogEntry = {
  id: string;
  ts: string;
  cacheLevel: string;
  model: string;
  provider: string;
  tokensIn: number;
  tokensOut: number;
  tokensInOpt: number;
  tokensOutOpt: number;
  durationMs: number | null;
  costSaved: number;
  agentId: string;
  agentLabel: string;
  sessionId: string;
  apiKeyId: string;
};

const CACHE_LEVELS = ["NONE", "L0", "L1", "L2", "L3", "LOOP"] as const;
const MAX_BUFFER = 500; // keep last 500 entries in memory

const CACHE_COLORS: Record<string, string> = {
  NONE: "text-zinc-500",
  L0: "text-cyan-300",
  L1: "text-blue-300",
  L2: "text-emerald-300",
  L3: "text-purple-300",
  LOOP: "text-amber-300",
};

const CACHE_BG: Record<string, string> = {
  NONE: "bg-zinc-500/10 border-zinc-500/30",
  L0: "bg-cyan-500/10 border-cyan-500/30",
  L1: "bg-blue-500/10 border-blue-500/30",
  L2: "bg-emerald-500/10 border-emerald-500/30",
  L3: "bg-purple-500/10 border-purple-500/30",
  LOOP: "bg-amber-500/10 border-amber-500/30",
};

function fmtTs(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString("en-US", { hour12: false }) +
    "." + String(d.getMilliseconds()).padStart(3, "0");
}

function fmtNum(n: number): string {
  return new Intl.NumberFormat("en-US").format(Math.round(n));
}

export function LiveLogConsole() {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [connected, setConnected] = useState(false);
  const [paused, setPaused] = useState(false);
  const [activeLevels, setActiveLevels] = useState<Set<string>>(new Set(CACHE_LEVELS));
  const [agentFilter, setAgentFilter] = useState("");
  const [minCostSaved, setMinCostSaved] = useState(0); // cents
  const [statusMsg, setStatusMsg] = useState<string>("connecting…");
  const [stats, setStats] = useState({ received: 0, filtered: 0, errors: 0 });
  const [autoScroll, setAutoScroll] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastScrollTop = useRef(0);
  const pausedRef = useRef(false);

  // Keep a ref of paused state so the SSE callback (defined once on mount)
  // always sees the latest value without re-subscribing.
  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  // Filtered view (memoised so the terminal scroll doesn't re-render
  // on every parent state change).
  const filtered = useMemo(() => {
    const needle = agentFilter.toLowerCase().trim();
    return logs.filter((l) => {
      if (!activeLevels.has(l.cacheLevel || "NONE")) return false;
      if (minCostSaved > 0 && l.costSaved < minCostSaved / 100) return false;
      if (needle && !(l.agentId + " " + l.agentLabel + " " + l.model).toLowerCase().includes(needle)) {
        return false;
      }
      return true;
    });
  }, [logs, activeLevels, agentFilter, minCostSaved]);

  // Detect manual scroll-up so we can pause auto-scroll.
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.clientHeight - el.scrollTop < 8;
    setAutoScroll(atBottom);
    lastScrollTop.current = el.scrollTop;
  }, []);

  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [filtered, autoScroll]);

  // Stats: per-level hit count over the current buffer (so the
  // SUPERADMIN can see "60% L2 hits in the last 30s" at a glance).
  const bufferStats = useMemo(() => {
    const counts: Record<string, number> = { NONE: 0, L0: 0, L1: 0, L2: 0, L3: 0, LOOP: 0 };
    let totalCost = 0;
    for (const l of logs) {
      counts[l.cacheLevel || "NONE"] = (counts[l.cacheLevel || "NONE"] || 0) + 1;
      totalCost += l.costSaved;
    }
    return { counts, totalCost };
  }, [logs]);

  useEffect(() => {
    if (typeof EventSource === "undefined") {
      setStatusMsg("EventSource not supported in this browser");
      return;
    }
    const es = new EventSource("/api/admin/logs/stream");
    es.onopen = () => {
      setConnected(true);
      setStatusMsg("connected · streaming live RequestLog");
    };
    es.onerror = () => {
      setConnected(false);
      setStatusMsg("connection lost · reconnecting…");
    };
    es.onmessage = (e) => {
      try {
        const l: LogEntry = JSON.parse(e.data);
        // When paused, drop the entry but keep stats accurate.
        setLogs((prev) => {
          const next = pausedRef.current ? prev : [l, ...prev].slice(0, MAX_BUFFER);
          return next;
        });
        setStats((s) => ({
          received: s.received + 1,
          filtered: s.filtered + (activeLevels.has(l.cacheLevel || "NONE") ? 1 : 0),
          errors: s.errors,
        }));
      } catch (err) {
        setStats((s) => ({ ...s, errors: s.errors + 1 }));
      }
    };
    return () => es.close();
    // We intentionally don't depend on activeLevels — toggling filters
    // must not close/reopen the SSE connection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const toggleLevel = (lvl: string) => {
    setActiveLevels((prev) => {
      const next = new Set(prev);
      if (next.has(lvl)) next.delete(lvl);
      else next.add(lvl);
      return next;
    });
  };

  const clearBuffer = () => {
    setLogs([]);
    setStats({ received: 0, filtered: 0, errors: 0 });
  };

  return (
    <div className="rounded-2xl bg-black/60 border border-white/10 overflow-hidden">
      {/* Header / toolbar */}
      <div className="border-b border-white/10 bg-black/40 px-4 py-3 space-y-3">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-3">
            <Terminal className="w-4 h-4 text-emerald-400" />
            <h2 className="text-sm font-black uppercase tracking-widest text-zinc-300">
              Live Log Console
            </h2>
            <AnimatePresence mode="wait">
              {connected ? (
                <motion.span
                  key="ok"
                  initial={{ opacity: 0, scale: 0.9 }}
                  animate={{ opacity: 1, scale: 1 }}
                  className="flex items-center gap-1.5 text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full bg-emerald-500/15 text-emerald-300 border border-emerald-500/30"
                >
                  <motion.span
                    className="w-1.5 h-1.5 rounded-full bg-emerald-400"
                    animate={{ opacity: [1, 0.4, 1] }}
                    transition={{ duration: 1.2, repeat: Infinity }}
                  />
                  LIVE
                </motion.span>
              ) : (
                <motion.span
                  key="down"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  className="flex items-center gap-1.5 text-[10px] font-bold uppercase tracking-wider px-2 py-0.5 rounded-full bg-rose-500/15 text-rose-300 border border-rose-500/30"
                >
                  <AlertTriangle className="w-3 h-3" />
                  DOWN
                </motion.span>
              )}
            </AnimatePresence>
            <span className="text-[10px] text-zinc-500 font-mono">{statusMsg}</span>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setPaused(!paused)}
              className={`flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-bold border transition ${
                paused
                  ? "bg-amber-500/15 text-amber-300 border-amber-500/30"
                  : "bg-white/5 text-zinc-300 border-white/10 hover:bg-white/10"
              }`}
              title={paused ? "Resume polling" : "Pause polling"}
            >
              {paused ? <Play className="w-3 h-3" /> : <Pause className="w-3 h-3" />}
              {paused ? "Paused" : "Live"}
            </button>
            <button
              type="button"
              onClick={clearBuffer}
              className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-bold border bg-white/5 text-zinc-300 border-white/10 hover:bg-white/10 transition"
              title="Clear in-memory buffer (does not touch server-side logs)"
            >
              <Trash2 className="w-3 h-3" />
              Clear
            </button>
          </div>
        </div>

        {/* Cache level filter pills */}
        <div className="flex flex-wrap items-center gap-1.5">
          <Filter className="w-3 h-3 text-zinc-500 mr-1" />
          {CACHE_LEVELS.map((lvl) => {
            const active = activeLevels.has(lvl);
            const count = bufferStats.counts[lvl] || 0;
            return (
              <button
                key={lvl}
                type="button"
                onClick={() => toggleLevel(lvl)}
                className={`flex items-center gap-1.5 px-2 py-1 rounded-md text-[10px] font-bold uppercase tracking-wider border transition ${
                  active
                    ? `${CACHE_BG[lvl]} ${CACHE_COLORS[lvl]}`
                    : "bg-white/5 text-zinc-600 border-white/5 opacity-50"
                }`}
              >
                {lvl}
                <span className="text-[9px] opacity-70">{count}</span>
              </button>
            );
          })}
          <span className="text-[10px] text-zinc-600 ml-auto font-mono">
            {stats.received} received · {filtered.length} shown · {bufferStats.totalCost.toFixed(4)}$ saved
          </span>
        </div>

        {/* Agent + cost filter row */}
        <div className="flex flex-wrap items-center gap-3">
          <input
            type="text"
            placeholder="Filter agent / model…"
            value={agentFilter}
            onChange={(e) => setAgentFilter(e.target.value)}
            className="flex-1 min-w-[180px] bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 text-xs text-zinc-200 placeholder-zinc-600 outline-none focus:border-emerald-500/50 font-mono"
          />
          <label className="flex items-center gap-2 text-[10px] font-bold uppercase tracking-wider text-zinc-500">
            <span>Min $ saved</span>
            <input
              type="range"
              min="0"
              max="100"
              step="1"
              value={minCostSaved}
              onChange={(e) => setMinCostSaved(Number(e.target.value))}
              className="accent-emerald-400 w-32"
            />
            <span className="text-zinc-300 font-mono w-12 text-right">
              ${(minCostSaved / 100).toFixed(2)}
            </span>
          </label>
        </div>
      </div>

      {/* Terminal body */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="font-mono text-[11px] leading-relaxed bg-[#0a0a0a] overflow-auto"
        style={{ height: 420 }}
      >
        {filtered.length === 0 ? (
          <div className="h-full flex items-center justify-center text-zinc-600 text-xs">
            <div className="text-center space-y-2">
              <Database className="w-8 h-8 mx-auto opacity-30" />
              <p>No logs match current filters.</p>
              <p className="text-[10px]">Stream is live · waiting for traffic…</p>
            </div>
          </div>
        ) : (
          <ul className="divide-y divide-white/[0.03]">
            <AnimatePresence initial={false}>
              {filtered.map((l) => (
                <motion.li
                  key={l.id}
                  initial={{ opacity: 0, x: -10 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: 10 }}
                  transition={{ duration: 0.2 }}
                  className="px-4 py-1.5 hover:bg-white/[0.02] flex items-center gap-3 font-mono"
                >
                  <span className="text-zinc-600 w-20 tabular-nums">{fmtTs(l.ts)}</span>
                  <span
                    className={`px-1.5 py-0.5 rounded text-[10px] font-bold border ${
                      CACHE_BG[l.cacheLevel || "NONE"]
                    } ${CACHE_COLORS[l.cacheLevel || "NONE"]} w-16 text-center`}
                  >
                    {l.cacheLevel || "MISS"}
                  </span>
                  <span className="text-zinc-300 w-44 truncate">{l.model || "—"}</span>
                  <span className="text-zinc-500 w-20 truncate text-[10px]">
                    {l.provider}
                  </span>
                  <span className="text-zinc-400 tabular-nums w-24 text-right">
                    {fmtNum(l.tokensIn)} → {fmtNum(l.tokensInOpt)}
                  </span>
                  <span className="text-zinc-400 tabular-nums w-20 text-right">
                    {l.durationMs != null ? `${l.durationMs}ms` : "—"}
                  </span>
                  <span className="text-emerald-400 tabular-nums w-20 text-right font-bold">
                    ${l.costSaved.toFixed(4)}
                  </span>
                  <span className="text-zinc-500 text-[10px] truncate flex-1">
                    {l.agentLabel || l.agentId || ""}
                  </span>
                </motion.li>
              ))}
            </AnimatePresence>
          </ul>
        )}

        {/* Auto-scroll pill */}
        {!autoScroll && filtered.length > 0 && (
          <motion.button
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            onClick={() => {
              setAutoScroll(true);
              if (scrollRef.current) {
                scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
              }
            }}
            className="sticky bottom-3 left-1/2 -translate-x-1/2 mx-auto block px-3 py-1.5 rounded-full bg-emerald-500/20 hover:bg-emerald-500/30 border border-emerald-500/40 text-emerald-200 text-[10px] font-bold uppercase tracking-wider shadow-lg"
          >
            ↓ Resume auto-scroll
          </motion.button>
        )}
      </div>
    </div>
  );
}
