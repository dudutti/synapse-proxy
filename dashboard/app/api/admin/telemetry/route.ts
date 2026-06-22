import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import { cacheJson } from "@/lib/redis";

const GLOBAL_CITIES = [
  { lat: 40.7128, lng: -74.0060 }, // New York
  { lat: 34.0522, lng: -118.2437 }, // Los Angeles
  { lat: 51.5074, lng: -0.1278 }, // London
  { lat: 48.8566, lng: 2.3522 }, // Paris
  { lat: 35.6762, lng: 139.6503 }, // Tokyo
  { lat: 1.3521, lng: 103.8198 }, // Singapore
  { lat: -33.8688, lng: 151.2093 }, // Sydney
  { lat: 37.7749, lng: -122.4194 }, // San Francisco
  { lat: 52.5200, lng: 13.4050 }, // Berlin
  { lat: 19.0760, lng: 72.8777 }, // Mumbai
  { lat: -23.5505, lng: -46.6333 }, // Sao Paulo
  { lat: 55.7558, lng: 37.6173 }, // Moscow
  { lat: 25.2048, lng: 55.2708 }, // Dubai
  { lat: -34.6037, lng: -58.3816 }, // Buenos Aires
  { lat: -1.2921, lng: 36.8219 }, // Nairobi
];

function getRandomCity(seed: string) {
  // Simple deterministic random based on log ID
  let hash = 0;
  for (let i = 0; i < seed.length; i++) {
    hash = seed.charCodeAt(i) + ((hash << 5) - hash);
  }
  const idx = Math.abs(hash) % GLOBAL_CITIES.length;
  return GLOBAL_CITIES[idx];
}

export async function GET(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user || user.role !== "SUPERADMIN") {
    return NextResponse.json({ error: "Unauthorized" }, { status: 403 });
  }

  // The telemetry payload is a full-table groupBy + aggregate + 50-row
  // ORDER BY createdAt DESC every time the home page polls it. Cache
  // the whole computed payload for 30s — the home page is fine with
  // 30s freshness on the cumulative counters.
  const payload = await cacheJson("synapse:dash:telemetry:v1", 30, async () => {
    // 1. Fetch Aggregated Global Stats
    const cacheStats = await prisma.requestLog.groupBy({
      by: ['cacheLevel'],
      _count: {
        _all: true,
      },
    });

    const totals = await prisma.requestLog.aggregate({
      _sum: {
        promptTokensOrig: true,
        completionTokensOrig: true,
        promptTokensOpt: true,
        completionTokensOpt: true,
        costSaved: true,
      },
      _count: {
        id: true,
      }
    });

    const distribution = { MISS: 0, L0: 0, L1: 0, L2: 0, L3: 0 };
    cacheStats.forEach(stat => {
      if (stat.cacheLevel === "NONE") distribution.MISS = stat._count._all;
      else if (stat.cacheLevel === "L0") distribution.L0 = stat._count._all;
      else if (stat.cacheLevel === "L1") distribution.L1 = stat._count._all;
      else if (stat.cacheLevel === "L2") distribution.L2 = stat._count._all;
      else if (stat.cacheLevel === "L3") distribution.L3 = stat._count._all;
    });

    const sum = totals._sum;
    const originalInput = sum.promptTokensOrig || 0;
    const originalOutput = sum.completionTokensOrig || 0;
    const optimizedInput = sum.promptTokensOpt || 0;
    const optimizedOutput = sum.completionTokensOpt || 0;

    const totalTokensPassed = originalInput + originalOutput;
    const totalTokensSaved = totalTokensPassed - (optimizedInput + optimizedOutput);

    const stats = {
      totalRequests: totals._count.id || 0,
      totalTokensPassed,
      totalTokensSaved,
      totalCostSaved: sum.costSaved || 0,
      distribution
    };

    // 2. Fetch Recent Logs for Globe Markers
    const recentLogs = await prisma.requestLog.findMany({
      orderBy: { createdAt: 'desc' },
      take: 50,
      select: {
        id: true,
        cacheLevel: true,
        createdAt: true
      }
    });

    const markers = recentLogs.map(log => {
      const city = getRandomCity(log.id);
      let size = 0.05;
      let color = [1, 0, 0]; // Red for MISS by default

      if (log.cacheLevel === "L1" || log.cacheLevel === "L2") {
        color = [0.2, 1, 0.2]; // Bright Green for Cache Hit
        size = 0.08;
      } else if (log.cacheLevel === "L3") {
        color = [0.7, 0.2, 1]; // Purple for Compression
        size = 0.06;
      }

      return {
        location: [city.lat, city.lng],
        size,
        color
      };
    });

    return { stats, markers };
  });

  return NextResponse.json(payload);
}
