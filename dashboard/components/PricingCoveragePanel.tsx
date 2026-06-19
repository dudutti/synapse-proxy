"use client";

import React, { useEffect, useState, useCallback } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { DollarSign, AlertTriangle, Plus, X, Check, Database, Zap, ArrowRight } from "lucide-react";

// Suggestion table for the "real" pricing of well-known models.
// When the SUPERADMIN clicks "Fix this" on a gap, we pre-fill the
// cost fields with a sensible default that they can adjust.
const PRICING_SUGGESTIONS: Record<string, { prompt: number; completion: number; cachedIn?: number; cacheWrite?: number }> = {
  "google:gemini-2.5-flash":            { prompt: 0.30, completion: 2.50, cachedIn: 0.075, cacheWrite: 0.30 },
  "google:gemini-3-flash":               { prompt: 0.30, completion: 2.50, cachedIn: 0.075, cacheWrite: 0.30 },
  "google:gemini-3-pro":                 { prompt: 1.25, completion: 5.00, cachedIn: 0.31, cacheWrite: 1.25 },
  "anthropic:claude-haiku-3.5":          { prompt: 0.80, completion: 4.00, cachedIn: 0.08, cacheWrite: 1.00 },
  "anthropic:claude-sonnet-4":           { prompt: 3.00, completion: 15.00, cachedIn: 0.30, cacheWrite: 3.75 },
  "anthropic:claude-opus-4":             { prompt: 15.00, completion: 75.00, cachedIn: 1.50, cacheWrite: 18.75 },
  "openai:gpt-4o":                       { prompt: 2.50, completion: 10.00, cachedIn: 1.25, cacheWrite: 2.50 },
  "openai:gpt-4o-mini":                  { prompt: 0.15, completion: 0.60, cachedIn: 0.075, cacheWrite: 0.15 },
  "openai:o1":                           { prompt: 15.00, completion: 60.00 },
  "openai:o1-mini":                      { prompt: 1.10, completion: 4.40 },
  "minimax:MiniMax-M3":                  { prompt: 0.30, completion: 1.20, cachedIn: 0.06, cacheWrite: 0.30 },
  "minimax:MiniMax-M2.7":                { prompt: 0.25, completion: 1.00, cachedIn: 0.05, cacheWrite: 0.25 },
  "deepseek:deepseek-v3":                { prompt: 0.27, completion: 1.10, cachedIn: 0.07, cacheWrite: 0.27 },
  "mistral:mistral-large":               { prompt: 2.00, completion: 6.00 },
};

const DEFAULT_PRICING = { prompt: 1.00, completion: 3.00 };

function lookupSuggestion(provider: string, model: string): { prompt: number; completion: number; cachedIn?: number; cacheWrite?: number } {
  const key = `${provider}:${model}`;
  if (PRICING_SUGGESTIONS[key]) return PRICING_SUGGESTIONS[key];
  // Fuzzy: try provider-only match
  const byPrefix = Object.entries(PRICING_SUGGESTIONS).find(([k]) =>
    k.startsWith(provider + ":") && model.toLowerCase().includes(k.split(":")[1].toLowerCase().split("-")[0])
  );
  if (byPrefix) return byPrefix[1];
  return DEFAULT_PRICING;
}

type Gap = {
  provider: string;
  model: string;
  requestCount: number;
  totalTokens: number;
  fallbackDollars: number;
};

export function PricingCoveragePanel() {
  const [gaps, setGaps] = useState<Gap[]>([]);
  const [knownCount, setKnownCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Gap | null>(null);
  const [lastFixed, setLastFixed] = useState<string | null>(null);

  const fetchAll = useCallback(async () => {
    setLoading(true);
    try {
      const r = await fetch("/api/admin/pricing-coverage", { cache: "no-store" });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const j = await r.json();
      setGaps(j.gaps || []);
      setKnownCount(j.knownModelCount || 0);
    } catch (e) {
      console.error("[pricing-coverage]", e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAll();
    const id = setInterval(fetchAll, 30000);
    return () => clearInterval(id);
  }, [fetchAll]);

  const totalFallback = gaps.reduce((acc, g) => acc + g.fallbackDollars, 0);
  const totalRequests = gaps.reduce((acc, g) => acc + g.requestCount, 0);

  const activateAll = async () => {
    if (gaps.length === 0) return;
    if (!confirm(`Apply pricing suggestions to all ${gaps.length} gaps? This will overwrite anything you've manually set.`)) return;
    setLoading(true);
    for (const g of gaps) {
      const sugg = lookupSuggestion(g.provider, g.model);
      await fetch("/api/admin/pricing-coverage", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          provider: g.provider,
          modelName: g.model,
          costPromptPer1M: sugg.prompt,
          costCompletionPer1M: sugg.completion,
          costCachedInputPer1M: sugg.cachedIn ?? null,
          costCacheWritePer1M: sugg.cacheWrite ?? null,
        }),
      });
    }
    fetchAll();
  };

  return (
    <div className="rounded-2xl bg-black/60 border border-white/10 overflow-hidden">
      <div className="border-b border-white/10 bg-black/40 px-4 py-3 flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <DollarSign className="w-4 h-4 text-amber-400" />
          <h2 className="text-sm font-black uppercase tracking-widest text-zinc-300">
            Pricing Coverage
          </h2>
          {gaps.length > 0 && (
            <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-amber-500/15 text-amber-300 border border-amber-500/30">
              {gaps.length} gap{gaps.length > 1 ? "s" : ""}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {gaps.length > 0 && (
            <button
              type="button"
              onClick={activateAll}
              disabled={loading}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 hover:bg-emerald-500/25 disabled:opacity-50 transition"
              title="Apply pricing suggestions to every gap at once. Use this after reviewing the suggestion table to make sure the values match your negotiated prices."
            >
              <Zap className="w-3 h-3" />
              Activate all suggestions
            </button>
          )}
          <div className="text-[10px] text-zinc-500 font-mono whitespace-nowrap">
            {knownCount} priced · {gaps.length} unpriced · {totalRequests.toLocaleString()} reqs · ~${totalFallback.toFixed(4)} at fallback
          </div>
        </div>
      </div>

      {gaps.length === 0 ? (
        <div className="px-4 py-8 text-center">
          <Check className="w-8 h-8 mx-auto text-emerald-400 mb-2" />
          <p className="text-sm font-bold text-emerald-300">All (provider, model) pairs are priced</p>
          <p className="text-[11px] text-zinc-500 mt-1">No $1/MTok fallback leaks.</p>
        </div>
      ) : (
        <div className="divide-y divide-white/5">
          {loading && gaps.length === 0 ? (
            <div className="px-4 py-6 text-center text-zinc-500 text-xs">Loading…</div>
          ) : (
            gaps.map((g) => (
              <GapRow
                key={`${g.provider}:${g.model}`}
                gap={g}
                onFix={() => setEditing(g)}
                onFixed={(id) => {
                  setLastFixed(id);
                  fetchAll();
                  setTimeout(() => setLastFixed(null), 3000);
                }}
                justFixed={lastFixed === `${g.provider}:${g.model}`}
              />
            ))
          )}
        </div>
      )}

      <AnimatePresence>
        {editing && (
          <FixModal
            gap={editing}
            onClose={() => setEditing(null)}
            onFixed={() => {
              setEditing(null);
              fetchAll();
            }}
          />
        )}
      </AnimatePresence>
    </div>
  );
}

function GapRow({
  gap,
  onFix,
  onFixed,
  justFixed,
}: {
  gap: Gap;
  onFix: () => void;
  onFixed: (key: string) => void;
  justFixed: boolean;
}) {
  const key = `${gap.provider}:${gap.model}`;
  return (
    <motion.div
      initial={false}
      animate={justFixed ? { backgroundColor: ["rgba(52,211,153,0.15)", "rgba(0,0,0,0)"] } : {}}
      transition={{ duration: 1.5 }}
      className="px-4 py-3 flex items-center gap-4 hover:bg-white/[0.02]"
    >
      <AlertTriangle className="w-4 h-4 text-amber-400 flex-shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <span className="text-sm font-bold text-zinc-200 truncate">{gap.model}</span>
          <span className="text-[10px] font-mono text-zinc-500 px-1.5 py-0.5 rounded bg-white/5 border border-white/10">
            {gap.provider}
          </span>
          <span className="text-[10px] text-zinc-500 font-mono">
            · {gap.requestCount.toLocaleString()} reqs · {gap.totalTokens.toLocaleString()} tokens
          </span>
        </div>
        <div className="text-[11px] text-amber-300/80 font-mono">
          Currently billing at $1.00/MTok fallback
        </div>
      </div>
      <div className="text-right">
        <div className="text-xs font-bold text-amber-300 tabular-nums">
          ${gap.fallbackDollars.toFixed(4)}
        </div>
        <div className="text-[10px] text-zinc-600">fallback cost</div>
      </div>
      <button
        type="button"
        onClick={onFix}
        className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 hover:bg-emerald-500/25"
      >
        <Plus className="w-3 h-3" />
        Fix this
      </button>
    </motion.div>
  );
}

function FixModal({
  gap,
  onClose,
  onFixed,
}: {
  gap: Gap;
  onClose: () => void;
  onFixed: () => void;
}) {
  const sugg = lookupSuggestion(gap.provider, gap.model);
  const [costPrompt, setCostPrompt] = useState(sugg.prompt);
  const [costCompletion, setCostCompletion] = useState(sugg.completion);
  const [costCachedIn, setCostCachedIn] = useState<number | "">(sugg.cachedIn ?? "");
  const [costCacheWrite, setCostCacheWrite] = useState<number | "">(sugg.cacheWrite ?? "");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Compute the cost the user *would have paid* with these real prices
  // vs the $1/MTok fallback. Pure token-share estimate.
  // (Real savings depend on prompt/output mix which we approximate 50/50.)
  const fallbackDollars = gap.totalTokens / 1_000_000;
  const realDollars =
    (gap.totalTokens * 0.5 / 1_000_000) * costPrompt +
    (gap.totalTokens * 0.5 / 1_000_000) * costCompletion;
  const difference = fallbackDollars - realDollars;

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const res = await fetch("/api/admin/pricing-coverage", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          provider: gap.provider,
          modelName: gap.model,
          costPromptPer1M: costPrompt,
          costCompletionPer1M: costCompletion,
          costCachedInputPer1M: costCachedIn === "" ? null : Number(costCachedIn),
          costCacheWritePer1M: costCacheWrite === "" ? null : Number(costCacheWrite),
        }),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        setError(j.error || `HTTP ${res.status}`);
        return;
      }
      onFixed();
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-center justify-center p-4"
      onClick={onClose}
    >
      <motion.div
        initial={{ scale: 0.95, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        exit={{ scale: 0.95, opacity: 0 }}
        className="w-full max-w-lg bg-[#0a0a0a] border border-white/10 rounded-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="border-b border-white/10 bg-black/40 px-5 py-3 flex items-center justify-between">
          <div>
            <h3 className="text-sm font-black uppercase tracking-widest text-zinc-200">Fix pricing gap</h3>
            <p className="text-[11px] text-zinc-500 mt-0.5 font-mono">
              {gap.provider} / <span className="text-amber-300">{gap.model}</span>
            </p>
          </div>
          <button type="button" onClick={onClose} className="p-1 rounded text-zinc-500 hover:text-white hover:bg-white/5">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="p-5 space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <NumberField label="Cost input (per 1M tokens)" value={costPrompt} onChange={setCostPrompt} unit="$" accent />
            <NumberField label="Cost output (per 1M tokens)" value={costCompletion} onChange={setCostCompletion} unit="$" accent />
            <NumberField label="Cost cached input (optional)" value={costCachedIn} onChange={(v) => setCostCachedIn(v === null ? "" : v)} unit="$" />
            <NumberField label="Cost cache write (optional)" value={costCacheWrite} onChange={(v) => setCostCacheWrite(v === null ? "" : v)} unit="$" />
          </div>

          <div className="p-3 rounded-lg bg-black/40 border border-white/10 space-y-2">
            <div className="flex items-center justify-between text-xs">
              <span className="text-zinc-500">Fallback cost (at $1/MTok):</span>
              <span className="font-mono text-amber-300">${fallbackDollars.toFixed(4)}</span>
            </div>
            <div className="flex items-center justify-between text-xs">
              <span className="text-zinc-500">Real cost (with this pricing):</span>
              <span className="font-mono text-emerald-300">${realDollars.toFixed(4)}</span>
            </div>
            <div className="flex items-center justify-between text-xs border-t border-white/5 pt-2">
              <span className="text-zinc-400 font-bold">Difference:</span>
              <span className={`font-mono font-bold ${difference >= 0 ? "text-emerald-300" : "text-rose-300"}`}>
                {difference >= 0 ? "+" : ""}${difference.toFixed(4)}
              </span>
            </div>
            <p className="text-[10px] text-zinc-600">
              Approximation: 50/50 input/output split. The real savings depend on the
              actual mix of your traffic; refine the values above once you have more
              production data.
            </p>
          </div>

          {error && (
            <div className="px-3 py-2 rounded bg-rose-500/10 border border-rose-500/30 text-xs text-rose-300">
              {error}
            </div>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 py-1.5 rounded-lg text-xs font-bold bg-white/5 text-zinc-300 border border-white/10 hover:bg-white/10"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={submit}
              disabled={submitting}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 hover:bg-emerald-500/30 disabled:opacity-50"
            >
              {submitting ? "Saving…" : "Save pricing"}
              <ArrowRight className="w-3 h-3" />
            </button>
          </div>
        </div>
      </motion.div>
    </motion.div>
  );
}

function NumberField({
  label,
  value,
  onChange,
  unit,
  accent,
}: {
  label: string;
  value: number | "";
  onChange: (v: number | null) => void;
  unit?: string;
  accent?: boolean;
}) {
  return (
    <div>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-zinc-500 mb-1">{label}</label>
      <div className="relative">
        <input
          type="number"
          step="0.01"
          min="0"
          value={value}
          onChange={(e) => {
            const v = e.target.value;
            if (v === "") onChange("");
            else onChange(Number(v));
          }}
          className={`w-full bg-white/5 border border-white/10 rounded-lg px-3 py-1.5 pr-8 text-sm text-zinc-200 outline-none focus:border-emerald-500/50 font-mono ${accent ? "border-emerald-500/30" : ""}`}
        />
        {unit && (
          <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-zinc-500 font-bold pointer-events-none">{unit}</span>
        )}
      </div>
    </div>
  );
}
