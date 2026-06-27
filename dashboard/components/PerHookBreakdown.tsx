"use client";

import React from "react";

type HookSavings = {
  bytes: number;
  tokens: number;
  count: number;
  hits?: number;
  zones?: number;
  toolsInjected?: number;
};

type HookSavingsMap = {
  logCompressor: HookSavings & { count: number };
  outputReducer: HookSavings & { count: number };
  ccrCache: HookSavings;
  tagProtector: HookSavings;
  synapseRetrieve: HookSavings;
};

const formatNumber = (n: number) =>
  new Intl.NumberFormat("en-US").format(n);
const formatBytes = (n: number) => {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(2)} MB`;
};

// PerHookBreakdown renders the per-hook savings panel.
// Each row shows the hook name + its key counters
// (bytes/tokens for compressors, counts for the
// others). Color-coded to match the legend used in
// GlobalCommandCenter (L1/L2 emerald, L3 purple).
export function PerHookBreakdown({ data }: { data?: HookSavingsMap }) {
  if (!data) {
    return (
      <div className="p-6 bg-white/5 border border-white/10 rounded-2xl">
        <h3 className="text-sm font-bold uppercase tracking-wider text-gray-400 mb-3">
          Per-Hook Breakdown
        </h3>
        <p className="text-gray-500 text-sm">Loading...</p>
      </div>
    );
  }

  const rows: Array<{
    name: string;
    color: string;
    primary: string;
    secondary?: string;
    count?: number;
  }> = [
    {
      name: "LogCompressor",
      color: "bg-purple-500",
      primary: `${formatBytes(data.logCompressor.bytes)} saved`,
      secondary: `${formatNumber(data.logCompressor.tokens)} tokens · ${formatNumber(data.logCompressor.count)} compressions`,
    },
    {
      name: "OutputReducer",
      color: "bg-blue-500",
      primary: `${formatBytes(data.outputReducer.bytes)} saved`,
      secondary: `${formatNumber(data.outputReducer.tokens)} tokens · ${formatNumber(data.outputReducer.count)} reductions`,
    },
    {
      name: "CCR Cache",
      color: "bg-emerald-400",
      primary: `${formatNumber(data.ccrCache.hits || 0)} hits`,
      secondary: `${formatBytes(data.ccrCache.bytes || 0)} served from cache`,
    },
    {
      name: "TagProtector",
      color: "bg-amber-500",
      primary: `${formatNumber(data.tagProtector.zones || 0)} zones`,
      secondary: "HTML / Markdown / CDATA protected",
    },
    {
      name: "Synapse Retrieve",
      color: "bg-cyan-400",
      primary: `${formatNumber(data.synapseRetrieve.toolsInjected || 0)} tools injected`,
      secondary: "synapse_retrieve tool available",
    },
  ];

  return (
    <div className="p-6 bg-white/5 border border-white/10 rounded-2xl">
      <h3 className="text-sm font-bold uppercase tracking-wider text-gray-400 mb-4">
        Per-Hook Breakdown
      </h3>
      <div className="space-y-4">
        {rows.map((r) => (
          <div key={r.name} className="flex items-start gap-3">
            <div className={`w-1 h-12 rounded-full ${r.color} mt-1`} />
            <div className="flex-1 min-w-0">
              <div className="text-xs font-bold text-gray-500 uppercase tracking-wider">
                {r.name}
              </div>
              <div className="text-white text-sm font-bold mt-0.5 truncate">
                {r.primary}
              </div>
              {r.secondary && (
                <div className="text-gray-400 text-xs mt-0.5 truncate">
                  {r.secondary}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}