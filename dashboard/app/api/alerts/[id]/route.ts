import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/admin/alerts/[id] — update or delete a rule.
// SUPERADMIN only.

export async function PATCH(
  req: Request,
  { params }: { params: { id: string } }
) {
  const session = await getServerSession(authOptions);
  if (!session?.user) {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const user = session.user as any;
  const isSuper = user.role === "SUPERADMIN";

  const existing = await prisma.alertRule.findUnique({ where: { id: params.id } });
  if (!existing) return new NextResponse("Not Found", { status: 404 });
  if (!isSuper && existing.userId !== user.id) {
    return new NextResponse("Forbidden", { status: 403 });
  }

  const body = await req.json();
  const allowed: any = {};
  for (const k of ["name", "metric", "operator", "threshold", "windowSec", "enabled", "severity", "notifyEmail", "notifySlack"]) {
    if (body[k] !== undefined) allowed[k] = body[k];
  }

  const rule = await prisma.alertRule.update({
    where: { id: params.id },
    data: allowed,
  });

  return NextResponse.json({ rule });
}

export async function DELETE(
  _req: Request,
  { params }: { params: { id: string } }
) {
  const session = await getServerSession(authOptions);
  if (!session?.user) {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const user = session.user as any;
  const isSuper = user.role === "SUPERADMIN";

  const existing = await prisma.alertRule.findUnique({ where: { id: params.id } });
  if (!existing) return new NextResponse("Not Found", { status: 404 });
  if (!isSuper && existing.userId !== user.id) {
    return new NextResponse("Forbidden", { status: 403 });
  }

  await prisma.alertRule.delete({ where: { id: params.id } });
  return NextResponse.json({ ok: true });
}
