import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";

export async function GET(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

  // Get user's API keys
  const apiKeys = await prisma.apiKey.findMany({ where: { userId: user.id } });
  const url = new URL(req.url);
  const keyIdParam = url.searchParams.get("keyId");

  const isSuper = user.role === "SUPERADMIN";

  let keyIds = apiKeys.map((k) => k.id);
  if (keyIdParam) {
    if (isSuper || keyIds.includes(keyIdParam)) {
      keyIds = [keyIdParam];
    } else {
      return NextResponse.json({ error: "Invalid API Key" }, { status: 400 });
    }
  }

  if (!isSuper && keyIds.length === 0) {
    return NextResponse.json({ logs: [], chartData: [] });
  }

  const page = parseInt(url.searchParams.get("page") || "1", 10);
  const limit = parseInt(url.searchParams.get("limit") || "10", 10);
  const days = parseInt(url.searchParams.get("days") || "0", 10);
  const skip = (page - 1) * limit;

  const dateFilter = days > 0 ? {
    gte: new Date(Date.now() - days * 24 * 60 * 60 * 1000)
  } : undefined;

  const whereClause: any = {
    ...(dateFilter ? { createdAt: dateFilter } : {})
  };
  if (!isSuper) {
    whereClause.apiKeyId = { in: keyIds };
  } else if (keyIdParam) {
    whereClause.apiKeyId = keyIdParam;
  }

  // Get logs for the table with pagination
  const logs = await prisma.requestLog.findMany({
    where: whereClause,
    orderBy: { createdAt: 'desc' },
    skip: skip,
    take: limit
  });

  const totalLogs = await prisma.requestLog.count({
    where: whereClause
  });
  const totalPages = Math.ceil(totalLogs / limit);

  // Fetch dynamic model pricing from Superadmin configuration
  const modelsPricing = await prisma.providerModel.findMany({
    where: { OR: [{ userId: "global" }, { userId: user.id }] }
  });
  const pricingMap = new Map();
  // Sort so global is applied first, then user's custom pricing overwrites it
  modelsPricing.sort((a, b) => a.userId === "global" ? -1 : 1).forEach(m => {
    pricingMap.set(`${m.provider}_${m.modelName}`, m);
  });

  // Calculate cumulative chart data
  const allLogs = await prisma.requestLog.findMany({
    where: whereClause,
    orderBy: { createdAt: 'asc' }
  });

  const chartMap = new Map();
  let cumulativeCostWithout = 0;
  let cumulativeCostWith = 0;
  let totalTokensSent = { total: 0, input: 0, output: 0 };
  let totalTokensOptimized = { total: 0, input: 0, output: 0 };

  const cacheHitDistribution = { MISS: 0, L0: 0, L1: 0, L2: 0, L3: 0 };
  const savingsByProvider: Record<string, number> = {};

  // Prompt-cache aggregates per provider
  const cacheByProvider: Record<string, { creation: number; read: number; hit: number; miss: number; count: number }> = {};
  let totalCacheCreation = 0;
  let totalCacheRead = 0;
  let totalCacheHit = 0;
  let totalCacheMiss = 0;

  // Per-class savings aggregates (4 classes)
  const savingsByClassByProvider: Record<string, { inputFresh: number; cacheRead: number; cacheCreation: number; output: number }> = {};
  let totalSavingsByClass = { inputFresh: 0, cacheRead: 0, cacheCreation: 0, output: 0 };

  // Intent distribution
  const intentDistribution: Record<string, number> = {};

  for (const log of allLogs) {
    const origTokens = log.promptTokensOrig + log.completionTokensOrig;
    const optTokens = log.promptTokensOpt + log.completionTokensOpt;
    totalTokensSent.total += origTokens;
    totalTokensSent.input += log.promptTokensOrig;
    totalTokensSent.output += log.completionTokensOrig;

    totalTokensOptimized.total += optTokens;
    totalTokensOptimized.input += log.promptTokensOpt;
    totalTokensOptimized.output += log.completionTokensOpt;

    if (log.cacheLevel === "NONE") cacheHitDistribution.MISS++;
    else if (log.cacheLevel === "L0") cacheHitDistribution.L0++;
    else if (log.cacheLevel === "L1") cacheHitDistribution.L1++;
    else if (log.cacheLevel === "L2") cacheHitDistribution.L2++;
    else if (log.cacheLevel === "L3") cacheHitDistribution.L3++;

    // Cache prompt aggregates
    const cCreation = log.cacheCreationTokens ?? 0;
    const cRead = log.cacheReadTokens ?? 0;
    const cHit = log.cacheHitTokens ?? 0;
    const cMiss = log.cacheMissTokens ?? 0;
    totalCacheCreation += cCreation;
    totalCacheRead += cRead;
    totalCacheHit += cHit;
    totalCacheMiss += cMiss;
    if (!cacheByProvider[log.provider]) {
      cacheByProvider[log.provider] = { creation: 0, read: 0, hit: 0, miss: 0, count: 0 };
    }
    const p = cacheByProvider[log.provider];
    p.creation += cCreation;
    p.read += cRead;
    p.hit += cHit;
    p.miss += cMiss;
    p.count += 1;

    // Per-class savings aggregation
    const sIF = log.savingsInputFresh ?? 0;
    const sCR = log.savingsCacheRead ?? 0;
    const sCC = log.savingsCacheCreation ?? 0;
    const sO = log.savingsOutput ?? 0;
    totalSavingsByClass.inputFresh += sIF;
    totalSavingsByClass.cacheRead += sCR;
    totalSavingsByClass.cacheCreation += sCC;
    totalSavingsByClass.output += sO;
    if (!savingsByClassByProvider[log.provider]) {
      savingsByClassByProvider[log.provider] = { inputFresh: 0, cacheRead: 0, cacheCreation: 0, output: 0 };
    }
    const sp = savingsByClassByProvider[log.provider];
    sp.inputFresh += sIF;
    sp.cacheRead += sCR;
    sp.cacheCreation += sCC;
    sp.output += sO;

    const saved = origTokens - optTokens;
    if (saved > 0) {
      if (!savingsByProvider[log.provider]) savingsByProvider[log.provider] = 0;
      savingsByProvider[log.provider] += saved;
    }
    const day = log.createdAt.toISOString().split('T')[0];
    
    // Reverse engineer cost using dynamic pricing map
    let promptPrice = 1.0;
    let compPrice = 1.0;
    const modelPrice = pricingMap.get(`${log.provider}_${log.model}`);
    if (modelPrice) {
      promptPrice = modelPrice.costPromptPer1M;
      compPrice = modelPrice.costCompletionPer1M;
    }

    const costWithoutThisReq = (log.promptTokensOrig / 1000000.0) * promptPrice + (log.completionTokensOrig / 1000000.0) * compPrice;
    // costWith = what we actually paid (per-class: input fresh + cache_read + cache_creation + output)
    // = (orig - savings) = orig - 4-class breakdown
    const costWithThisReq = costWithoutThisReq - sIF - sCR - sCC - sO;

    cumulativeCostWithout += costWithoutThisReq;
    cumulativeCostWith += costWithThisReq;

    // Cumulative chart entry: cost + 4 savings classes + token counts + cache_read rate
    const existing = chartMap.get(day) || {
      day,
      costWithout: 0, costWith: 0,
      savingsInputFresh: 0, savingsCacheRead: 0, savingsCacheCreation: 0, savingsOutput: 0,
      promptTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0,
    };
    existing.costWithout = parseFloat(cumulativeCostWithout.toFixed(6));
    existing.costWith = parseFloat(cumulativeCostWith.toFixed(6));
    existing.savingsInputFresh = parseFloat((existing.savingsInputFresh + sIF).toFixed(6));
    existing.savingsCacheRead = parseFloat((existing.savingsCacheRead + sCR).toFixed(6));
    existing.savingsCacheCreation = parseFloat((existing.savingsCacheCreation + sCC).toFixed(6));
    existing.savingsOutput = parseFloat((existing.savingsOutput + sO).toFixed(6));
    existing.promptTokens += log.promptTokensOrig;
    existing.cacheReadTokens += cRead;
    existing.cacheCreationTokens += cCreation;
    chartMap.set(day, existing);

    // Intent tracking
    const intent = log.intentTag || "unknown";
    intentDistribution[intent] = (intentDistribution[intent] || 0) + 1;
  }

  const chartData = Array.from(chartMap.values()).map(d => ({
    ...d,
    // Cache read rate on input
    cacheReadRate: d.promptTokens > 0 ? d.cacheReadTokens / d.promptTokens : 0,
  }));

  const formattedLogs = logs.map(l => {
    let typeLabel = "Standard Routing (no opt)";
    if (l.cacheLevel === "L0") typeLabel = "L0 Coalesced (in-flight)";
    else if (l.cacheLevel === "L1") typeLabel = "L1 Cache (exact)";
    else if (l.cacheLevel === "L2") typeLabel = "L2 Cache (semantic)";
    else if (l.cacheLevel === "L3") typeLabel = "L3 Standard (compressed)";

    return {
      id: l.id,
      timestamp: l.createdAt.toISOString(), // Send full ISO string to frontend for local TZ formatting
      reqModel: l.model,
      provider: l.provider,
      // Agent / session / multiturn fields. These are critical for
      // the LiveTelemetryGrouped component to bucket rows by
      // agent / session / convSignature instead of falling back
      // to per-row "anon:<timestamp>" or "no-session:<timestamp>"
      // buckets. Without these fields the dashboard saw 67 buckets
      // of 1 request each, even though the rows were part of the
      // same conversation (Admin > Session History showed them
      // grouped correctly, but the live telemetry did not).
      // The /api/analytics/stream endpoint already includes them,
      // we just forgot to add them here when the firewall /
      // multiturn migration went in.
      agentId: l.agentId || "",
      agentLabel: l.agentLabel || "",
      sessionId: l.sessionId || "",
      turnCount: l.turnCount ?? 0,
      convSignature: l.convSignature || "",
      saved: (l.promptTokensOrig + l.completionTokensOrig) - (l.promptTokensOpt + l.completionTokensOpt),
      savedInput: l.promptTokensOrig - l.promptTokensOpt,
      savedOutput: l.completionTokensOrig - l.completionTokensOpt,
      type: typeLabel,
      cacheCreationTokens: l.cacheCreationTokens ?? 0,
      cacheReadTokens: l.cacheReadTokens ?? 0,
      cacheHitTokens: l.cacheHitTokens ?? 0,
      cacheMissTokens: l.cacheMissTokens ?? 0,
      savingsByClass: {
        inputFresh: l.savingsInputFresh ?? 0,
        cacheRead: l.savingsCacheRead ?? 0,
        cacheCreation: l.savingsCacheCreation ?? 0,
        output: l.savingsOutput ?? 0,
      },
      originalPayload: l.originalPayload,
      optimizedPayload: l.optimizedPayload
    };
  });

  // Cache hit rate per provider.
  // Anthropic/OpenAI: cache_read / (cache_creation + cache_read) — conservative, misses fresh input.
  // DeepSeek: cache_hit / (cache_hit + cache_miss).
  // Google: cachedContentTokenCount / total billed input (approx via read field).
  // Sample size floor: only compute when >= 30 requests to avoid spurious rates.
  const cacheHitRateByProvider: Record<string, number> = {};
  for (const [prov, s] of Object.entries(cacheByProvider)) {
    if (s.count < 30) {
      cacheHitRateByProvider[prov] = -1; // sentinel: insufficient data
      continue;
    }
    if (prov === "deepseek") {
      const denom = s.hit + s.miss;
      cacheHitRateByProvider[prov] = denom > 0 ? s.hit / denom : 0;
    } else {
      const denom = s.creation + s.read;
      cacheHitRateByProvider[prov] = denom > 0 ? s.read / denom : 0;
    }
  }

  // Tier separation: measured (Synapse Proxy interventions) vs opportunity (could-save-but-don't-yet).
  // The cache hit rate is the only signal we can act on for opportunity:
  // providers with high cache_read rate are at risk of L3 invalidating their prefix.
  const measuredSavings = {
    l1L2Hits: cacheHitDistribution.L1 + cacheHitDistribution.L2,
    l0Coalesced: cacheHitDistribution.L0,
    l3Compressions: cacheHitDistribution.L3,
  };
  const opportunitySavings = {
    // Heuristic: if read > 4 * creation, the provider is mostly using cache_read,
    // which means L3 risk of invalidating their cache prompt is HIGH.
    highCacheReadProviders: Object.entries(cacheByProvider)
      .filter(([_, s]) => s.read > 0 && s.read > s.creation * 4)
      .map(([p]) => p),
  };

  // Compute the cost-savings interval (conservative / brut) using the per-class breakdown.
  // Conservative: assume all savings came from cache_read (cheapest class) — minimum.
  // Brut: assume all savings came from input_fresh (most expensive) — maximum.
  // Real: sum of the 4 classes (what we actually measured).
  const totalSavingsReal = totalSavingsByClass.inputFresh + totalSavingsByClass.cacheRead +
    totalSavingsByClass.cacheCreation + totalSavingsByClass.output;

  return NextResponse.json({
    logs: formattedLogs,
    chartData,
    totalTokensSent,
    totalTokensOptimized,
    cacheHitDistribution,
    savingsByProvider,
    cacheByProvider,
    cacheHitRateByProvider,
    totalCacheCreation,
    totalCacheRead,
    totalCacheHit,
    totalCacheMiss,
    measuredSavings,
    opportunitySavings,
    intentDistribution,
    // Per-class savings (the full breakdown)
    totalSavingsByClass,
    savingsByClassByProvider,
    totalSavingsReal,
    pagination: {
      currentPage: page,
      totalPages: totalPages === 0 ? 1 : totalPages
    }
  });
}
