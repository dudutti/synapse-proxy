import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

// /api/admin/explorer/[id] — full payload of a single RequestLog row.
// SUPERADMIN only. Returns everything except apiKeyId (which is a
// cuid, not sensitive, but we trim it anyway for a cleaner drill-down).

export async function GET(
  _req: Request,
  { params }: { params: { id: string } }
) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  const row = await prisma.requestLog.findUnique({
    where: { id: params.id },
  });

  if (!row) {
    return NextResponse.json({ error: "Not found" }, { status: 404 });
  }

  return NextResponse.json({ row });
}
