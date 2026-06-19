import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

  const url = new URL(req.url);
  const page = parseInt(url.searchParams.get("page") || "1", 10);
  const limit = parseInt(url.searchParams.get("limit") || "10", 10);
  const skip = (page - 1) * limit;

  const logs = await prisma.benchmarkLog.findMany({
    where: { apiKey: { userId: user.id } },
    orderBy: { createdAt: 'desc' },
    skip: skip,
    take: limit
  });

  const totalLogs = await prisma.benchmarkLog.count({
    where: { apiKey: { userId: user.id } }
  });
  const totalPages = Math.ceil(totalLogs / limit);

  return NextResponse.json({
    data: logs,
    pagination: {
      currentPage: page,
      totalPages: totalPages === 0 ? 1 : totalPages
    }
  });
}
