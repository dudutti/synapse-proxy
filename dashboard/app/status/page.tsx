import { prisma } from "@/lib/prisma";
import { PublicStatusCard } from "@/components/PublicStatusCard";

// Public status page — no auth required. Shows aggregate system health
// so prospects and users can verify the platform is alive before
// signing up. Refreshes client-side every 10s.
//
// Differentiated from /admin by:
//   - No SUPERADMIN gate
//   - No per-user data (totals only, no virtualKeys, no emails)
//   - No metrics from the proxy data plane (those are SUPERADMIN-only)
//   - One single dense card, not a full HUD
export const dynamic = "force-dynamic";

export default async function PublicStatusPage() {
  // Pull global aggregates straight from Postgres. Two parallel queries
  // to keep the page TTFB under 100ms.
  const [userCount, apiKeyCount, requestLogs, costAgg, tokensAgg, distinctAgents] =
    await Promise.all([
      prisma.user.count(),
      prisma.apiKey.count(),
      prisma.requestLog.count(),
      prisma.requestLog.aggregate({ _sum: { costSaved: true } }),
      prisma.requestLog.aggregate({
        _sum: {
          promptTokensOrig: true,
          promptTokensOpt: true,
          completionTokensOrig: true,
          completionTokensOpt: true,
        },
      }),
      prisma.requestLog.findMany({
        distinct: ["agentId"],
        select: { agentId: true },
      }),
    ]);

  const t = tokensAgg._sum;
  const totalTokensSaved = Math.max(
    0,
    (t?.promptTokensOrig ?? 0) -
      (t?.promptTokensOpt ?? 0) +
      (t?.completionTokensOrig ?? 0) -
      (t?.completionTokensOpt ?? 0)
  );
  const totalCostSaved = costAgg._sum.costSaved ?? 0;

  return (
    <div className="min-h-screen bg-[#050505] text-white font-sans p-6 md:p-12">
      <div className="max-w-4xl mx-auto space-y-8">
        <header className="text-center space-y-3">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs font-bold uppercase tracking-widest">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75 animate-ping" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-400" />
            </span>
            All Systems Operational
          </div>
          <h1 className="text-4xl md:text-5xl font-black tracking-tight">
            Synapse Proxy Status
          </h1>
          <p className="text-zinc-400 max-w-xl mx-auto">
            Live aggregate health of the Synapse Proxy platform — counts of users,
            API keys, requests served, and dollars saved across all tenants.
          </p>
        </header>

        <PublicStatusCard
          totals={{
            users: userCount,
            apiKeys: apiKeyCount,
            requestLogs,
            tokensSaved: totalTokensSaved,
            costSaved: totalCostSaved,
            distinctAgents: distinctAgents.filter((a) => a.agentId).length,
          }}
        />

        <footer className="text-center text-xs text-zinc-600 space-y-1 pt-8">
          <p>
            Detailed per-call observability is available in the SUPERADMIN
            dashboard. This page is intentionally aggregate-only — no
            user data is exposed.
          </p>
          <p>
            Refreshes every 10 seconds · last server fetch just now
          </p>
        </footer>
      </div>
    </div>
  );
}
