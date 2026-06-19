import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/admin/alerts — CRUD for AlertRule + list of recent AlertEvents.
// SUPERADMIN only.

export async function GET() {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const [rules, recentEvents, unackedCount] = await Promise.all([
    prisma.alertRule.findMany({
      orderBy: { createdAt: "desc" },
    }),
    prisma.alertEvent.findMany({
      orderBy: { firedAt: "desc" },
      take: 50,
    }),
    prisma.alertEvent.count({ where: { acknowledged: false } }),
  ]);

  return NextResponse.json({ rules, recentEvents, unackedCount });
}

export async function POST(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const body = await req.json();
  const { name, metric, operator, threshold, windowSec, enabled, severity, notifyEmail, notifySlack } = body;

  if (!name || !metric || !operator || typeof threshold !== "number") {
    return NextResponse.json({ error: "name, metric, operator and threshold are required" }, { status: 400 });
  }
  if (!["gt", "lt", "gte", "lte"].includes(operator)) {
    return NextResponse.json({ error: "operator must be gt|lt|gte|lte" }, { status: 400 });
  }
  if (!["panic_rate", "error_rate", "cache_hit_rate", "upstream_latency_p95", "pricing_gaps"].includes(metric)) {
    return NextResponse.json({ error: `unknown metric ${metric}` }, { status: 400 });
  }

  const rule = await prisma.alertRule.create({
    data: {
      name,
      metric,
      operator,
      threshold,
      windowSec: windowSec || 300,
      enabled: enabled !== false,
      severity: severity || "warning",
      notifyEmail: notifyEmail || null,
      notifySlack: notifySlack || null,
    },
  });

  return NextResponse.json({ rule }, { status: 201 });
}
