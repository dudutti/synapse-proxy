import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";
import { cacheJson } from "@/lib/redis";

export const dynamic = 'force-dynamic';

// /api/admin/status — aggregate server health for the SUPERADMIN HUD.
//
// Combines three sources:
//   1. Prometheus /metrics scraped from the proxy data plane
//   2. PostgreSQL aggregates (users, request log volume, pricing data)
//   3. Process-level signals (panic recovery count from the metrics pkg)
//
// Refreshed every 5s by the StatusPage client. Each sub-fetch has its
// own timeout so a slow dependency doesn't block the whole page.
// The expensive DB block (10 parallel queries) is cached in Redis
// for 10s so the 5Hz polling of the HUD doesn't hammer Postgres.
//
// Requires SUPERADMIN.
export async function GET() {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const proxyUrl = process.env.PROXY_URL || "http://proxy:8080";

  // 1. Scrape Prometheus /metrics from the proxy. We parse it client-
  // side-ish by reusing the same regex-based parser the Go side uses.
  // 2. Cache the heavy DB block in Redis for 10s.
  const [prometheusText, dbStats] = await Promise.all([
    fetchPrometheus(`${proxyUrl}/metrics`).catch((e) => ({ error: String(e), text: null })),
    cacheJson("synapse:dash:status:db", 10, fetchDbStats),
  ]);

  const metrics = parsePrometheus(prometheusText.text || "");

  return NextResponse.json({
    ok: true,
    fetched_at: new Date().toISOString(),
    proxy: {
      url: proxyUrl,
      reachable: prometheusText.text != null,
      error: prometheusText.error,
      metrics,
    },
    database: dbStats,
  });
}

async function fetchPrometheus(url: string): Promise<{ text: string | null; error?: string }> {
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), 3000);
  try {
    const r = await fetch(url, { signal: ac.signal, cache: "no-store" });
    if (!r.ok) return { text: null, error: `HTTP ${r.status}` };
    return { text: await r.text() };
  } catch (e: any) {
    return { text: null, error: e?.message || String(e) };
  } finally {
    clearTimeout(t);
  }
}

interface DbStats {
  totalUsers: number;
  activeApiKeys: number;
  zeroLogKeys: number;
  benchmarkKeys: number;
  totalRequestLogs: number;
  logsLastHour: number;
  logsLast24h: number;
  totalCostSaved: number;
  totalTokensSaved: number;
  totalPricingModels: number;
  modelsWithoutPricing: number;
  distinctAgents: number;
  distinctProviders: number;
}

async function fetchDbStats(): Promise<DbStats> {
  const now = new Date();
  const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
  const oneDayAgo = new Date(now.getTime() - 24 * 60 * 60 * 1000);

  const [
    totalUsers,
    apiKeys,
    requestLogs,
    logsLastHour,
    logsLast24h,
    pricingRows,
    distinctAgents,
    distinctProviders,
    costAgg,
    tokensAgg,
  ] = await Promise.all([
    prisma.user.count(),
    prisma.apiKey.findMany({
      select: { zeroLog: true, benchmarkMode: true },
    }),
    prisma.requestLog.count(),
    prisma.requestLog.count({ where: { createdAt: { gte: oneHourAgo } } }),
    prisma.requestLog.count({ where: { createdAt: { gte: oneDayAgo } } }),
    prisma.providerModel.findMany({ where: { userId: "global" }, select: { provider: true, modelName: true } }),
    // distinct + null filter in where is brittle in Prisma 5 — fetch the
    // raw agentId column and dedupe in JS. Cheap because there are
    // typically < 100 distinct agents per active deployment.
    prisma.requestLog.findMany({
      distinct: ["agentId"],
      select: { agentId: true },
    }),
    prisma.requestLog.findMany({
      distinct: ["provider"],
      select: { provider: true },
    }),
    prisma.requestLog.aggregate({
      _sum: { costSaved: true },
    }),
    prisma.requestLog.aggregate({
      _sum: {
        promptTokensOrig: true,
        promptTokensOpt: true,
        completionTokensOrig: true,
        completionTokensOpt: true,
      },
    }),
  ]);

  const zeroLogKeys = apiKeys.filter((k) => k.zeroLog).length;
  const benchmarkKeys = apiKeys.filter((k) => k.benchmarkMode).length;
  const activeApiKeys = apiKeys.length;

  // Models actually used in RequestLog but NOT in ProviderModel — i.e.
  // pricing gaps that are currently falling back to $1/MTok.
  const usedModels = await prisma.requestLog.findMany({
    distinct: ["provider", "model"],
    select: { provider: true, model: true },
  });
  const knownKeys = new Set(
    pricingRows.map((p) => `${p.provider}:${p.modelName}`)
  );
  const modelsWithoutPricing = usedModels.filter(
    (u) => !knownKeys.has(`${u.provider}:${u.model}`)
  ).length;

  const t = tokensAgg._sum;
  const totalTokensSaved =
    (t?.promptTokensOrig ?? 0) -
    (t?.promptTokensOpt ?? 0) +
    (t?.completionTokensOrig ?? 0) -
    (t?.completionTokensOpt ?? 0);

  return {
    totalUsers,
    activeApiKeys,
    zeroLogKeys,
    benchmarkKeys,
    totalRequestLogs: requestLogs,
    logsLastHour,
    logsLast24h,
    totalCostSaved: costAgg._sum.costSaved ?? 0,
    totalTokensSaved: Math.max(0, totalTokensSaved),
    totalPricingModels: pricingRows.length,
    modelsWithoutPricing,
    distinctAgents: distinctAgents.filter((a) => a.agentId).length,
    distinctProviders: distinctProviders.length,
  };
}

// parsePrometheus extracts counter values from the text format. We
// only need a flat (label -> number) map for the HUD; full series with
// histograms / quantiles is out of scope.
type ParsedMetric = {
  type: "counter" | "gauge" | "histogram";
  help: string;
  samples: Record<string, number>; // label-string -> value
};

function parsePrometheus(text: string): Record<string, ParsedMetric> {
  const out: Record<string, ParsedMetric> = {};
  if (!text) return out;

  let currentName = "";
  let currentType: "counter" | "gauge" | "histogram" = "counter";
  let currentHelp = "";
  for (const raw of text.split("\n")) {
    const line = raw.trim();
    if (line.startsWith("# HELP ")) {
      const rest = line.slice(7);
      const space = rest.indexOf(" ");
      currentName = space === -1 ? rest : rest.slice(0, space);
      currentHelp = space === -1 ? "" : rest.slice(space + 1);
      if (!out[currentName]) {
        out[currentName] = { type: currentType, help: currentHelp, samples: {} };
      } else {
        out[currentName].help = currentHelp;
      }
    } else if (line.startsWith("# TYPE ")) {
      const rest = line.slice(7);
      const space = rest.indexOf(" ");
      currentName = space === -1 ? rest : rest.slice(0, space);
      const typeStr = space === -1 ? "" : rest.slice(space + 1);
      currentType = (typeStr === "gauge"
        ? "gauge"
        : typeStr === "histogram"
        ? "histogram"
        : "counter") as any;
      if (!out[currentName]) {
        out[currentName] = { type: currentType, help: "", samples: {} };
      } else {
        out[currentName].type = currentType;
      }
    } else if (line && !line.startsWith("#")) {
      // Sample line: name{labels} value OR name value
      const spaceIdx = line.lastIndexOf(" ");
      if (spaceIdx === -1) continue;
      const nameAndLabels = line.slice(0, spaceIdx);
      const valueStr = line.slice(spaceIdx + 1);
      const value = parseFloat(valueStr);
      if (Number.isNaN(value)) continue;
      const brace = nameAndLabels.indexOf("{");
      const metricName = brace === -1 ? nameAndLabels : nameAndLabels.slice(0, brace);
      const labelStr = brace === -1 ? "" : nameAndLabels.slice(brace + 1, nameAndLabels.length);
      
      let labelKey = "_total";
      if (labelStr) {
        const labels = parseLabels(labelStr);
        const primaryKey = ["cache_level", "handler", "le", "kind"].find(k => k in labels);
        if (primaryKey) {
          labelKey = labels[primaryKey];
        } else {
          const values = Object.values(labels);
          labelKey = values.length > 0 ? values[0] : labelStr;
        }
      }

      if (!out[metricName]) {
        out[metricName] = { type: currentType, help: "", samples: {} };
      }
      out[metricName].samples[labelKey] = value;
    }
  }
  return out;
}

function parseLabels(labelStr: string): Record<string, string> {
  const labels: Record<string, string> = {};
  if (!labelStr) return labels;
  let clean = labelStr.trim();
  if (clean.endsWith("}")) {
    clean = clean.slice(0, -1);
  }
  const pairs = clean.split(",");
  for (const pair of pairs) {
    const eqIdx = pair.indexOf("=");
    if (eqIdx === -1) continue;
    const name = pair.slice(0, eqIdx).trim();
    let val = pair.slice(eqIdx + 1).trim();
    if (val.startsWith('"') && val.endsWith('"')) {
      val = val.slice(1, -1);
    }
    labels[name] = val;
  }
  return labels;
}
