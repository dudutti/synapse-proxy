import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/authOptions";
import { prisma } from "@/lib/prisma";
import { createClient } from "redis";

export async function PUT(
  req: Request,
  { params }: { params: { id: string } }
) {
  const session = await getServerSession(authOptions);
  if (!session || (session.user as any).role !== "SUPERADMIN") {
    return new NextResponse("Unauthorized", { status: 401 });
  }

  try {
    const { tier } = await req.json();
    if (!tier) {
      return NextResponse.json({ error: "Missing tier" }, { status: 400 });
    }

    const userId = params.id;

    // Update user in Postgres
    const updatedUser = await prisma.user.update({
      where: { id: userId },
      data: { tier }
    });

    // Reset limit status for user keys in Redis
    const apiKeys = await prisma.apiKey.findMany({
      where: { userId }
    });

    if (apiKeys.length > 0) {
      const redisClient = createClient({ url: process.env.REDIS_URL || 'redis://localhost:6379' });
      await redisClient.connect();
      for (const key of apiKeys) {
        await redisClient.hSet(`synapse:keys:${key.virtualKey}`, {
          tier: tier,
          limit_exceeded: "false"
        });
      }
      await redisClient.disconnect();
    }

    return NextResponse.json(updatedUser);
  } catch (error) {
    console.error("[ADMIN_USER_TIER_PUT]", error);
    return new NextResponse("Internal Error", { status: 500 });
  }
}
