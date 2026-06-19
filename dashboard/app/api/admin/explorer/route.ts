import { NextRequest, NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/admin/explorer — advanced RequestLog search.
//
// Query params:
//   ?agent=hermes        (substring match on agentId or agentLabel)
//   &model=MiniMax-M3    (exact match)
//   &provider=minimax    (exact match)
//   &level=L1,L2,MISS    (comma-separated, OR)
//   &minTokens=1000      (token count threshold on promptTokensOrig)
//   &minDuration=2000    (duration threshold in ms)
//   &minCost=0.001       (costSaved threshold)
//   &virtualKey=sk-opti  (virtual key prefix)
//   &from=2026-06-15     (ISO date)
//   &to=2026-06-17       (ISO date)
//   &page=1&limit=50     (pagination)
//
// SUPERADMIN only. Returns the matching RequestLog rows plus a
// pagination cursor.

const DEFAULT_LIMIT = 50;
const MAX_LIMIT = 200;

export async function GET(req: NextRequest) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const url = new URL(req.url);
  const sp = url.searchParams;

  const where: any = {};

  const agent = sp.get("agent")?.trim();
  if (agent) {
    where.OR = [
      { agentId: { contains: agent, mode: "insensitive" } },
      { agentLabel: { contains: agent, mode: "insensitive" } },
    ];
  }

  const model = sp.get("model")?.trim();
  if (model) where.model = model;

  const provider = sp.get("provider")?.trim();
  if (provider) where.provider = provider;

  const level = sp.get("level")?.trim();
  if (level) {
    const levels = level.split(",").map((s) => s.trim()).filter(Boolean);
    if (levels.length > 0) where.cacheLevel = { in: levels };
  }

  const minTokens = sp.get("minTokens");
  if (minTokens) where.promptTokensOrig = { gte: Number(minTokens) };

  const minDuration = sp.get("minDuration");
  if (minDuration) where.durationMs = { gte: Number(minDuration) };

  const minCost = sp.get("minCost");
  if (minCost) where.costSaved = { gte: Number(minCost) };

  const virtualKey = sp.get("virtualKey")?.trim();
  if (virtualKey) {
    // apiKeyId is a cuid, but the user usually types the virtualKey
    // string (sk-opti-...). Look up the ApiKey first.
    const key = await prisma.apiKey.findFirst({
      where: { virtualKey: { startsWith: virtualKey } },
      select: { id: true },
    });
    if (key) where.apiKeyId = key.id;
    else where.apiKeyId = "__no_match__"; // force empty result
  }

  const from = sp.get("from");
  const to = sp.get("to");
  if (from || to) {
    where.createdAt = {};
    if (from) (where.createdAt as any).gte = new Date(from);
    if (to) (where.createdAt as any).lte = new Date(to);
  }

  const page = Math.max(1, Number(sp.get("page") || 1));
  const limit = Math.min(MAX_LIMIT, Math.max(1, Number(sp.get("limit") || DEFAULT_LIMIT)));

  const [total, rows] = await Promise.all([
    prisma.requestLog.count({ where }),
    prisma.requestLog.findMany({
      where,
      orderBy: { createdAt: "desc" },
      skip: (page - 1) * limit,
      take: limit,
      select: {
        id: true,
        cacheLevel: true,
        createdAt: true,
        model: true,
        provider: true,
        promptTokensOrig: true,
        completionTokensOrig: true,
        promptTokensOpt: true,
        completionTokensOpt: true,
        durationMs: true,
        costSaved: true,
        agentId: true,
        agentLabel: true,
        sessionId: true,
        apiKeyId: true,
      },
    }),
  ]);

  // Aggregate stats for the current filter window
  const aggregate = await prisma.requestLog.aggregate({
    where,
    _sum: {
      promptTokensOrig: true,
      promptTokensOpt: true,
      completionTokensOrig: true,
      completionTokensOpt: true,
      costSaved: true,
    },
    _count: { _all: true },
  });

  const tokensSaved =
    (aggregate._sum.promptTokensOrig ?? 0) -
      (aggregate._sum.promptTokensOpt ?? 0) +
      (aggregate._sum.completionTokensOrig ?? 0) -
      (aggregate._sum.completionTokensOpt ?? 0);

  return NextResponse.json({
    rows,
    pagination: {
      page,
      limit,
      total,
      totalPages: Math.ceil(total / limit),
    },
    aggregate: {
      count: aggregate._count._all,
      tokensSaved: Math.max(0, tokensSaved),
      totalCostSaved: aggregate._sum.costSaved ?? 0,
    },
  });
}
