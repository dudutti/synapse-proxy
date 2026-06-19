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
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const url = new URL(req.url);
  const unackedOnly = url.searchParams.get("unacked") === "1";

  const events = await prisma.alertEvent.findMany({
    where: unackedOnly ? { acknowledged: false } : undefined,
    orderBy: { firedAt: "desc" },
    take: 100,
  });

  return NextResponse.json({ events });
}

// POST: acknowledge an event.
// body: { id: string, by: string }
export async function POST(req: Request) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const { id, by } = await req.json();
  if (!id) {
    return NextResponse.json({ error: "id required" }, { status: 400 });
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
