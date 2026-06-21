"use client";

import React, { useEffect, useState, useCallback, useMemo } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Search, ChevronLeft, ChevronRight, X, FileCode2, Eye, Clock, Hash, Database, Filter as FilterIcon, RotateCcw, Sparkles } from "lucide-react";
import { useSearchParams, useRouter } from "next/navigation";

// Query state is just a plain object — we serialise to query string in
// the useEffect that fetches.
type Query = {
  agent: string;
  model: string;
  provider: string;
  level: string;
  minTokens: string;
  minDuration: string;
  minCost: string;
  virtualKey: string;
  from: string;
  to: string;
  sessionId: string;
};

const EMPTY: Query = {
  agent: "",
  model: "",
  provider: "",
  level: "",
  minTokens: "",
  minDuration: "",
  minCost: "",
  virtualKey: "",
  from: "",
  to: "",
  sessionId: "",
};

type Row = {
  id: string;
  cacheLevel: string;
  createdAt: string;
  model: string;
  provider: string;
  promptTokensOrig: number;
  completionTokensOrig: number;
  promptTokensOpt: number;
  completionTokensOpt: number;
  durationMs: number | null;
  costSaved: number;
  agentId: string;
  agentLabel: string;
  sessionId: string;
};

type Aggregate = {
  count: number;
  tokensSaved: number;
  totalCostSaved: number;
};

const CACHE_COLORS: Record<string, string> = {
  NONE: "bg-zinc-700/40 text-zinc-300 border-zinc-600/30",
  L0: "bg-cyan-500/15 text-cyan-300 border-cyan-500/30",
  L1: "bg-blue-500/15 text-blue-300 border-blue-500/30",
  L2: "bg-emerald-500/15 text-emerald-300 border-emerald-500/30",
  L3: "bg-purple-500/15 text-purple-300 border-purple-500/30",
  LOOP: "bg-amber-500/15 text-amber-300 border-amber-500/30",
};

function fmtTs(iso: string): string {
  return new Date(iso).toLocaleString("en-US", { hour12: false });
}

function fmtNum(n: number): string {
  return new Intl.NumberFormat("en-US").format(n);
}

export function RequestExplorer() {
  const searchParams = useSearchParams();
  const [q, setQ] = useState<Query>(() => ({
    ...EMPTY,
    sessionId: searchParams?.get("sessionId") || "",
  }));
  const [page, setPage] = useState(1);
  const [rows, setRows] = useState<Row[]>([]);
  const [agg, setAgg] = useState<Aggregate>({ count: 0, tokensSaved: 0, totalCostSaved: 0 });
  const [totalPages, setTotalPages] = useState(0);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [lastFetchMs, setLastFetchMs] = useState<number | null>(null);

  const buildQs = useCallback(() => {
    const sp = new URLSearchParams();
    if (q.agent) sp.set("agent", q.agent);
    if (q.model) sp.set("model", q.model);
    if (q.provider) sp.set("provider", q.provider);
    if (q.level) sp.set("level", q.level);
    if (q.minTokens) sp.set("minTokens", q.minTokens);
    if (q.minDuration) sp.set("minDuration", q.minDuration);
    if (q.minCost) sp.set("minCost", q.minCost);
    if (q.virtualKey) sp.set("virtualKey", q.virtualKey);
    if (q.from) sp.set("from", q.from);
    if (q.to) sp.set("to", q.to);
    if (q.sessionId) sp.set("sessionId", q.sessionId);
    sp.set("page", String(page));
    sp.set("limit", "50");
    return sp.toString();
  }, [q, page]);

  const fetchRows = useCallback(async () => {
    setLoading(true);
    const t0 = performance.now();
    try {
      const r = await fetch(`/api/explorer?${buildQs()}`, { cache: "no-store" });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const j = await r.json();
      setRows(j.rows || []);
      setTotalPages(j.pagination?.totalPages || 0);
      setTotal(j.pagination?.total || 0);
      setAgg(j.aggregate || { count: 0, tokensSaved: 0, totalCostSaved: 0 });
      setLastFetchMs(performance.now() - t0);
    } catch (e) {
      console.error("[explorer] fetch failed:", e);
    } finally {
      setLoading(false);
    }
  }, [buildQs]);

  useEffect(() => {
    fetchRows();
  }, [fetchRows]);

  const reset = () => {
    setQ(EMPTY);
    setPage(1);
  };

  return (
    <div className="rounded-2xl bg-black/60 border border-white/10 overflow-hidden">
      <div className="border-b border-white/10 bg-black/40 px-4 py-3 space-y-3">
        <div className="flex items-center justify-between flex-wrap gap-3">
          <div className="flex items-center gap-3">
            <Search className="w-4 h-4 text-blue-400" />
            <h2 className="text-sm font-black uppercase tracking-widest text-zinc-300">
              Request Explorer
            </h2>
            <span className="text-[10px] text-zinc-500 font-mono">
              {total} matches {"\u00b7"} {fmtNum(agg.tokensSaved)} tokens saved {"\u00b7"} ${agg.totalCostSaved.toFixed(4)} total
              {lastFetchMs !== null && (
                <span className="text-zinc-600 ml-2">{"\u00b7"} {lastFetchMs.toFixed(0)}ms</span>
              )}
            </span>
          </div>
          <button
            type="button"
            onClick={reset}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-bold bg-white/5 text-zinc-300 border border-white/10 hover:bg-white/10"
          >
            <RotateCcw className="w-3 h-3" />
            Reset
          </button>
        </div>

        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
          <FilterInput icon={<Search className="w-3 h-3" />} label="Agent" value={q.agent} onChange={(v) => { setQ({ ...q, agent: v }); setPage(1); }} placeholder="hermes / openclaw / claude-code" />
          <FilterInput icon={<Hash className="w-3 h-3" />} label="Model" value={q.model} onChange={(v) => { setQ({ ...q, model: v }); setPage(1); }} placeholder="MiniMax-M3" />
          <FilterInput icon={<Database className="w-3 h-3" />} label="Provider" value={q.provider} onChange={(v) => { setQ({ ...q, provider: v }); setPage(1); }} placeholder="minimax" />
          <FilterInput icon={<FilterIcon className="w-3 h-3" />} label="Cache levels" value={q.level} onChange={(v) => { setQ({ ...q, level: v }); setPage(1); }} placeholder="L1,L2,NONE" />
          <FilterInput icon={<Hash className="w-3 h-3" />} label="Min tokens" value={q.minTokens} onChange={(v) => { setQ({ ...q, minTokens: v }); setPage(1); }} placeholder="1000" type="number" />
          <FilterInput icon={<Clock className="w-3 h-3" />} label="Min duration (ms)" value={q.minDuration} onChange={(v) => { setQ({ ...q, minDuration: v }); setPage(1); }} placeholder="2000" type="number" />
                  <FilterInput icon={<Hash className="w-3 h-3" />} label="Virtual key prefix" value={q.virtualKey} onChange={(v) => { setQ({ ...q, virtualKey: v }); setPage(1); }} placeholder="sk-opti-..." />
          <FilterInput icon={<Clock className="w-3 h-3" />} label="From (ISO)" value={q.from} onChange={(v) => { setQ({ ...q, from: v }); setPage(1); }} placeholder="2026-06-15T00:00:00Z" />
          <FilterInput icon={<Search className="w-3 h-3" />} label="Session ID" value={q.sessionId} onChange={(v) => { setQ({ ...q, sessionId: v }); setPage(1); }} placeholder="sess_..." />
        </div>
      </div>

      {/* Results table */}
      <div className="overflow-auto" style={{ maxHeight: 540 }}>
        <table className="w-full text-xs font-mono">
          <thead className="bg-black/40 sticky top-0 z-10">
            <tr className="text-left text-[10px] uppercase tracking-wider text-zinc-500">
              <th className="px-3 py-2">Time</th>
              <th className="px-3 py-2">Level</th>
              <th className="px-3 py-2">Model</th>
              <th className="px-3 py-2">Agent</th>
              <th className="px-3 py-2 text-right">Tokens in</th>
              <th className="px-3 py-2 text-right">Tokens out</th>
              <th className="px-3 py-2 text-right">Latency</th>
              <th className="px-3 py-2 text-right">$ saved</th>
              <th className="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.04]">
            {loading && rows.length === 0 && (
              <tr>
                <td colSpan={9} className="px-3 py-6 text-center text-zinc-500">Loading{"\u2026"}</td>
              </tr>
            )}
            {!loading && rows.length === 0 && (
              <tr>
                <td colSpan={9} className="px-3 py-6 text-center text-zinc-500">No matches.</td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="hover:bg-white/[0.03] cursor-pointer" onClick={() => setSelectedId(r.id)}>
                <td className="px-3 py-1.5 text-zinc-500 whitespace-nowrap">{fmtTs(r.createdAt)}</td>
                <td className="px-3 py-1.5">
                  <span className={`px-1.5 py-0.5 rounded text-[10px] font-bold border ${CACHE_COLORS[r.cacheLevel] || CACHE_COLORS.NONE}`}>
                    {r.cacheLevel || "MISS"}
                  </span>
                </td>
                <td className="px-3 py-1.5 text-zinc-200 truncate max-w-[200px]">{r.model || "\u2014"}</td>
                <td className="px-3 py-1.5 text-zinc-400 truncate max-w-[180px]">{r.agentLabel || r.agentId || "\u2014"}</td>
                <td className="px-3 py-1.5 text-right text-zinc-300 tabular-nums">
                  {fmtNum(r.promptTokensOrig)}<span className="text-zinc-600"> {"\u2192"} </span>{fmtNum(r.promptTokensOpt)}
                </td>
                <td className="px-3 py-1.5 text-right text-zinc-300 tabular-nums">
                  {fmtNum(r.completionTokensOrig)}<span className="text-zinc-600"> {"\u2192"} </span>{fmtNum(r.completionTokensOpt)}
                </td>
                <td className="px-3 py-1.5 text-right text-zinc-400 tabular-nums">
                  {r.durationMs != null ? `${r.durationMs}ms` : "\u2014"}
                </td>
                <td className="px-3 py-1.5 text-right text-emerald-400 tabular-nums font-bold">
                  ${r.costSaved.toFixed(5)}
                </td>
                <td className="px-3 py-1.5 text-right">
                  <Eye className="w-3 h-3 text-zinc-500 inline" />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="border-t border-white/10 bg-black/40 px-4 py-2 flex items-center justify-between text-xs">
          <span className="text-zinc-500 font-mono">
            Page {page} / {totalPages}
          </span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setPage(Math.max(1, page - 1))}
              disabled={page === 1}
              className="flex items-center gap-1 px-2 py-1 rounded bg-white/5 text-zinc-300 border border-white/10 hover:bg-white/10 disabled:opacity-30 disabled:cursor-not-allowed"
            >
              <ChevronLeft className="w-3 h-3" />
              Prev
            </button>
            <button
              type="button"
              onClick={() => setPage(Math.min(totalPages, page + 1))}
              disabled={page === totalPages}
              className="flex items-center gap-1 px-2 py-1 rounded bg-white/5 text-zinc-300 border border-white/10 hover:bg-white/10 disabled:opacity-30 disabled:cursor-not-allowed"
            >
              Next
              <ChevronRight className="w-3 h-3" />
            </button>
          </div>
        </div>
      )}

      <AnimatePresence>
        {selectedId && (
          <RequestDetailDrawer
            id={selectedId}
            onClose={() => setSelectedId(null)}
          />
        )}
      </AnimatePresence>
    </div>
  );
}

function FilterInput({
  icon,
  label,
  value,
  onChange,
  placeholder,
  type = "text",
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
}) {
  return (
    <div>
      <label className="flex items-center gap-1 text-[9px] font-black uppercase tracking-wider text-zinc-500 mb-0.5">
        {icon}
        {label}
      </label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full bg-white/5 border border-white/10 rounded-md px-2 py-1 text-xs text-zinc-200 placeholder-zinc-600 outline-none focus:border-blue-500/50 font-mono"
      />
    </div>
  );
}

// Slide-in drawer from the right with the full row + payload preview.
function RequestDetailDrawer({ id, onClose }: { id: string; onClose: () => void }) {
  const router = useRouter();
  const [row, setRow] = useState<any | null>(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState<"summary" | "original" | "optimized" | "response">("summary");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetch(`/api/explorer/${id}`, { cache: "no-store" })
      .then((r) => r.json())
      .then((j) => {
        if (!cancelled) {
          setRow(j.row || null);
          setLoading(false);
        }
      })
      .catch(() => setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [id]);

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex justify-end"
      onClick={onClose}
    >
      <motion.div
        initial={{ x: "100%" }}
        animate={{ x: 0 }}
        exit={{ x: "100%" }}
        transition={{ type: "spring", damping: 25, stiffness: 200 }}
        className="w-full max-w-3xl h-full bg-[#0a0a0a] border-l border-white/10 overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="sticky top-0 z-10 bg-[#0a0a0a] border-b border-white/10 px-5 py-3 flex items-center justify-between">
          <h3 className="text-sm font-black uppercase tracking-widest text-zinc-200">
            Request detail
          </h3>
          <button type="button" onClick={onClose} className="p-1 rounded text-zinc-500 hover:text-white hover:bg-white/5">
            <X className="w-4 h-4" />
          </button>
        </div>

        {loading && (
          <div className="p-6 text-zinc-500 text-sm">Loading{"\u2026"}</div>
        )}
        {!loading && !row && (
          <div className="p-6 text-zinc-500 text-sm">Row not found.</div>
        )}
        {!loading && row && (
          <div className="p-5 space-y-4 text-xs">
            <div className="flex flex-wrap gap-2">
              <Pill k="ID" v={row.id} />
              <Pill k="Time" v={fmtTs(row.createdAt)} />
              <Pill k="Level" v={row.cacheLevel || "MISS"} highlight />
              <Pill k="Provider" v={row.provider} />
              <Pill k="Model" v={row.model} />
              {row.agentLabel && <Pill k="Agent" v={row.agentLabel} />}
              {row.sessionId && <Pill k="Session" v={row.sessionId} />}
            </div>

            <button
              type="button"
              onClick={() => router.push(`/playground?forkRequestId=${row.id}`)}
              className="w-full flex items-center justify-center gap-2 py-2.5 px-4 rounded-xl bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-400 hover:to-teal-500 text-black font-black text-xs transition-all shadow-lg shadow-emerald-500/10"
            >
              <Sparkles className="w-3.5 h-3.5" /> Fork this step in Playground
            </button>

            <div className="grid grid-cols-2 gap-2 text-xs">
              <Stat label="Tokens in" value={`${fmtNum(row.promptTokensOrig)} \u2192 ${fmtNum(row.promptTokensOpt)}`} />
              <Stat label="Tokens out" value={`${fmtNum(row.completionTokensOrig)} \u2192 ${fmtNum(row.completionTokensOpt)}`} />
              <Stat label="Latency" value={row.durationMs != null ? `${row.durationMs}ms` : "\u2014"} />
              <Stat label="$ saved" value={`$${Number(row.costSaved).toFixed(5)}`} accent />
              <Stat label="Cache read" value={fmtNum(row.cacheReadTokens ?? 0)} />
              <Stat label="Cache write" value={fmtNum(row.cacheCreationTokens ?? 0)} />
            </div>

            <div className="flex items-center gap-1 border-b border-white/10">
              {(["summary", "original", "optimized", "response"] as const).map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => setTab(t)}
                  className={`px-3 py-1.5 text-xs font-bold uppercase tracking-wider transition ${
                    tab === t ? "text-emerald-300 border-b-2 border-emerald-300" : "text-zinc-500 hover:text-zinc-300"
                  }`}
                >
                  {t}
                </button>
              ))}
            </div>

            <div className="bg-black/40 border border-white/10 rounded-lg p-3 max-h-[60vh] overflow-auto">
              {tab === "summary" && (
                <pre className="text-[11px] text-zinc-300 font-mono whitespace-pre-wrap break-all leading-relaxed">
{JSON.stringify(
  Object.fromEntries(
    Object.entries(row).filter(([k]) =>
      [
        "id", "cacheLevel", "provider", "model", "createdAt",
        "promptTokensOrig", "promptTokensOpt",
        "completionTokensOrig", "completionTokensOpt",
        "cacheReadTokens", "cacheCreationTokens", "cacheHitTokens", "cacheMissTokens",
        "costSaved", "durationMs", "agentId", "agentLabel", "sessionId",
      ].includes(k)
    )
  ),
  null,
  2
)}
                </pre>
              )}
              {(tab === "original" || tab === "optimized" || tab === "response") && (
                <PayloadBlock
                  text={
                    tab === "original" ? row.originalPayload :
                    tab === "optimized" ? row.optimizedPayload :
                    row.responsePayload
                  }
                />
              )}
            </div>
          </div>
        )}
      </motion.div>
    </motion.div>
  );
}

function Pill({ k, v, highlight }: { k: string; v: string; highlight?: boolean }) {
  return (
    <span className={`px-2 py-0.5 rounded text-[10px] font-bold border ${highlight ? "bg-emerald-500/10 text-emerald-300 border-emerald-500/30" : "bg-white/5 text-zinc-300 border-white/10"}`}>
      <span className="text-zinc-500 font-mono mr-1">{k}:</span> {v}
    </span>
  );
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="p-2 rounded-lg bg-black/30 border border-white/10">
      <div className="text-[9px] uppercase tracking-wider text-zinc-500">{label}</div>
      <div className={`text-sm font-mono font-bold ${accent ? "text-emerald-300" : "text-zinc-200"}`}>{value}</div>
    </div>
  );
}

function PayloadBlock({ text }: { text: string | null | undefined }) {
  const [copied, setCopied] = useState(false);
  if (!text) {
    return <div className="text-zinc-600 text-xs italic">No payload recorded (zero-log mode? bypass?).</div>;
  }
  let pretty: string;
  try {
    pretty = JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    pretty = text;
  }
  return (
    <div className="relative">
      <button
        type="button"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(pretty);
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
          } catch {}
        }}
        className="absolute top-2 right-2 flex items-center gap-1 text-[10px] font-bold text-zinc-400 hover:text-white px-2 py-1 rounded bg-black/40 border border-white/10"
      >
        {copied ? "Copied" : "Copy"}
      </button>
      <pre className="text-[11px] text-zinc-200 font-mono whitespace-pre-wrap break-all leading-relaxed">
        {pretty}
      </pre>
    </div>
  );
}
