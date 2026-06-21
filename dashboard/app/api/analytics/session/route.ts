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

  const apiKeys = await prisma.apiKey.findMany({ where: { userId: user.id } });
  const url = new URL(req.url);
  const keyIdParam = url.searchParams.get("keyId");
  const sessionIdParam = url.searchParams.get("sessionId"); // optional: filter by sessionId

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
    return NextResponse.json({ error: "No API Keys found" }, { status: 400 });
  }

  const startParam = url.searchParams.get("start");
  const endParam = url.searchParams.get("end");

  if (!startParam || !endParam) {
    return NextResponse.json({ error: "Start and end timestamps are required" }, { status: 400 });
  }

  const startDate = new Date(parseInt(startParam, 10));
  const endDate = new Date(parseInt(endParam, 10));

  // Build Prisma where clause. sessionId filter is optional.
  const where: any = {
    createdAt: { gte: startDate, lte: endDate },
  };
  if (!isSuper) {
    where.apiKeyId = { in: keyIds };
  } else if (keyIdParam) {
    where.apiKeyId = keyIdParam;
  }
  if (sessionIdParam) {
    where.sessionId = sessionIdParam;
  }

  // Fetch raw logs (capped at 5000 rows to keep the response bounded;
  // the dashboard only needs aggregates for sessions that fit in 5000 reqs).
  const logs = await prisma.requestLog.findMany({
    where,
    orderBy: { createdAt: "asc" },
    take: 5000,
  });

  // Fetch dynamic model pricing from Superadmin configuration
  const modelsPricing = await prisma.providerModel.findMany({
    where: { OR: [{ userId: "global" }, { userId: user.id }] }
  });
  const pricingMap = new Map();
  // Sort so global is applied first, then user's custom pricing overwrites it
  modelsPricing.sort((a, b) => a.userId === "global" ? -1 : 1).forEach(m => {
    pricingMap.set(`${m.provider}_${m.modelName}`, m);
  });

  // === AGGREGATES ===
  let totalRequests = 0;
  let totalTokensSent = { total: 0, input: 0, output: 0 };
  let totalTokensOptimized = { total: 0, input: 0, output: 0 };
  let cacheHitDistribution: Record<string, number> = { MISS: 0, L0: 0, L1: 0, L2: 0, L3: 0, LOOP: 0 };
  let byClass = {
    inputFresh: 0, // $ saved
    cacheRead: 0, // $ saved
    cacheCreation: 0, // $ saved
    output: 0, // $ saved
  };
  let totalCostSaved = 0;
  let totalCostWithout = 0;
  let totalCostWith = 0;
  let totalCacheCreationTokens = 0;
  let totalCacheReadTokens = 0;
  let totalCacheHitTokens = 0;
  let totalCacheMissTokens = 0;
  let totalDurationMs = 0;
  let totalReasoningTokens = 0;

  // Per-provider / per-model / per-agent breakdowns
  const byProvider: Record<
    string,
    { requests: number; costSaved: number; costWithout: number; costWith: number; tokensIn: number; tokensOut: number; cacheHits: number }
  > = {};
  const byModel: Record<string, { requests: number; costSaved: number }> = {};
  const byAgent: Record<string, { requests: number; costSaved: number; label: string }> = {};

  const requestsDetail: Array<any> = [];
  const topExpensive: Array<{
    id: string;
    ts: string;
    model: string;
    cacheLevel: string;
    promptTokensOrig: number;
    completionTokensOrig: number;
    promptTokensOpt: number;
    completionTokensOpt: number;
    costWithout: number;
    costWith: number;
    costSaved: number;
    agentId: string;
    durationMs: number;
  }> = [];

  for (const log of logs) {
    totalRequests++;
    const origIn = log.promptTokensOrig || 0;
    const origOut = log.completionTokensOrig || 0;
    const optIn = log.promptTokensOpt || 0;
    const optOut = log.completionTokensOpt || 0;
    totalTokensSent.total += origIn + origOut;
    totalTokensSent.input += origIn;
    totalTokensSent.output += origOut;
    totalTokensOptimized.total += optIn + optOut;
    totalTokensOptimized.input += optIn;
    totalTokensOptimized.output += optOut;

    const lvl = log.cacheLevel || "NONE";
    cacheHitDistribution[lvl] = (cacheHitDistribution[lvl] || 0) + 1;
    if (lvl !== "NONE") {
      // anything that wasn't a MISS is a "cache hit" in the broad sense
    }
    totalCacheCreationTokens += log.cacheCreationTokens || 0;
    totalCacheReadTokens += log.cacheReadTokens || 0;
    totalCacheHitTokens += log.cacheHitTokens || 0;
    totalCacheMissTokens += log.cacheMissTokens || 0;
    totalDurationMs += log.durationMs || 0;
    totalReasoningTokens += log.reasoningTokens || 0;

    byClass.inputFresh += log.savingsInputFresh || 0;
    byClass.cacheRead += log.savingsCacheRead || 0;
    byClass.cacheCreation += log.savingsCacheCreation || 0;
    byClass.output += log.savingsOutput || 0;

    // Use the costSaved already computed by the proxy (sum of the 4
    // per-class savings, computed in `utils.CalculateSavingsByClass`).
    // It already accounts for cache_read/cache_creation at their
    // proper rates, and can be negative for some requests.
    totalCostSaved += log.costSaved || 0;

    // Estimate "what the user would have paid without Synapse Proxy" using
    // current pricing. The cost saved from above is already
    // computed at the time the request was made (against the same
    // pricing table), so the two values should agree up to rounding.
    const modelPrice = pricingMap.get(`${log.provider}_${log.model}`);
    const promptPrice = modelPrice ? modelPrice.costPromptPer1M : 1.0;
    const compPrice = modelPrice ? modelPrice.costCompletionPer1M : 1.0;
    const costWithout = (origIn / 1000000.0) * promptPrice + (origOut / 1000000.0) * compPrice;
    const costWith = (optIn / 1000000.0) * promptPrice + (optOut / 1000000.0) * compPrice;
    totalCostWithout += costWithout;
    totalCostWith += costWith;

    // Per-provider
    if (!byProvider[log.provider]) {
      byProvider[log.provider] = {
        requests: 0,
        costSaved: 0,
        costWithout: 0,
        costWith: 0,
        tokensIn: 0,
        tokensOut: 0,
        cacheHits: 0,
      };
    }
    byProvider[log.provider].requests++;
    byProvider[log.provider].costSaved += log.costSaved || 0;
    byProvider[log.provider].costWithout += costWithout;
    byProvider[log.provider].costWith += costWith;
    byProvider[log.provider].tokensIn += origIn;
    byProvider[log.provider].tokensOut += origOut;
    if (lvl !== "NONE") byProvider[log.provider].cacheHits++;

    // Per-model
    if (!byModel[log.model]) byModel[log.model] = { requests: 0, costSaved: 0 };
    byModel[log.model].requests++;
    byModel[log.model].costSaved += log.costSaved || 0;

    // Per-agent
    if (!byAgent[log.agentId || "unknown"])
      byAgent[log.agentId || "unknown"] = { requests: 0, costSaved: 0, label: log.agentLabel || "" };
    byAgent[log.agentId || "unknown"].requests++;
    byAgent[log.agentId || "unknown"].costSaved += log.costSaved || 0;

    // Extract system prompt from originalPayload if available
    let systemPrompt = "";
    if (log.originalPayload) {
      try {
        const parsed = JSON.parse(log.originalPayload);
        if (parsed.messages && Array.isArray(parsed.messages) && parsed.messages.length > 0) {
          if (parsed.messages[0].role === "system") {
            systemPrompt = parsed.messages[0].content;
          }
        }
      } catch (e) {}
    }

    requestsDetail.push({
      id: log.id,
      ts: log.createdAt.toISOString(),
      model: log.model,
      cacheLevel: lvl,
      promptTokensOrig: origIn,
      promptTokensOpt: optIn,
      durationMs: log.durationMs || 0,
      toolCalls: log.toolCalls ? JSON.parse(log.toolCalls) : null,
      systemPrompt: systemPrompt,
      turnCount: log.turnCount || 0,
      convSignature: log.convSignature || "",
    });

    // Top expensive (cap at 10)
    if (topExpensive.length < 10) {
      topExpensive.push({
        id: log.id,
        ts: log.createdAt.toISOString(),
        model: log.model,
        cacheLevel: lvl,
        promptTokensOrig: origIn,
        completionTokensOrig: origOut,
        promptTokensOpt: optIn,
        completionTokensOpt: optOut,
        costWithout,
        costWith,
        costSaved: log.costSaved || 0,
        agentId: log.agentId || "",
        durationMs: log.durationMs || 0,
      });
    }
  }

  // === SORT TOP EXPENSIVE BY costSaved DESC (largest savings first) ===
  topExpensive.sort((a, b) => b.costSaved - a.costSaved);

  // === HIT RATE ===
  const cacheHitsTotal = totalRequests - (cacheHitDistribution["NONE"] || 0);
  const hitRate = totalRequests > 0 ? (cacheHitsTotal / totalRequests) * 100 : 0;

  return NextResponse.json({
    sessionStart: startDate.toISOString(),
    sessionEnd: endDate.toISOString(),
    durationMs: endDate.getTime() - startDate.getTime(),
    totalRequests,
    hitRate,
    cacheHitDistribution,
    tokens: {
      original: totalTokensSent,
      optimized: totalTokensOptimized,
    },
    savingsByClass: byClass,
    costs: {
      withoutCache: totalCostWithout,
      withCache: totalCostWith,
      saved: totalCostSaved,
    },
    cacheTokens: {
      creation: totalCacheCreationTokens,
      read: totalCacheReadTokens,
      hit: totalCacheHitTokens,
      miss: totalCacheMissTokens,
    },
    byProvider,
    byModel,
    byAgent,
    topExpensive,
    requests: requestsDetail,
    totalDurationMs,
    avgDurationMs: totalRequests > 0 ? totalDurationMs / totalRequests : 0,
    totalReasoningTokens,
  });
}
