import { NextResponse } from "next/server";
import { createClient } from "redis";

let redisClient: ReturnType<typeof createClient> | null = null;

async function getRedisClient() {
  if (!redisClient) {
    redisClient = createClient({
      url: process.env.REDIS_URL || "redis://localhost:6379",
    });
    redisClient.on("error", (err) => console.error("Redis Client Error", err));
    await redisClient.connect();
  }
  return redisClient;
}

export const dynamic = "force-dynamic";

export async function GET() {
  try {
    const client = await getRedisClient();
    const data = await client.get("synapse:global_stats");

    if (!data) {
      // Fallback if worker hasn't run yet
      return NextResponse.json({
        totalRequests: 0,
        totalCostSaved: 0,
        tokensSent: 0,
        tokensOptimized: 0,
        tokensPurged: 0,
        compressionRatio: 0,
        cacheDistribution: { MISS: 0, L1: 0, L2: 0, L3: 0 },
        topModels: [],
        lastUpdated: new Date().toISOString()
      });
    }

    return NextResponse.json(JSON.parse(data));
  } catch (error) {
    console.error("Global stats fetch error:", error);
    return NextResponse.json({ error: "Failed to fetch stats" }, { status: 500 });
  }
}
