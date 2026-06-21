import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/admin/alerts/events — list recent events, optionally filter
// by acknowledged status. Used by the AlertEvent log on the admin page.
//
// SUPERADMIN only.

export async function GET(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session?.user) {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const user = session.user as any;
  const isSuper = user.role === "SUPERADMIN";
  const targetUserId = isSuper ? "global" : user.id;

  const url = new URL(req.url);
  const unackedOnly = url.searchParams.get("unacked") === "1";

  const whereClause: any = {};
  if (unackedOnly) whereClause.acknowledged = false;
  if (!isSuper) whereClause.rule = { userId: targetUserId };

  const events = await prisma.alertEvent.findMany({
    where: whereClause,
    orderBy: { firedAt: "desc" },
    take: 100,
  });

  return NextResponse.json({ events });
}

// POST: acknowledge an event.
// body: { id: string, by: string }
export async function POST(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session?.user) {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const user = session.user as any;
  const isSuper = user.role === "SUPERADMIN";

  const { id, by } = await req.json();
  if (!id) {
    return NextResponse.json({ error: "id required" }, { status: 400 });
  }

  const existing = await prisma.alertEvent.findUnique({ where: { id }, include: { rule: true } });
  if (!existing) return new NextResponse("Not Found", { status: 404 });
  if (!isSuper && existing.rule.userId !== user.id) {
    return new NextResponse("Forbidden", { status: 403 });
  }

  const event = await prisma.alertEvent.update({
    where: { id },
    data: {
      acknowledged: true,
      acknowledgedAt: new Date(),
      acknowledgedBy: by || session.user?.email || "superadmin",
    },
  });

  return NextResponse.json({ event });
}
