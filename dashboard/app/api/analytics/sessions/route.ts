import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import { Prisma } from "@prisma/client";

export const dynamic = "force-dynamic";

// List every past "Record Session" the user has ever run.
//
// We don't have a dedicated SessionRecord table yet — sessions are
// just RequestLog rows that share the same `sessionId` field. So
// this query groups by `sessionId` and aggregates the per-class
// metrics we already track in the per-request row. The result is
// a list of one row per session, with all the fields the dashboard
// needs to render the history list (and the per-session summary
// modal on click).
//
// The `sessionId = ''` group is excluded — it represents "no
// recording" traffic (the vast majority of requests) and is not
// useful in this view.
export async function GET(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

  const isSuper = user.role === "SUPERADMIN";

  const userKeyIds = (await prisma.apiKey.findMany({ where: { userId: user.id } })).map((k) => k.id);
  if (!isSuper && userKeyIds.length === 0) {
    return NextResponse.json({ sessions: [] });
  }

  const url = new URL(req.url);
  const limit = Math.min(parseInt(url.searchParams.get("limit") || "50", 10), 200);

  // Aggregate per session. The grouping is by `sessionId`; the
  // empty-string bucket is excluded via `where.sessionId.not`.
  // We use Prisma's $queryRaw to express the GROUP BY directly
  // because Prisma's groupBy does not support custom aggregations
  // on numeric columns the way we need (sum, count, plus a
  // "startedAt = MIN(createdAt)" and "endedAt = MAX(createdAt)").
  const rows = await prisma.$queryRaw<Array<{
    sessionId: string;
    startedAt: Date;
    endedAt: Date;
    totalRequests: bigint;
    promptTokensOrig: bigint;
    completionTokensOrig: bigint;
    promptTokensOpt: bigint;
    completionTokensOpt: bigint;
    costSaved: number;
    costWithout: number;
    costWith: number;
    cacheHits: bigint;
  }>>`
    SELECT
      "sessionId"::text AS "sessionId",
      MIN("createdAt") AS "startedAt",
      MAX("createdAt") AS "endedAt",
      COUNT(*)::bigint AS "totalRequests",
      COALESCE(SUM("promptTokensOrig"), 0)::bigint AS "promptTokensOrig",
      COALESCE(SUM("completionTokensOrig"), 0)::bigint AS "completionTokensOrig",
      COALESCE(SUM("promptTokensOpt"), 0)::bigint AS "promptTokensOpt",
      COALESCE(SUM("completionTokensOpt"), 0)::bigint AS "completionTokensOpt",
      COALESCE(SUM("costSaved"), 0)::float AS "costSaved",
      COALESCE(SUM("promptTokensOrig") * 0.30 / 1000000.0 + SUM("completionTokensOrig") * 1.20 / 1000000.0, 0)::float AS "costWithout",
      COALESCE(SUM("promptTokensOpt") * 0.30 / 1000000.0 + SUM("completionTokensOpt") * 1.20 / 1000000.0, 0)::float AS "costWith",
      COUNT(*) FILTER (WHERE "cacheLevel" IN ('L1','L2','L3','LOOP','L0'))::bigint AS "cacheHits"
    FROM "RequestLog"
    WHERE "sessionId" != ''
      ${!isSuper ? `AND "apiKeyId" IN (${Prisma.join(userKeyIds)})` : ""}
    GROUP BY "sessionId"
    ORDER BY MAX("createdAt") DESC
    LIMIT ${limit}
  `;

  return NextResponse.json({
    sessions: rows.map((r) => ({
      sessionId: r.sessionId,
      startedAt: r.startedAt.toISOString(),
      endedAt: r.endedAt.toISOString(),
      durationMs: r.endedAt.getTime() - r.startedAt.getTime(),
      totalRequests: Number(r.totalRequests),
      promptTokensOrig: Number(r.promptTokensOrig),
      completionTokensOrig: Number(r.completionTokensOrig),
      promptTokensOpt: Number(r.promptTokensOpt),
      completionTokensOpt: Number(r.completionTokensOpt),
      costSaved: r.costSaved,
      costWithout: r.costWithout,
      costWith: r.costWith,
      cacheHits: Number(r.cacheHits),
    })),
  });
}
