"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Clock, Database, ChevronRight, X, Activity, Loader2 } from "lucide-react";
import ParticleBackground from "@/components/ParticleBackground";

type SessionRow = {
  sessionId: string;
  startedAt: string;
  endedAt: string;
  durationMs: number;
  totalRequests: number;
  promptTokensOrig: number;
  completionTokensOrig: number;
  promptTokensOpt: number;
  completionTokensOpt: number;
  costSaved: number;
  costWithout: number;
  costWith: number;
  cacheHits: number;
};

type SessionDetail = {
  durationMs: number;
  totalRequests: number;
  hitRate: number;
  cacheHitDistribution: Record<string, number>;
  tokens: { original: { total: number; input: number; output: number }; optimized: { total: number; input: number; output: number } };
  savingsByClass: { inputFresh: number; cacheRead: number; cacheCreation: number; output: number };
  costs: { withoutCache: number; withCache: number; saved: number };
  cacheTokens: { creation: number; read: number; hit: number; miss: number };
  byProvider: Record<string, any>;
  byModel: Record<string, any>;
  byAgent: Record<string, any>;
  topExpensive: Array<any>;
  totalDurationMs: number;
  avgDurationMs: number;
  totalReasoningTokens: number;
};

function fmtDuration(ms: number) {
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3_600_000) return `${(ms / 60_000).toFixed(1)}min`;
  return `${(ms / 3_600_000).toFixed(1)}h`;
}

function fmtDate(iso: string) {
  return new Date(iso).toLocaleString();
}

export default function SessionsHistoryPage() {
  const [sessions, setSessions] = useState<SessionRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [openSessionId, setOpenSessionId] = useState<string | null>(null);
  const [openSessionSummary, setOpenSessionSummary] = useState<SessionRow | null>(null);
  const [openSessionDetail, setOpenSessionDetail] = useState<SessionDetail | null>(null);
  const [openLoading, setOpenLoading] = useState(false);
  const router = useRouter();

  useEffect(() => {
    fetch("/api/analytics/sessions")
      .then((r) => r.json())
      .then((d) => {
        if (d.error) setError(d.error);
        else setSessions(d.sessions || []);
      })
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false));
  }, []);

  // When the user clicks a row, open the modal in two stages:
  //   1) openSessionSummary is filled from the list row (so the
  //      user sees the metadata instantly).
  //   2) we then fetch the full /api/analytics/session response
  //      for that sessionId and put it in openSessionDetail.
  //      The /api/analytics/session call uses the same proxy
  //      pipeline as the live Session Summary modal on the
  //      dashboard home, so the per-row detail is identical
  //      to what the user sees when they hit Stop in real time.
  const openSession = (s: SessionRow) => {
    setOpenSessionId(s.sessionId);
    setOpenSessionSummary(s);
    setOpenSessionDetail(null);
    setOpenLoading(true);

    const startMs = new Date(s.startedAt).getTime();
    const endMs = new Date(s.endedAt).getTime();
    const url = `/api/analytics/session?start=${startMs}&end=${endMs}&sessionId=${encodeURIComponent(s.sessionId)}`;

    fetch(url)
      .then((r) => r.json())
      .then((d) => {
        if (d.error) setError(d.error);
        else setOpenSessionDetail(d);
      })
      .catch((e) => setError(String(e)))
      .finally(() => setOpenLoading(false));
  };

  const closeSession = () => {
    setOpenSessionId(null);
    setOpenSessionSummary(null);
    setOpenSessionDetail(null);
  };

  return (
    <div className="min-h-screen bg-[#050505] text-white p-8 font-sans relative overflow-hidden">
      <ParticleBackground />

      <div className="max-w-6xl mx-auto relative z-10">
        <div className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-3">
            <Clock className="w-7 h-7 text-emerald-400" />
            <h1 className="text-3xl font-bold">Session History</h1>
          </div>
          <button
            onClick={() => router.push("/")}
            className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-sm transition"
          >
            Back to Dashboard
          </button>
        </div>

        <p className="text-sm text-zinc-400 mb-6">
          Every "Record Session" you ran is saved here. Click a row to see the full per-class savings breakdown.
          Sessions are reconstructed from <code className="text-emerald-300">RequestLog.sessionId</code> — no separate table is needed.
        </p>

        {error && (
          <div className="p-4 rounded-lg bg-red-500/10 border border-red-500/30 text-red-300 text-sm mb-4">
            {error}
          </div>
        )}

        {loading ? (
          <div className="text-zinc-500">Loading sessions{"\u2026"}</div>
        ) : sessions.length === 0 ? (
          <div className="p-12 rounded-2xl border border-dashed border-white/10 text-center text-zinc-500">
            <Database className="w-10 h-10 mx-auto mb-3 opacity-40" />
            <p className="text-lg">No recorded sessions yet</p>
            <p className="text-sm mt-2">Start a recording from the dashboard home or the playground.</p>
          </div>
        ) : (
          <div className="rounded-2xl border border-white/10 overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-white/5 text-zinc-400 text-[10px] uppercase tracking-wider">
                <tr>
                  <th className="text-left px-4 py-3 font-bold">Session</th>
                  <th className="text-left px-4 py-3 font-bold">When</th>
                  <th className="text-left px-4 py-3 font-bold">Duration</th>
                  <th className="text-right px-4 py-3 font-bold">Requests</th>
                  <th className="text-right px-4 py-3 font-bold">Cache Hits</th>
                  <th className="text-right px-4 py-3 font-bold">Tokens (in/out)</th>
                  <th className="text-right px-4 py-3 font-bold">Cost Saved</th>
                  <th className="px-2 py-3"></th>
                </tr>
              </thead>
              <tbody>
                {sessions.map((s) => (
                  <tr
                    key={s.sessionId}
                    onClick={() => openSession(s)}
                    className="border-t border-white/5 hover:bg-white/5 cursor-pointer transition"
                  >
                    <td className="px-4 py-3 font-mono text-xs text-zinc-300">
                      {s.sessionId.slice(0, 32)}{"\u2026"}
                    </td>
                    <td className="px-4 py-3 text-zinc-300">{fmtDate(s.startedAt)}</td>
                    <td className="px-4 py-3 text-zinc-300">{fmtDuration(s.durationMs)}</td>
                    <td className="px-4 py-3 text-right font-mono">{s.totalRequests}</td>
                    <td className="px-4 py-3 text-right font-mono text-emerald-400">{s.cacheHits}</td>
                    <td className="px-4 py-3 text-right font-mono text-zinc-400 text-xs">
                      {s.promptTokensOrig.toLocaleString()}/{s.completionTokensOrig.toLocaleString()}
                      {" \u2192 "}
                      {s.promptTokensOpt.toLocaleString()}/{s.completionTokensOpt.toLocaleString()}
                    </td>
                    <td className="px-4 py-3 text-right font-mono text-emerald-400">
                      ${s.costSaved.toFixed(6)}
                    </td>
                    <td className="px-2 py-3 text-zinc-500">
                      <ChevronRight className="w-4 h-4" />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {openSessionId && openSessionSummary && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm overflow-y-auto">
            <div className="bg-[#111] border border-white/10 rounded-3xl p-8 max-w-3xl w-full relative my-8">
              <button
                onClick={closeSession}
                className="absolute top-4 right-4 text-zinc-500 hover:text-white"
              >
                <X className="w-6 h-6" />
              </button>

              <div className="flex items-center justify-between flex-wrap gap-3 mb-6">
                <div>
                  <h2 className="text-2xl font-bold mb-1 text-white flex items-center gap-3">
                    <Activity className="text-emerald-400" />
                    Session Summary
                  </h2>
                  <p className="text-xs font-mono text-zinc-500">{openSessionSummary.sessionId}</p>
                </div>
                <button
                  onClick={() => router.push(`/explorer?sessionId=${encodeURIComponent(openSessionSummary.sessionId)}`)}
                  className="px-4 py-2 rounded-xl bg-blue-600 hover:bg-blue-500 text-white font-bold text-xs transition shadow-lg shadow-blue-600/10"
                >
                  Explore Session Logs {"\u2192"}
                </button>
              </div>

              {/* --- Top stats: duration, requests, hit rate --- */}
              <div className="grid grid-cols-3 gap-3">
                <div className="flex flex-col p-3 bg-white/5 rounded-xl">
                  <span className="text-zinc-500 text-[10px] uppercase tracking-wider font-bold">Duration</span>
                  <span className="font-mono font-bold text-2xl text-white">
                    {openSessionDetail
                      ? fmtDuration(openSessionDetail.durationMs)
                      : fmtDuration(openSessionSummary.durationMs)}
                  </span>
                </div>
                <div className="flex flex-col p-3 bg-white/5 rounded-xl">
                  <span className="text-zinc-500 text-[10px] uppercase tracking-wider font-bold">Total Requests</span>
                  <span className="font-mono font-bold text-2xl text-white">
                    {openSessionDetail
                      ? openSessionDetail.totalRequests
                      : openSessionSummary.totalRequests}
                  </span>
                </div>
                <div className="flex flex-col p-3 bg-white/5 rounded-xl">
                  <span className="text-zinc-500 text-[10px] uppercase tracking-wider font-bold">Hit Rate</span>
                  <span className="font-mono font-bold text-2xl text-emerald-400">
                    {openSessionDetail
                      ? openSessionDetail.hitRate.toFixed(1)
                      : openSessionSummary.totalRequests > 0
                        ? ((openSessionSummary.cacheHits / openSessionSummary.totalRequests) * 100).toFixed(1)
                        : "0.0"}%
                  </span>
                </div>
              </div>

              {openLoading && (
                <div className="p-4 mt-3 rounded-lg bg-white/5 text-zinc-400 text-sm flex items-center gap-2">
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Loading full per-class breakdown{"\u2026"}
                </div>
              )}

              {/* Everything below only renders once the detail
                  fetch has resolved. Same exact rendering as the
                  Session Summary modal on the dashboard home. */}
              {openSessionDetail && (
                <>
                  {/* --- Cache hit distribution (L0/L1/L2/L3/LOOP/MISS) --- */}
                  <div className="grid grid-cols-6 gap-2 mt-4">
                    {(["MISS", "L0", "L1", "L2", "L3", "LOOP"] as const).map((lvl) => {
                      const styles: Record<string, string> = {
                        MISS: "bg-zinc-700/40 text-zinc-400 border-white/5",
                        L0: "bg-cyan-500/20 text-cyan-300 border-cyan-500/30",
                        L1: "bg-blue-500/20 text-blue-300 border-blue-500/30",
                        L2: "bg-emerald-500/20 text-emerald-300 border-emerald-500/30",
                        L3: "bg-purple-500/20 text-purple-300 border-purple-500/30",
                        LOOP: "bg-amber-500/20 text-amber-300 border-amber-500/30",
                      };
                      return (
                        <div key={lvl} className={`p-2 rounded-lg text-center border ${styles[lvl]}`}>
                          <div className="text-[10px] uppercase font-bold">{lvl === "L0" ? "L0 Coalesced" : lvl}</div>
                          <div className="font-mono text-sm font-bold">
                            {openSessionDetail.cacheHitDistribution?.[lvl] || 0}
                          </div>
                        </div>
                      );
                    })}
                  </div>

                  {/* --- Tokens: original vs optimized --- */}
                  <div className="grid grid-cols-2 gap-3 mt-3">
                    <div className="flex justify-between items-center p-3 bg-white/5 rounded-xl">
                      <span className="text-zinc-400 text-sm">Original Tokens</span>
                      <div className="text-right">
                        <div className="font-mono font-bold text-zinc-300">
                          {openSessionDetail.tokens.original.total.toLocaleString()}
                        </div>
                        <div className="text-[10px] text-zinc-500 font-mono">
                          {openSessionDetail.tokens.original.input.toLocaleString()} in /{" "}
                          {openSessionDetail.tokens.original.output.toLocaleString()} out
                        </div>
                      </div>
                    </div>
                    <div className="flex justify-between items-center p-3 bg-white/5 rounded-xl border border-emerald-500/30">
                      <span className="text-zinc-400 text-sm">Optimized Tokens</span>
                      <div className="text-right">
                        <div className="font-mono font-bold text-emerald-400">
                          {openSessionDetail.tokens.optimized.total.toLocaleString()}
                        </div>
                        <div className="text-[10px] text-emerald-500/70 font-mono">
                          {openSessionDetail.tokens.optimized.input.toLocaleString()} in /{" "}
                          {openSessionDetail.tokens.optimized.output.toLocaleString()} out
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* --- Tokens Purged --- */}
                  <div className="flex flex-col p-3 bg-emerald-500/10 rounded-xl border border-emerald-500/50 mt-3">
                    <div className="flex justify-between items-center mb-2">
                      <span className="text-emerald-400 text-sm font-bold">Tokens Purged (Saved)</span>
                      <div className="flex items-center gap-2">
                        <span className="font-mono font-bold text-emerald-400">
                          {(openSessionDetail.tokens.original.total - openSessionDetail.tokens.optimized.total).toLocaleString()}
                        </span>
                        <span className="text-xs bg-emerald-500/20 text-emerald-300 px-2 py-0.5 rounded-full">
                          {openSessionDetail.tokens.original.total > 0
                            ? (((openSessionDetail.tokens.original.total - openSessionDetail.tokens.optimized.total) / openSessionDetail.tokens.original.total) * 100).toFixed(1)
                            : "0.0"}%
                        </span>
                      </div>
                    </div>
                    <div className="flex justify-between text-xs text-emerald-500/70 border-t border-emerald-500/20 pt-2 mt-1">
                      <span>Input: {(openSessionDetail.tokens.original.input - openSessionDetail.tokens.optimized.input).toLocaleString()}</span>
                      <span>Output: {(openSessionDetail.tokens.original.output - openSessionDetail.tokens.optimized.output).toLocaleString()}</span>
                    </div>
                  </div>

                  {/* --- Cache tokens (Anthropic-style) --- */}
                  {openSessionDetail.cacheTokens && (
                    <div className="grid grid-cols-4 gap-2 mt-3">
                      <Stat label="Cache Write" value={openSessionDetail.cacheTokens.creation} />
                      <Stat label="Cache Read" value={openSessionDetail.cacheTokens.read} />
                      <Stat label="Cache Hit" value={openSessionDetail.cacheTokens.hit} />
                      <Stat label="Cache Miss" value={openSessionDetail.cacheTokens.miss} />
                    </div>
                  )}

                  {/* --- Per-class savings --- */}
                  {openSessionDetail.savingsByClass && (
                    <div className="p-3 bg-white/5 rounded-xl mt-3">
                      <div className="text-[10px] text-zinc-500 uppercase tracking-wider font-bold mb-2">Per-Class Savings</div>
                      <div className="grid grid-cols-4 gap-2 text-center">
                        <div>
                          <div className="text-[10px] text-emerald-400">input fresh</div>
                          <div className="font-mono text-emerald-300 text-xs">${openSessionDetail.savingsByClass.inputFresh.toFixed(6)}</div>
                        </div>
                        <div>
                          <div className="text-[10px] text-blue-400">cache read</div>
                          <div className="font-mono text-blue-300 text-xs">${openSessionDetail.savingsByClass.cacheRead.toFixed(6)}</div>
                        </div>
                        <div>
                          <div className="text-[10px] text-purple-400">cache creation</div>
                          <div className="font-mono text-purple-300 text-xs">${openSessionDetail.savingsByClass.cacheCreation.toFixed(6)}</div>
                        </div>
                        <div>
                          <div className="text-[10px] text-amber-400">output</div>
                          <div className="font-mono text-amber-300 text-xs">${openSessionDetail.savingsByClass.output.toFixed(6)}</div>
                        </div>
                      </div>
                    </div>
                  )}

                  {/* --- By provider --- */}
                  {openSessionDetail.byProvider && Object.keys(openSessionDetail.byProvider).length > 0 && (
                    <Section title="By Provider">
                      {Object.entries(openSessionDetail.byProvider).map(([prov, stats]: [string, any]) => (
                        <Row
                          key={prov}
                          left={<span className="text-sm text-zinc-300 font-mono">{prov}</span>}
                          right={
                            <div className="flex items-center gap-3 text-xs">
                              <span className="text-zinc-500">{stats.requests} req</span>
                              <span className="text-emerald-400">${(stats.costSaved || 0).toFixed(6)}</span>
                              <span className="text-blue-400">{(stats.cacheHits || 0)} cache hits</span>
                            </div>
                          }
                        />
                      ))}
                    </Section>
                  )}

                  {/* --- By agent --- */}
                  {openSessionDetail.byAgent && Object.keys(openSessionDetail.byAgent).length > 0 && (
                    <Section title="By Agent">
                      {Object.entries(openSessionDetail.byAgent).map(([agentID, stats]: [string, any]) => (
                        <Row
                          key={agentID}
                          left={
                            <div className="flex items-center gap-2">
                              <span className="text-sm text-zinc-300 font-mono">{agentID || "unknown"}</span>
                              {stats.label && stats.label !== agentID && (
                                <span className="text-[10px] text-zinc-500">{stats.label}</span>
                              )}
                            </div>
                          }
                          right={
                            <div className="flex items-center gap-3 text-xs">
                              <span className="text-zinc-500">{stats.requests} req</span>
                              <span className="text-emerald-400">${(stats.costSaved || 0).toFixed(6)}</span>
                            </div>
                          }
                        />
                      ))}
                    </Section>
                  )}

                  {/* --- Cost (always last) --- */}
                  <div className="p-3 bg-white/5 rounded-xl mt-3">
                    <div className="text-[10px] text-zinc-500 uppercase tracking-wider font-bold mb-2">Cost</div>
                    <div className="grid grid-cols-3 gap-3 text-sm">
                      <div>
                        <div className="text-zinc-500 text-[10px]">Without Synapse Proxy</div>
                        <div className="font-mono">${openSessionDetail.costs.withoutCache.toFixed(6)}</div>
                      </div>
                      <div>
                        <div className="text-zinc-500 text-[10px]">With Synapse Proxy</div>
                        <div className="font-mono">${openSessionDetail.costs.withCache.toFixed(6)}</div>
                      </div>
                      <div>
                        <div className="text-zinc-500 text-[10px]">Net Cash Saved</div>
                        <div className="font-mono text-emerald-400">${openSessionDetail.costs.saved.toFixed(6)}</div>
                      </div>
                    </div>
                  </div>
                </>
              )}

              {/* Fallback: while openSessionDetail is null (e.g. the
                  detail fetch failed or the session has 0 rows),
                  show the summary-only fields from the list row. */}
              {!openSessionDetail && !openLoading && (
                <div className="grid grid-cols-2 gap-3 mt-3">
                  <div className="flex justify-between items-center p-3 bg-white/5 rounded-xl">
                    <span className="text-zinc-400 text-sm">Original Tokens</span>
                    <div className="text-right">
                      <div className="font-mono font-bold text-zinc-300">
                        {(openSessionSummary.promptTokensOrig + openSessionSummary.completionTokensOrig).toLocaleString()}
                      </div>
                      <div className="text-[10px] text-zinc-500 font-mono">
                        {openSessionSummary.promptTokensOrig.toLocaleString()} in /{" "}
                        {openSessionSummary.completionTokensOrig.toLocaleString()} out
                      </div>
                    </div>
                  </div>
                  <div className="flex justify-between items-center p-3 bg-white/5 rounded-xl border border-emerald-500/30">
                    <span className="text-zinc-400 text-sm">Optimized Tokens</span>
                    <div className="text-right">
                      <div className="font-mono font-bold text-emerald-400">
                        {(openSessionSummary.promptTokensOpt + openSessionSummary.completionTokensOpt).toLocaleString()}
                      </div>
                      <div className="text-[10px] text-emerald-500/70 font-mono">
                        {openSessionSummary.promptTokensOpt.toLocaleString()} in /{" "}
                        {openSessionSummary.completionTokensOpt.toLocaleString()} out
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="p-2 bg-white/5 rounded-lg text-center border border-white/5">
      <div className="text-[10px] text-zinc-500 uppercase font-bold">{label}</div>
      <div className="font-mono text-white text-xs">{(value || 0).toLocaleString()}</div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="p-3 bg-white/5 rounded-xl mt-3">
      <div className="text-[10px] text-zinc-500 uppercase tracking-wider font-bold mb-2">{title}</div>
      {children}
    </div>
  );
}

function Row({ left, right }: { left: React.ReactNode; right: React.ReactNode }) {
  return (
    <div className="flex justify-between items-center py-1.5 border-b border-white/5 last:border-0">
      {left}
      {right}
    </div>
  );
}
