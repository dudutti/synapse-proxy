import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";
import { cacheJson } from "@/lib/redis";

export const dynamic = "force-dynamic";

// /api/admin/expensive-prompts — top N (prompt fingerprint, total
// tokens, total cost, hit count) sorted by total cost. The fingerprint
// is the SHA-256 of the request payload so we group identical
// prompts together.
//
// We also compute the L2 potential: if all repeats of a prompt could
// be served from cache, the total costSaved would be (hits - 1) *
// costPerRequest. We surface this as a "miss opportunity" hint.
//
// SUPERADMIN only.

const DEFAULT_LIMIT = 20;

export async function GET(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session?.user) {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const user = session.user as any;
  const isSuper = user.role === "SUPERADMIN";

  let userKeyIds: string[] = [];
  if (!isSuper) {
    const keys = await prisma.apiKey.findMany({
      where: { userId: user.id },
      select: { id: true }
    });
    userKeyIds = keys.map(k => k.id);
    if (userKeyIds.length === 0) {
      return NextResponse.json({
        rows: [],
        windowHours: 0,
        totalHits: 0,
        totalFallbackCost: 0,
        totalL2Potential: 0,
        groupingMode: "payloadHash",
      });
    }
  }

  const url = new URL(req.url);
  const limit = Math.min(100, Math.max(1, Number(url.searchParams.get("limit") || DEFAULT_LIMIT)));
  const windowHours = Math.min(720, Math.max(1, Number(url.searchParams.get("windowHours") || 24 * 30))); // default 30 days

  const since = new Date(Date.now() - windowHours * 3600 * 1000);

  // The (provider, model, payloadHash) groupBy + 50-row sample fetch is
  // expensive on a 30-day window. Cache the whole response for 60s —
  // the user can wait a minute for fresh numbers on a deep analytics page.
  const cacheKey = `synapse:dash:expensive:${user.id || "global"}:${windowHours}:${limit}`;
  const cached = await cacheJson<any>(cacheKey, 60, async () => {
    return await computeExpensive({ since, limit, isSuper, userKeyIds, windowHours });
  });
  return NextResponse.json(cached);
}

async function computeExpensive({
  since, limit, isSuper, userKeyIds, windowHours,
}: {
  since: Date;
  limit: number;
  isSuper: boolean;
  userKeyIds: string[];
  windowHours: number;
}) {

  // Group by payloadHash so identical prompts collapse into a single
  // row. The proxy now populates payloadHash correctly (see the
  // Rebuild Guide in the plan). We filter out null payloads so
  // legacy rows from before the fix are skipped cleanly.
  const grouped = await prisma.requestLog.groupBy({
    by: ["provider", "model", "payloadHash"],
    where: {
      createdAt: { gte: since },
      model: { not: "" },
      ...(isSuper ? {} : { apiKeyId: { in: userKeyIds } }),
    },
    _sum: {
      promptTokensOrig: true,
      promptTokensOpt: true,
      completionTokensOrig: true,
      completionTokensOpt: true,
      costSaved: true,
    },
    _count: { _all: true },
  });

  // Pull the actual hash + a sample of the prompt text for context.
  // We pick the most expensive groups and fetch one row each for the
  // prompt excerpt.
  const sortedByCost = grouped
    .filter((g) => g.payloadHash && g.payloadHash !== "")
    .map((g) => ({
      bucketKey: `${g.provider}|${g.model}|${g.payloadHash}`,
      provider: g.provider,
      model: g.model,
      payloadHash: g.payloadHash || "",
      hits: g._count._all,
      tokensOrig:
        (g._sum.promptTokensOrig ?? 0) + (g._sum.completionTokensOrig ?? 0),
      tokensOpt:
        (g._sum.promptTokensOpt ?? 0) + (g._sum.completionTokensOpt ?? 0),
      tokensSaved:
        (g._sum.promptTokensOrig ?? 0) - (g._sum.promptTokensOpt ?? 0) +
        (g._sum.completionTokensOrig ?? 0) - (g._sum.completionTokensOpt ?? 0),
      costSaved: g._sum.costSaved ?? 0,
      approxFallbackCost: ((g._sum.promptTokensOrig ?? 0) + (g._sum.completionTokensOrig ?? 0)) / 1_000_000,
    }))
    .sort((a, b) => b.approxFallbackCost - a.approxFallbackCost)
    .slice(0, limit);

  // Fetch one sample per payloadHash so we can show a snippet of the
  // prompt text.
  const hashKeys = sortedByCost.map((g) => g.payloadHash).filter(Boolean);
  const samples = hashKeys.length > 0
    ? await prisma.requestLog.findMany({
        where: { 
          payloadHash: { in: hashKeys },
          ...(isSuper ? {} : { apiKeyId: { in: userKeyIds } })
        },
        orderBy: { createdAt: "desc" },
        select: { payloadHash: true, originalPayload: true, createdAt: true },
      })
    : [];

  const hashToPrompt = new Map<string, { text: string; at: string }>();
  for (const s of samples) {
    if (!s.payloadHash || hashToPrompt.has(s.payloadHash)) continue;
    const raw = s.originalPayload || "";
    try {
      const parsed = JSON.parse(raw);
      const msgs = Array.isArray(parsed?.messages) ? parsed.messages : [];
      const lastUser = [...msgs].reverse().find((m: any) => m?.role === "user");
      const text = lastUser?.content;
      const preview = typeof text === "string"
        ? text.slice(0, 240)
        : JSON.stringify(text || parsed).slice(0, 240);
      hashToPrompt.set(s.payloadHash, { text: preview, at: s.createdAt.toISOString() });
    } catch {
      hashToPrompt.set(s.payloadHash, { text: raw.slice(0, 240), at: s.createdAt.toISOString() });
    }
  }

  const rows = sortedByCost.map((r) => {
    const sample = hashToPrompt.get(r.payloadHash);
    return {
      ...r,
      promptPreview: sample?.text || `(no preview — ${r.hits} hits)`,
      lastSeenAt: sample?.at || null,
      l2Potential:
        r.hits > 1
          ? (r.hits - 1) * (r.approxFallbackCost / r.hits) * 0.8
          : 0,
    };
  });

  return {
    rows,
    windowHours,
    totalHits: rows.reduce((acc, r) => acc + r.hits, 0),
    totalFallbackCost: rows.reduce((acc, r) => acc + r.approxFallbackCost, 0),
    totalL2Potential: rows.reduce((acc, r) => acc + r.l2Potential, 0),
    groupingMode: "payloadHash",
  };
}
