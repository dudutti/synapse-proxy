"use client";

import { useMemo } from "react";

// Compact one-line stats footer for a single assistant bubble.
// Shows: cache level badge, tokens in/out, latency, cost saved.
export interface BubbleStats {
  cacheLevel?: string;
  tokensIn?: number;
  tokensOut?: number;
  costSaved?: number;
  costWithout?: number;
  costWith?: number;
  latencyMs?: number;
}

const CACHE_COLORS: Record<string, string> = {
  L0: "bg-cyan-500/20 text-cyan-300 border-cyan-500/30",
  L1: "bg-blue-500/20 text-blue-300 border-blue-500/30",
  L2: "bg-emerald-500/20 text-emerald-300 border-emerald-500/30",
  L3: "bg-purple-500/20 text-purple-300 border-purple-500/30",
  LOOP: "bg-amber-500/20 text-amber-300 border-amber-500/30",
  NONE: "bg-zinc-700/40 text-zinc-400 border-zinc-600/30",
};

export function MessageStats({ stats, isControl }: { stats: BubbleStats; isControl: boolean }) {
  const lvl = stats.cacheLevel || "NONE";
  const colorCls = CACHE_COLORS[lvl] || CACHE_COLORS.NONE;
  const hasSavings = (stats.costSaved || 0) > 0;

  return (
    <div className="mt-2 pt-2 border-t border-white/5 flex flex-wrap items-center gap-2 text-[10px] font-mono">
      <span className={`rounded px-1.5 py-0.5 border font-bold uppercase ${colorCls}`}>
        {lvl}
      </span>
      <span className="text-zinc-400">
        <span className="text-zinc-500">in</span>{" "}
        <span className="text-white">{(stats.tokensIn ?? 0).toLocaleString()}</span>
      </span>
      <span className="text-zinc-400">
        <span className="text-zinc-500">out</span>{" "}
        <span className="text-white">{(stats.tokensOut ?? 0).toLocaleString()}</span>
      </span>
      {stats.latencyMs !== undefined && stats.latencyMs !== null && (
        <span className="text-zinc-400">
          <span className="text-zinc-500">⏱</span>{" "}
          <span className="text-white">{stats.latencyMs}ms</span>
        </span>
      )}
      {!isControl && hasSavings && (
        <span className="text-emerald-400 font-bold">
          −${(stats.costSaved || 0).toFixed(5)}
        </span>
      )}
      {isControl && (stats.costWithout || 0) > 0 && (
        <span className="text-zinc-400">
          <span className="text-zinc-500">$</span>{(stats.costWithout || 0).toFixed(5)}
        </span>
      )}
    </div>
  );
}

// A vs B comparison bar — appears under the side-by-side panels.
// Deltas: cost saved, latency difference, token reduction.
export function ComparisonBar({
  optiStats,
  ctrlStats,
  bypass,
}: {
  optiStats: BubbleStats | null;
  ctrlStats: BubbleStats | null;
  bypass: boolean;
}) {
  const data = useMemo(() => {
    if (!optiStats || !ctrlStats) return null;
    const costSavedOpti = optiStats.costSaved || 0;
    const costDirect = optiStats.costWithout || 0;
    const latencyOpti = optiStats.latencyMs || 0;
    const latencyDirect = ctrlStats.latencyMs || 0;
    const tokensOpti = (optiStats.tokensIn || 0) + (optiStats.tokensOut || 0);
    const tokensDirect = (ctrlStats.tokensIn || 0) + (ctrlStats.tokensOut || 0);
    return {
      costSavedOpti,
      costDirect,
      latencyOpti,
      latencyDirect,
      tokensOpti,
      tokensDirect,
      latencySaved: latencyDirect > 0 ? latencyDirect - latencyOpti : 0,
      tokenSaved: tokensDirect - tokensOpti,
    };
  }, [optiStats, ctrlStats]);

  if (!data) return null;

  return (
    <div className="mt-3 grid grid-cols-3 gap-2 text-xs">
      <DeltaCell
        label="Cost saved"
        optiValue={`$${data.costSavedOpti.toFixed(5)}`}
        ctrlValue={`$${data.costDirect.toFixed(5)}`}
        delta={
          data.costDirect > 0
            ? `${(((data.costDirect - data.costSavedOpti) / data.costDirect) * 100).toFixed(1)}% cheaper`
            : null
        }
        highlight={data.costSavedOpti > 0 ? "emerald" : "zinc"}
      />
      <DeltaCell
        label="Latency"
        optiValue={`${data.latencyOpti}ms`}
        ctrlValue={`${data.latencyDirect}ms`}
        delta={
          data.latencySaved > 0
            ? `${data.latencySaved}ms faster`
            : data.latencySaved < 0
            ? `${Math.abs(data.latencySaved)}ms slower`
            : null
        }
        highlight={data.latencySaved > 0 ? "emerald" : data.latencySaved < 0 ? "amber" : "zinc"}
      />
      <DeltaCell
        label="Tokens (in+out)"
        optiValue={data.tokensOpti.toLocaleString()}
        ctrlValue={data.tokensDirect.toLocaleString()}
        delta={
          data.tokenSaved > 0
            ? `${data.tokenSaved.toLocaleString()} fewer`
            : data.tokenSaved < 0
            ? `${Math.abs(data.tokenSaved).toLocaleString()} more`
            : null
        }
        highlight={data.tokenSaved > 0 ? "emerald" : data.tokenSaved < 0 ? "amber" : "zinc"}
      />
    </div>
  );
}

function DeltaCell({
  label,
  optiValue,
  ctrlValue,
  delta,
  highlight,
}: {
  label: string;
  optiValue: string;
  ctrlValue: string;
  delta: string | null;
  highlight: "emerald" | "amber" | "zinc";
}) {
  const cls = {
    emerald: "bg-emerald-500/10 border-emerald-500/30 text-emerald-300",
    amber: "bg-amber-500/10 border-amber-500/30 text-amber-300",
    zinc: "bg-zinc-700/30 border-zinc-600/30 text-zinc-400",
  }[highlight];
  return (
    <div className={`rounded-lg border p-2 ${cls}`}>
      <div className="text-[10px] uppercase tracking-wider opacity-70">{label}</div>
      <div className="flex items-baseline gap-2 mt-0.5">
        <span className="text-white font-mono">{optiValue}</span>
        <span className="text-zinc-500 text-[10px]">vs {ctrlValue}</span>
      </div>
      {delta && <div className="text-[10px] font-bold mt-0.5">{delta}</div>}
    </div>
  );
}
