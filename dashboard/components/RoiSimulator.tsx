"use client";

import { useState, useMemo } from "react";
import GlowingCard from "./GlowingCard";
import { Coins, ShieldAlert, Cpu, ArrowRight } from "lucide-react";
import Link from "next/link";

const MODELS = [
  { id: "gpt-4o", name: "GPT-4o", input: 5.0, output: 15.0 },
  { id: "claude-3-5-sonnet", name: "Claude 3.5 Sonnet", input: 3.0, output: 15.0 },
  { id: "gpt-4-turbo", name: "GPT-4 Turbo", input: 10.0, output: 30.0 },
  { id: "gpt-3.5-turbo", name: "GPT-3.5 Turbo", input: 0.5, output: 1.5 },
];

export default function RoiSimulator() {
  const [model, setModel] = useState(MODELS[0]);
  const [millionsTokens, setMillionsTokens] = useState(50); // 50M tokens per month
  const [ratioInputOutput, setRatioInputOutput] = useState(80); // 80% input, 20% output

  // Assumptions
  const cacheHitRate = 0.30; // 30% cache hit rate (L1 + L2)
  const compressionRatio = 0.20; // 20% saved on remaining input via L3 compression

  const results = useMemo(() => {
    const inputM = millionsTokens * (ratioInputOutput / 100);
    const outputM = millionsTokens * ((100 - ratioInputOutput) / 100);

    const monthlyCostStandard = (inputM * model.input) + (outputM * model.output);
    const annualCostStandard = monthlyCostStandard * 12;

    // With Synapse Proxy
    // 1. Cache hits save 100% of input and output for those requests
    const inputAfterCache = inputM * (1 - cacheHitRate);
    const outputAfterCache = outputM * (1 - cacheHitRate);

    // 2. Compression saves on input
    const inputAfterCompression = inputAfterCache * (1 - compressionRatio);

    const monthlyCostSynapse = (inputAfterCompression * model.input) + (outputAfterCache * model.output);
    const annualCostSynapse = monthlyCostSynapse * 12;

    const monthlySaved = monthlyCostStandard - monthlyCostSynapse;
    const annualSaved = annualCostStandard - annualCostSynapse;
    const percentageSaved = monthlyCostStandard > 0 ? Math.round((monthlySaved / monthlyCostStandard) * 100) : 0;

    return {
      monthlyCostStandard,
      annualCostStandard,
      monthlyCostSynapse,
      annualCostSynapse,
      monthlySaved,
      annualSaved,
      percentageSaved
    };
  }, [model, millionsTokens, ratioInputOutput]);

  return (
    <GlowingCard className="w-full max-w-4xl mx-auto p-8 lg:p-12 bg-black/60 backdrop-blur-2xl border-white/10 mt-12">
      <div className="text-center mb-10">
        <h2 className="text-3xl font-black text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 to-cyan-400 mb-4">
          Calculate Your ROI
        </h2>
        <p className="text-gray-400">See how much you could save with Synapse Proxy's advanced caching and compression.</p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-12">
        {/* Controls */}
        <div className="space-y-8">
          <div>
            <label className="block text-sm font-bold text-gray-300 mb-2 uppercase tracking-wider">Language Model</label>
            <div className="grid grid-cols-2 gap-3">
              {MODELS.map(m => (
                <button
                  key={m.id}
                  onClick={() => setModel(m)}
                  className={`p-3 rounded-xl border text-sm font-bold transition-all ${model.id === m.id ? "bg-emerald-500/20 border-emerald-500/50 text-emerald-400" : "bg-white/5 border-white/10 text-gray-500 hover:text-gray-300 hover:bg-white/10"}`}
                >
                  {m.name}
                </button>
              ))}
            </div>
            <div className="text-xs text-gray-500 mt-2 text-right">
              ${model.input}/1M in · ${model.output}/1M out
            </div>
          </div>

          <div>
            <div className="flex justify-between items-end mb-2">
              <label className="block text-sm font-bold text-gray-300 uppercase tracking-wider">Monthly Volume</label>
              <span className="text-emerald-400 font-mono font-bold">{millionsTokens} Million Tokens</span>
            </div>
            <input 
              type="range" 
              min="1" 
              max="1000" 
              value={millionsTokens} 
              onChange={(e) => setMillionsTokens(Number(e.target.value))}
              className="w-full h-2 bg-white/10 rounded-lg appearance-none cursor-pointer accent-emerald-500"
            />
          </div>

          <div>
            <div className="flex justify-between items-end mb-2">
              <label className="block text-sm font-bold text-gray-300 uppercase tracking-wider">Input vs Output Ratio</label>
              <span className="text-emerald-400 font-mono font-bold">{ratioInputOutput}% Input</span>
            </div>
            <input 
              type="range" 
              min="10" 
              max="90" 
              value={ratioInputOutput} 
              onChange={(e) => setRatioInputOutput(Number(e.target.value))}
              className="w-full h-2 bg-white/10 rounded-lg appearance-none cursor-pointer accent-cyan-500"
            />
            <div className="flex justify-between text-[10px] text-gray-500 mt-1 uppercase font-bold">
              <span>More Prompts</span>
              <span>More Generation</span>
            </div>
          </div>
        </div>

        {/* Results */}
        <div className="bg-black/50 rounded-3xl border border-white/5 p-8 flex flex-col justify-center relative overflow-hidden">
          <div className="absolute top-[-50px] right-[-50px] w-40 h-40 bg-emerald-500/20 rounded-full blur-[50px] pointer-events-none" />
          
          <div className="mb-8">
            <div className="text-gray-500 text-xs font-bold uppercase tracking-wider mb-1">Standard Annual Cost</div>
            <div className="text-3xl font-mono text-gray-400 line-through decoration-rose-500/50 decoration-2">
              ${results.annualCostStandard.toLocaleString(undefined, { maximumFractionDigits: 0 })}
            </div>
          </div>

          <div className="mb-8">
            <div className="text-emerald-500 text-xs font-bold uppercase tracking-wider mb-1">With Synapse Proxy</div>
            <div className="text-5xl font-black font-mono text-white flex items-baseline gap-3">
              ${results.annualCostSynapse.toLocaleString(undefined, { maximumFractionDigits: 0 })}
              <span className="text-lg text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 px-3 py-1 rounded-full">
                -{results.percentageSaved}%
              </span>
            </div>
          </div>

          <div className="pt-8 border-t border-white/10">
            <div className="text-cyan-400 text-sm font-bold uppercase tracking-wider mb-2">You Save Every Year:</div>
            <div className="text-4xl font-black text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 to-cyan-400">
              ${results.annualSaved.toLocaleString(undefined, { maximumFractionDigits: 0 })}
            </div>
          </div>

          <div className="mt-8">
            <Link href="/signup" className="w-full py-4 rounded-xl bg-white text-black font-black flex items-center justify-center gap-2 hover:bg-gray-200 transition-colors">
              Start Saving Now <ArrowRight className="w-5 h-5" />
            </Link>
          </div>
        </div>
      </div>
    </GlowingCard>
  );
}
