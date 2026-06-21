import { NextRequest, NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/admin/pricing-coverage — lists every (provider, model) used
// in production RequestLog that does NOT have an entry in
// ProviderModel. Each gap = silent $1/MTok fallback at the proxy.
//
// Also accepts POST to seed a new ProviderModel entry. SUPERADMIN only.

export async function GET() {
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
  }

  // Distinct (provider, model) in RequestLog. Filter out null model
  // rows in JS because Prisma 5 chokes on `distinct` + `where: { NOT: null }`.
  const usedRaw = await prisma.requestLog.findMany({
    where: isSuper ? {} : { apiKeyId: { in: userKeyIds } },
    distinct: ["provider", "model"],
    select: { provider: true, model: true },
  });
  const used = usedRaw.filter((u) => u.model != null && u.model !== "");

  // Distinct entries in ProviderModel
  const known = await prisma.providerModel.findMany({
    where: { OR: [{ userId: "global" }, { userId: user.id }] },
    select: { provider: true, modelName: true },
  });
  const knownKeys = new Set(known.map((k) => `${k.provider}:${k.modelName}`));

  // For each gap, count requests and rough cost at $1/MTok
  const gaps = [];
  for (const u of used) {
    const key = `${u.provider}:${u.model}`;
    if (knownKeys.has(key)) continue;

    const stats = await prisma.requestLog.aggregate({
      where: { 
        provider: u.provider, 
        model: u.model,
        ...(isSuper ? {} : { apiKeyId: { in: userKeyIds } })
      },
      _count: { _all: true },
      _sum: {
        promptTokensOrig: true,
        completionTokensOrig: true,
      },
    });

    const tokens = (stats._sum.promptTokensOrig ?? 0) +
                   (stats._sum.completionTokensOrig ?? 0);
    // Current fallback is $1/MTok both ways. Real price would be
    // $X/MTok for input and $Y/MTok for output; we just show both.
    const fallbackDollars = tokens * 1.0 / 1_000_000;

    gaps.push({
      provider: u.provider,
      model: u.model,
      requestCount: stats._count._all,
      totalTokens: tokens,
      fallbackDollars,
    });
  }

  // Sort by token volume desc (the biggest leaks first)
  gaps.sort((a, b) => b.totalTokens - a.totalTokens);

  return NextResponse.json({
    gaps,
    knownModelCount: known.length,
    gapCount: gaps.length,
  });
}

// POST: add a new ProviderModel entry. Body: { provider, modelName, costPromptPer1M, costCompletionPer1M, costCachedInputPer1M?, costCacheWritePer1M? }
export async function POST(req: NextRequest) {
  const session = await getServerSession(authOptions);
  if (!session?.user) {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const user = session.user as any;
  const isSuper = user.role === "SUPERADMIN";
  const targetUserId = isSuper ? "global" : user.id;

  const body = await req.json();
  const { provider, modelName, costPromptPer1M, costCompletionPer1M, costCachedInputPer1M, costCacheWritePer1M } = body;

  if (!provider || !modelName || typeof costPromptPer1M !== "number" || typeof costCompletionPer1M !== "number") {
    return NextResponse.json({ error: "provider, modelName, costPromptPer1M and costCompletionPer1M are required" }, { status: 400 });
  }
  if (costPromptPer1M < 0 || costCompletionPer1M < 0) {
    return NextResponse.json({ error: "costs must be >= 0" }, { status: 400 });
  }

  // Upsert by (provider, modelName) unique constraint
  const row = await prisma.providerModel.upsert({
    where: { provider_modelName_userId: { provider, modelName, userId: targetUserId } },
    update: {
      costPromptPer1M,
      costCompletionPer1M,
      costCachedInputPer1M: costCachedInputPer1M ?? null,
      costCacheWritePer1M: costCacheWritePer1M ?? null,
    },
    create: {
      userId: targetUserId,
      provider,
      modelName,
      costPromptPer1M,
      costCompletionPer1M,
      costCachedInputPer1M: costCachedInputPer1M ?? null,
      costCacheWritePer1M: costCacheWritePer1M ?? null,
    },
  });

  return NextResponse.json({ row }, { status: 201 });
}
