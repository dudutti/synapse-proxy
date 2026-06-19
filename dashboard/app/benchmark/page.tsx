"use client";

import { useSession } from "next-auth/react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import Link from "next/link";
import { motion } from "framer-motion";
import { ArrowLeft, RefreshCw, Activity, ArrowRight, Settings } from "lucide-react";

interface BenchmarkLog {
  id: string;
  originalPrompt: string;
  optimizedPrompt: string;
  originalResponse: string;
  optimizedResponse: string;
  latencyOriginalMs: number;
  latencyOptimizedMs: number;
  promptTokensOrig: number;
  completionTokensOrig: number;
  promptTokensOpt: number;
  completionTokensOpt: number;
  aiReliabilityScore: number;
  aiFeedback: string;
  createdAt: string;
}

export default function BenchmarkPage() {
  const { data: session, status } = useSession();
  const router = useRouter();
  const [logs, setLogs] = useState<BenchmarkLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);

  useEffect(() => {
    if (status === "unauthenticated") {
      router.push("/login");
    } else if (status === "authenticated") {
      fetchLogs();
    }
  }, [status, router, page]);

  const fetchLogs = async () => {
    setLoading(true);
    const res = await fetch(`/api/benchmark?page=${page}&limit=5`);
    if (res.ok) {
      const data = await res.json();
      setLogs(data.data || []);
      if (data.pagination) {
        setTotalPages(data.pagination.totalPages);
      }
    }
    setLoading(false);
  };

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

  if (status === "loading" || loading) return <div className="min-h-screen bg-[#050505] text-white flex items-center justify-center">Loading...</div>;

  return (
    <div className="min-h-screen bg-[#050505] text-white p-8 font-sans relative overflow-hidden">
      {/* Background Orbs */}
      <div className="absolute top-[20%] left-[20%] w-[500px] h-[500px] bg-purple-500/10 rounded-full blur-[150px] pointer-events-none" />
      <div className="absolute bottom-[20%] right-[20%] w-[500px] h-[500px] bg-pink-500/10 rounded-full blur-[150px] pointer-events-none" />

      <motion.div 
        variants={containerVars}
        initial="hidden"
        animate="show"
        className="max-w-7xl mx-auto relative z-10"
      >
        <motion.header variants={itemVars} className="mb-10 flex justify-between items-center bg-white/5 border border-white/10 p-6 rounded-2xl backdrop-blur-xl shadow-2xl">
          <div className="flex items-center gap-4">
            <Link href="/" className="w-10 h-10 rounded-xl bg-white/5 hover:bg-white/10 transition border border-white/10 flex items-center justify-center text-gray-400 hover:text-white">
              <ArrowLeft className="w-5 h-5" />
            </Link>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-white">Live Benchmark</h1>
              <p className="text-gray-400 text-sm">A/B test cache quality in real-time</p>
            </div>
          </div>
          <div className="flex gap-4 items-center">
            <button onClick={fetchLogs} className="flex items-center gap-2 px-4 py-2 bg-white/5 text-white border border-white/10 rounded-xl hover:bg-white/10 transition text-sm">
              <RefreshCw className="w-4 h-4" /> Refresh
            </button>
            <Link href="/settings" className="flex items-center gap-2 px-4 py-2 bg-gradient-to-r from-emerald-500 to-teal-500 text-black rounded-xl hover:from-emerald-400 hover:to-teal-400 font-bold transition text-sm shadow-lg shadow-emerald-500/20">
              <Settings className="w-4 h-4" /> Settings
            </Link>
          </div>
        </motion.header>

        <motion.main variants={itemVars} className="space-y-12">
          {/* Benchmark mode warning: each request triggers 3 LLM calls (control + optimized + judge) */}
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            className="bg-gradient-to-br from-red-500/15 via-orange-500/10 to-amber-500/5 border-2 border-red-500/40 rounded-3xl p-6 shadow-[0_0_40px_rgba(239,68,68,0.15)] backdrop-blur-xl"
          >
            <div className="flex items-start gap-4">
              <div className="flex-shrink-0 w-12 h-12 rounded-full bg-red-500/20 border-2 border-red-500/50 flex items-center justify-center text-2xl shadow-[0_0_20px_rgba(239,68,68,0.4)]">
                ⚠️
              </div>
              <div className="flex-1">
                <h2 className="text-xl font-black text-red-300 mb-2 tracking-wide">
                  BENCHMARK MODE TRIPLES YOUR COSTS — DISABLE AFTER TESTING
                </h2>
                <p className="text-sm text-red-200/90 leading-relaxed mb-3">
                  Every request intercepted in benchmark mode fires <strong className="text-white">3 LLM calls</strong> instead of 1:
                </p>
                <ol className="text-sm text-red-200/80 space-y-1 ml-4 mb-3 list-decimal list-inside">
                  <li><strong className="text-red-300">Control call</strong> — the original request forwarded as-is to the upstream provider</li>
                  <li><strong className="text-red-300">Optimized call</strong> — the L3-compressed version of the same request</li>
                  <li><strong className="text-red-300">Judge LLM call</strong> — a third LLM that scores how similar the two responses are (0-100)</li>
                </ol>
                <div className="bg-black/40 border border-red-500/30 rounded-xl p-3 mt-3">
                  <p className="text-xs text-red-200 leading-relaxed">
                    <strong className="text-red-300">⚠️ Production warning:</strong> Benchmark mode is designed to <em>measure the quality</em> of Synapse Proxy's L3 compression on a sample of traffic.
                    It is <strong>not</strong> a runtime cost optimization. For every 1 real request, you pay for 3 LLM calls.
                    <strong className="block mt-1 text-white">Always disable benchmark mode after you've collected enough samples (typically 50-100 requests per model).</strong>
                  </p>
                </div>
                <p className="text-[10px] text-gray-500 mt-3 italic">
                  This page shows the side-by-side comparison and AI reliability score. The <code className="text-red-300">BenchmarkLogs</code> table in Postgres is the source of truth for production tuning.
                </p>
              </div>
            </div>
          </motion.div>

          {logs.length === 0 ? (
            <div className="text-center py-24 bg-white/5 rounded-3xl border border-white/10 backdrop-blur-xl">
              <Activity className="w-16 h-16 mx-auto mb-6 text-gray-500 opacity-50" />
              <h2 className="text-2xl font-bold text-gray-300">No benchmark logs yet.</h2>
              <p className="text-gray-500 mt-2">Enable Benchmark Mode on a virtual key and send a request to see A/B testing here.</p>
            </div>
          ) : (
            logs.map(log => (
              <motion.div 
                initial={{ opacity: 0, y: 20 }}
                whileInView={{ opacity: 1, y: 0 }}
                viewport={{ once: true, margin: "-100px" }}
                key={log.id} 
                className="bg-white/5 rounded-3xl border border-white/10 overflow-hidden shadow-2xl backdrop-blur-xl"
              >
                {/* Header with AI Score */}
                <div className="bg-black/20 p-6 border-b border-white/5 flex justify-between items-center relative overflow-hidden">
                  <div className={`absolute top-0 right-0 w-64 h-full blur-[60px] opacity-20 pointer-events-none ${log.aiReliabilityScore > 90 ? 'bg-emerald-500' : 'bg-amber-500'}`} />
                  <div className="relative z-10">
                    <h3 className="text-lg font-bold text-white">Request intercepted on {new Date(log.createdAt).toLocaleString()}</h3>
                    <p className="text-sm text-gray-400 mt-1">Prompt length: {log.originalPrompt.length} chars</p>
                  </div>
                  <div className="text-right relative z-10">
                    <div className="text-xs text-gray-400 uppercase tracking-widest font-bold mb-1">AI Reliability Score</div>
                    <div className={`text-5xl font-black ${log.aiReliabilityScore > 90 ? 'text-transparent bg-clip-text bg-gradient-to-r from-emerald-400 to-teal-400' : 'text-transparent bg-clip-text bg-gradient-to-r from-amber-400 to-orange-400'}`}>
                      {log.aiReliabilityScore}%
                    </div>
                  </div>
                </div>

                {/* Split Screen */}
                <div className="grid grid-cols-1 lg:grid-cols-2 divide-y lg:divide-y-0 lg:divide-x divide-white/5 relative">
                  
                  {/* VS Badge */}
                  <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 w-10 h-10 bg-black border border-white/10 rounded-full flex items-center justify-center text-gray-500 font-bold text-xs shadow-xl z-20 hidden lg:flex">
                    VS
                  </div>

                  {/* Unoptimized (Control) */}
                  <div className="p-8">
                    <h4 className="text-xl font-bold text-red-400 mb-6 flex items-center gap-3">
                      <span className="bg-red-500/20 text-red-300 px-3 py-1 rounded-lg text-xs tracking-wider border border-red-500/30 shadow-[0_0_15px_rgba(239,68,68,0.2)]">CONTROL</span> 
                      Without Synapse Proxy
                    </h4>
                    <div className="grid grid-cols-2 gap-4 mb-6">
                      <div className="bg-black/30 rounded-xl p-5 border border-white/5">
                        <div className="text-gray-500 text-xs uppercase tracking-wider font-bold mb-2">Input / Output Billed</div>
                        <div className="text-2xl font-mono text-gray-300">{log.promptTokensOrig} / {log.completionTokensOrig}</div>
                      </div>
                      <div className="bg-black/30 rounded-xl p-5 border border-white/5">
                        <div className="text-gray-500 text-xs uppercase tracking-wider font-bold mb-2">Latency</div>
                        <div className="text-2xl font-mono text-gray-300">{log.latencyOriginalMs}ms</div>
                      </div>
                    </div>
                    <details className="mb-4">
                      <summary className="text-xs font-bold text-gray-500 uppercase tracking-widest cursor-pointer hover:text-white transition flex items-center gap-3">
                        Show Raw Prompt
                        <span className="bg-white/10 text-gray-300 px-2 py-0.5 rounded-md text-[10px] normal-case">{log.promptTokensOrig} tokens</span>
                      </summary>
                      <div className="bg-black/50 p-5 rounded-xl border border-white/5 text-gray-400 font-mono text-sm whitespace-pre-wrap h-64 overflow-y-auto leading-relaxed mt-2">
                        {log.originalPrompt || "No prompt recorded"}
                      </div>
                    </details>
                    <div className="flex items-center gap-3 mb-2">
                      <div className="text-xs font-bold text-gray-500 uppercase tracking-widest">Raw Response</div>
                      <span className="bg-white/10 text-gray-300 px-2 py-0.5 rounded-md text-[10px]">{log.completionTokensOrig} tokens</span>
                    </div>
                    <div className="bg-black/50 p-5 rounded-xl border border-white/5 text-gray-400 font-mono text-sm whitespace-pre-wrap h-64 overflow-y-auto leading-relaxed">
                      {log.originalResponse || "No response recorded"}
                    </div>
                  </div>

                  {/* Optimized */}
                  <div className="p-8 bg-emerald-500/[0.02]">
                    <h4 className="text-xl font-bold text-emerald-400 mb-6 flex items-center gap-3">
                      <span className="bg-emerald-500/20 text-emerald-300 px-3 py-1 rounded-lg text-xs tracking-wider border border-emerald-500/30 shadow-[0_0_15px_rgba(16,185,129,0.2)]">TEST</span> 
                      With Synapse Proxy
                    </h4>
                    <div className="grid grid-cols-2 gap-4 mb-6">
                      <div className="bg-black/30 rounded-xl p-5 border border-emerald-500/10">
                        <div className="text-emerald-500/70 text-xs uppercase tracking-wider font-bold mb-2">Input / Output Billed</div>
                        <div className="text-2xl font-mono text-emerald-400">{log.promptTokensOpt} / {log.completionTokensOpt}</div>
                      </div>
                      <div className="bg-black/30 rounded-xl p-5 border border-emerald-500/10">
                        <div className="text-emerald-500/70 text-xs uppercase tracking-wider font-bold mb-2">Latency</div>
                        <div className="text-2xl font-mono text-emerald-400">{log.latencyOptimizedMs}ms</div>
                      </div>
                    </div>
                    <details className="mb-4">
                      <summary className="text-xs font-bold text-emerald-500/70 uppercase tracking-widest cursor-pointer hover:text-emerald-400 transition flex items-center gap-3">
                        Show Optimized Prompt
                        <span className="bg-emerald-500/20 text-emerald-300 px-2 py-0.5 rounded-md text-[10px] normal-case">{log.promptTokensOpt} tokens</span>
                      </summary>
                      <div className="bg-black/50 p-5 rounded-xl border border-emerald-500/10 text-gray-300 font-mono text-sm whitespace-pre-wrap h-64 overflow-y-auto leading-relaxed mt-2">
                        {log.optimizedPrompt || "No prompt recorded"}
                      </div>
                    </details>
                    <div className="flex items-center gap-3 mb-2">
                      <div className="text-xs font-bold text-gray-500 uppercase tracking-widest">Optimized Response</div>
                      <span className="bg-emerald-500/20 text-emerald-300 px-2 py-0.5 rounded-md text-[10px]">{log.completionTokensOpt} tokens</span>
                    </div>
                    <div className="bg-black/50 p-5 rounded-xl border border-emerald-500/10 text-gray-300 font-mono text-sm whitespace-pre-wrap h-64 overflow-y-auto leading-relaxed">
                      {log.optimizedResponse || "No response recorded"}
                    </div>
                  </div>
                </div>

                {/* AI Feedback Footer */}
                <div className="bg-black/40 p-6 border-t border-white/5 flex gap-4 items-start">
                  <div className="p-2 bg-white/5 rounded-lg border border-white/10 shrink-0">
                    <Activity className="w-5 h-5 text-gray-400" />
                  </div>
                  <div>
                    <h4 className="text-xs font-bold text-gray-500 uppercase tracking-widest mb-1">LLM Judge Feedback</h4>
                    <p className="text-gray-300 text-sm leading-relaxed">{log.aiFeedback}</p>
                  </div>
                </div>
              </motion.div>
            ))
          )}

          {/* Pagination Controls */}
          {totalPages > 1 && (
            <div className="flex justify-between items-center bg-white/5 p-3 rounded-2xl border border-white/10 backdrop-blur-xl">
              <button 
                onClick={() => setPage(p => Math.max(1, p - 1))}
                disabled={page === 1}
                className="px-6 py-2.5 bg-black/50 hover:bg-black/80 text-white rounded-xl disabled:opacity-30 disabled:cursor-not-allowed transition"
              >
                Previous
              </button>
              <span className="text-gray-400 font-medium text-sm">Page {page} of {totalPages}</span>
              <button 
                onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                disabled={page === totalPages}
                className="px-6 py-2.5 bg-black/50 hover:bg-black/80 text-white rounded-xl disabled:opacity-30 disabled:cursor-not-allowed transition"
              >
                Next
              </button>
            </div>
          )}
        </motion.main>
      </motion.div>
    </div>
  );
}
