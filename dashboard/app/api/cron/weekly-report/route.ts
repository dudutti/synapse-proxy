import { NextRequest, NextResponse } from "next/server";
import { prisma } from "@/lib/prisma";
import { sendEmail } from "@/lib/email";

export const maxDuration = 60; // Max execution time (Vercel)

export async function GET(req: NextRequest) {
  // Simple Authorization via Bearer token to protect this route
  const authHeader = req.headers.get("authorization");
  if (authHeader !== `Bearer ${process.env.CRON_SECRET}`) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  try {
    // 1. Get the date 7 days ago
    const sevenDaysAgo = new Date();
    sevenDaysAgo.setDate(sevenDaysAgo.getDate() - 7);

    // 2. Fetch all users who have an active account.
    const users = await prisma.user.findMany({
      select: { id: true, name: true, email: true },
    });

    let emailsSent = 0;

    for (const user of users) {
      // 3. Aggregate RequestLogs for this user over the last 7 days.
      //    NOTE: there is no tokensIn / tokensOut column on RequestLog —
      //    the real columns are promptTokensOrig / completionTokensOrig
      //    (and their promptTokensOpt / completionTokensOpt siblings).
      //    We sum everything in one SQL pass instead of loading each row
      //    into JS — this avoids the O(N × user) N+1 from the previous
      //    implementation that did `findMany + JS loop`.
      const userKeys = await prisma.apiKey.findMany({
        where: { userId: user.id },
        select: { id: true },
      });
      const keyIds = userKeys.map((k) => k.id);

      if (keyIds.length === 0) continue;

      const [totals, byLevel] = await Promise.all([
        prisma.requestLog.aggregate({
          where: {
            apiKeyId: { in: keyIds },
            createdAt: { gte: sevenDaysAgo },
          },
          _sum: {
            promptTokensOrig: true,
            completionTokensOrig: true,
            promptTokensOpt: true,
            completionTokensOpt: true,
            costSaved: true,
          },
          _count: { _all: true },
        }),
        prisma.requestLog.groupBy({
          by: ["cacheLevel"],
          where: {
            apiKeyId: { in: keyIds },
            createdAt: { gte: sevenDaysAgo },
          },
          _count: { _all: true },
        }),
      ]);

      const totalRequests = totals._count._all;
      if (totalRequests === 0) continue;

      const sum = totals._sum;
      const promptOrig = sum.promptTokensOrig ?? 0;
      const completionOrig = sum.completionTokensOrig ?? 0;
      const promptOpt = sum.promptTokensOpt ?? 0;
      const completionOpt = sum.completionTokensOpt ?? 0;
      const totalOrig = promptOrig + completionOrig;
      const totalOpt = promptOpt + completionOpt;
      const totalTokensSaved = Math.max(0, totalOrig - totalOpt);

      let l1l2Hits = 0;
      let l3Hits = 0;
      for (const row of byLevel) {
        if (row.cacheLevel === "L1" || row.cacheLevel === "L2") {
          l1l2Hits += row._count._all;
        } else if (row.cacheLevel === "L3") {
          l3Hits += row._count._all;
        }
      }
      const hitRate = Math.round((l1l2Hits / totalRequests) * 100);

      // Use the real costSaved column instead of a $10/1M token estimate.
      // costSaved is already a dollar amount (Float) computed at telemetry
      // ingest time using ProviderModel pricing.
      const dollarsSaved = (sum.costSaved ?? 0).toFixed(2);

      await sendEmail({
        to: user.email,
        templateId: "weekly_report",
        variables: {
          NAME: user.name || "Utilisateur",
          TOKENS_SAVED: totalTokensSaved.toLocaleString(),
          DOLLARS_SAVED: dollarsSaved,
          CACHE_HIT_RATE: hitRate.toString(),
          L3_HITS: l3Hits.toLocaleString(),
          TOTAL_REQUESTS: totalRequests.toLocaleString(),
          DASHBOARD_URL: "https://synapse-proxy.com",
        },
      });
      emailsSent++;
    }

    return NextResponse.json({ success: true, emailsSent });
  } catch (error) {
    console.error("Weekly report cron error:", error);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}