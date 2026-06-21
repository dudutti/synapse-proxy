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

    // 2. Fetch all users who have an active account (e.g. at least one API key)
    const users = await prisma.user.findMany({
      select: {
        id: true,
        name: true,
        email: true,
      }
    });

    let emailsSent = 0;

    for (const user of users) {
      // 3. Aggregate RequestLogs for this user over the last 7 days
      // First, get all keys for this user
      const userKeys = await prisma.apiKey.findMany({
        where: { userId: user.id },
        select: { id: true }
      });
      const keyIds = userKeys.map(k => k.id);

      if (keyIds.length === 0) continue;

      const logs = await prisma.requestLog.findMany({
        where: {
          apiKeyId: { in: keyIds },
          createdAt: { gte: sevenDaysAgo }
        },
        select: {
          tokensIn: true,
          tokensOut: true,
          cacheLevel: true,
          // Calculate standard cost vs actual cost if you stored pricing
          // but for simplicity, we can do rough estimates based on tokens
        }
      });

      if (logs.length === 0) continue; // No activity this week

      let totalTokensSaved = 0;
      let totalL1L2Hits = 0;
      
      logs.forEach(log => {
        if (log.cacheLevel === "L1" || log.cacheLevel === "L2") {
          totalL1L2Hits++;
          // For a cache hit, we save 100% of the tokens (in + out)
          totalTokensSaved += (log.tokensIn || 0) + (log.tokensOut || 0);
        } else if (log.cacheLevel === "L3") {
          // L3 compression saves a percentage
          totalTokensSaved += Math.floor((log.tokensIn || 0) * 0.2); // rough estimate of 20%
        }
      });

      const hitRate = Math.round((totalL1L2Hits / logs.length) * 100);

      // Estimate dollars saved (assume $10/1M tokens average)
      const dollarsSaved = ((totalTokensSaved / 1_000_000) * 10).toFixed(2);

      // Send the Weekly Report email
      await sendEmail({
        to: user.email,
        templateId: "weekly_report",
        variables: {
          NAME: user.name || "Utilisateur",
          TOKENS_SAVED: totalTokensSaved.toLocaleString(),
          DOLLARS_SAVED: dollarsSaved.toString(),
          CACHE_HIT_RATE: hitRate.toString(),
          DASHBOARD_URL: "https://synapse-proxy.com"
        }
      });
      emailsSent++;
    }

    return NextResponse.json({ success: true, emailsSent });
  } catch (error) {
    console.error("Weekly report cron error:", error);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}
