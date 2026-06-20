"use client";

import React, { useEffect, useState, useCallback } from "react";
import { motion } from "framer-motion";
import { Flame, ChevronDown, ChevronUp, ZapOff, Clock } from "lucide-react";

type Row = {
  provider: string;
  model: string;
  payloadHash: string;
  bucketKey: string;
  hits: number;
  tokensOrig: number;
  tokensOpt: number;
  tokensSaved: number;
  costSaved: number;
  approxFallbackCost: number;
  promptPreview: string;
  lastSeenAt: string | null;
  l2Potential: number;
};

const WINDOWS: { label: string; hours: number }[] = [
  { label: "Last 24h", hours: 24 },
  { label: "Last 7d", hours: 24 * 7 },
  { label: "Last 30d", hours: 24 * 30 },
];

export function ExpensivePromptsPanel() {
  const [rows, setRows] = useState<Row[]>([]);
  const [loading, setLoading] = useState(true);
  const [windowHours, setWindowHours] = useState(24 * 30);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [meta, setMeta] = useState({
    totalHits: 0,
    totalFallbackCost: 0,
    totalL2Potential: 0,
    groupingMode: "payloadHash" as string,
  });

  const fetchRows = useCallback(async () => {
    setLoading(true);
    try {
      const r = await fetch(`/api/admin/expensive-prompts?windowHours=${windowHours}`, { cache: "no-store" });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const j = await r.json();
      setRows(j.rows || []);
      setMeta({
        totalHits: j.totalHits || 0,
        totalFallbackCost: j.totalFallbackCost || 0,
        totalL2Potential: j.totalL2Potential || 0,
        groupingMode: j.groupingMode || "model+agent+level",
      });
    } catch (e) {
      console.error("[expensive-prompts]", e);
    } finally {
      setLoading(false);
    }
  }, [windowHours]);

  useEffect(() => {
    fetchRows();
  }, [fetchRows]);

  return (
    <div className="rounded-2xl bg-black/60 border border-white/10 overflow-hidden">
      <div className="border-b border-white/10 bg-black/40 px-4 py-3 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Flame className="w-4 h-4 text-orange-400" />
          <h2 className="text-sm font-black uppercase tracking-widest text-zinc-300">
            Most Expensive Prompts
          </h2>
          <span className="text-[10px] text-zinc-500 font-mono flex items-center gap-2">
            <span>
              {rows.length} prompts {"\u00b7"} {meta.totalHits.toLocaleString()} total hits {"\u00b7"} ~${meta.totalFallbackCost.toFixed(2)} at $1/MTok fallback
            </span>
          </span>
        </div>
        <div className="flex items-center gap-1">
          {WINDOWS.map((w) => (
            <button
              key={w.hours}
              type="button"
              onClick={() => setWindowHours(w.hours)}
              className={`px-2 py-1 rounded text-[10px] font-bold uppercase tracking-wider transition ${
                windowHours === w.hours
                  ? "bg-orange-500/20 text-orange-300 border border-orange-500/30"
                  : "bg-white/5 text-zinc-500 border border-white/10 hover:text-zinc-300"
              }`}
            >
              <Clock className="w-3 h-3 inline mr-1" />
              {w.label}
            </button>
          ))}
        </div>
      </div>

      {meta.totalL2Potential > 0 && (
        <div className="px-4 py-2 bg-emerald-500/[0.04] border-b border-emerald-500/20 text-xs">
          <ZapOff className="w-3 h-3 inline mr-1 text-emerald-400" />
          <span className="text-zinc-300">
            <span className="font-bold text-emerald-300">${meta.totalL2Potential.toFixed(2)}</span> of potential L2 cache savings
            if these prompts were added to the semantic cache. With an 80% L2 hit rate, you'd save that amount on subsequent calls of the same prompt.
          </span>
        </div>
      )}

      <div className="overflow-auto" style={{ maxHeight: 600 }}>
        <table className="w-full text-xs">
          <thead className="bg-black/40 sticky top-0 z-10">
            <tr className="text-left text-[10px] uppercase tracking-wider text-zinc-500">
              <th className="px-3 py-2">#</th>
              <th className="px-3 py-2">Prompt</th>
              <th className="px-3 py-2">Model</th>
              <th className="px-3 py-2 text-right">Hits</th>
              <th className="px-3 py-2 text-right">Tokens</th>
              <th className="px-3 py-2 text-right">$ (fallback)</th>
              <th className="px-3 py-2 text-right">L2 potential</th>
              <th className="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-white/[0.04]">
            {loading && rows.length === 0 && (
               <tr><td colSpan={8} className="px-3 py-6 text-center text-zinc-500">Loading{"\u2026"}</td></tr>
            )}
            {!loading && rows.length === 0 && (
              <tr><td colSpan={8} className="px-3 py-6 text-center text-zinc-500">No data in this window.</td></tr>
            )}
            {rows.map((r, idx) => (
              <ExpensiveRow
                key={r.bucketKey}
                row={r}
                rank={idx + 1}
                expanded={expandedId === r.bucketKey}
                onToggle={() => setExpandedId(expandedId === r.bucketKey ? null : r.bucketKey)}
              />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ExpensiveRow({ row, rank, expanded, onToggle }: { row: Row; rank: number; expanded: boolean; onToggle: () => void }) {
  const hitRatio = row.tokensOrig > 0 ? (row.tokensSaved / row.tokensOrig) * 100 : 0;
  return (
    <>
      <tr className="hover:bg-white/[0.03] cursor-pointer" onClick={onToggle}>
        <td className="px-3 py-2 font-mono text-zinc-500">{rank}</td>
        <td className="px-3 py-2 max-w-md">
          <div className="text-zinc-300 font-mono text-[11px] truncate">
            {row.promptPreview.slice(0, 80) || "(empty)"}
          </div>
        </td>
        <td className="px-3 py-2 font-mono text-zinc-400 whitespace-nowrap">{row.model || "\u2014"}</td>
        <td className="px-3 py-2 text-right font-mono text-zinc-200 tabular-nums">{row.hits}</td>
        <td className="px-3 py-2 text-right font-mono text-zinc-300 tabular-nums">
          {row.tokensOrig.toLocaleString()}
          <span className="text-zinc-600"> / </span>
          <span className="text-zinc-400">{hitRatio.toFixed(0)}%</span>
        </td>
        <td className="px-3 py-2 text-right font-mono text-amber-300 tabular-nums font-bold">
          ${row.approxFallbackCost.toFixed(3)}
        </td>
        <td className="px-3 py-2 text-right font-mono tabular-nums">
          {row.l2Potential > 0 ? (
            <span className="text-emerald-300 font-bold">+${row.l2Potential.toFixed(3)}</span>
          ) : (
            <span className="text-zinc-600">{"\u2014"}</span>
          )}
        </td>
        <td className="px-3 py-2 text-zinc-500">
          {expanded ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={8} className="bg-black/30 px-3 py-3">
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="space-y-2"
            >
              <div className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Full prompt preview</div>
              <pre className="text-[11px] text-zinc-300 font-mono whitespace-pre-wrap break-all leading-relaxed max-h-48 overflow-auto bg-black/40 p-3 rounded border border-white/10">
                {row.promptPreview}
              </pre>
              <div className="grid grid-cols-4 gap-2 text-[10px] text-zinc-500 font-mono">
                <div><span className="text-zinc-600">Provider:</span> <span className="text-zinc-300">{row.provider}</span></div>
                <div><span className="text-zinc-600">Hash:</span> <span className="text-zinc-300 font-mono">{row.payloadHash.slice(0, 12)}</span></div>
                <div><span className="text-zinc-600">Hits:</span> <span className="text-zinc-300">{row.hits}</span></div>
                <div><span className="text-zinc-600">Tokens:</span> <span className="text-zinc-300">{row.tokensOrig.toLocaleString()} / {row.tokensOpt.toLocaleString()}</span></div>
              </div>
              {row.lastSeenAt && (
                <div className="text-[10px] text-zinc-500 font-mono">
                  <span className="text-zinc-600">Last seen:</span> <span className="text-zinc-300">{new Date(row.lastSeenAt).toLocaleString()}</span>
                </div>
              )}
            </motion.div>
          </td>
        </tr>
      )}
    </>
  );
}
