import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import { createClient } from "redis";

export async function PUT(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

  const body = await req.json();
  const { enable, keyIds } = body;

  const redisUrl = process.env.REDIS_URL || "redis://localhost:6379";
  const redis = createClient({ url: redisUrl });
  await redis.connect();

  let modifiedKeyIds: string[] = [];

  try {
    if (enable) {
      // Find all user keys
      const userKeys = await prisma.apiKey.findMany({ where: { userId: user.id } });
      const keysToEnable = userKeys.filter(k => k.benchmarkMode === false);
      
      modifiedKeyIds = keysToEnable.map(k => k.id);

      for (const key of keysToEnable) {
        await prisma.apiKey.update({
          where: { id: key.id },
          data: { benchmarkMode: true }
        });
        await redis.hSet(`synapse:keys:${key.virtualKey}`, "benchmark_mode", "true");
      }
    } else {
      // Disable only for specific keys
      if (!keyIds || !Array.isArray(keyIds)) {
        await redis.disconnect();
        return NextResponse.json({ error: "Missing keyIds array" }, { status: 400 });
      }

      for (const kid of keyIds) {
        const key = await prisma.apiKey.findUnique({ where: { id: kid } });
        if (key && key.userId === user.id) {
          await prisma.apiKey.update({
            where: { id: kid },
            data: { benchmarkMode: false }
          });
          await redis.hSet(`synapse:keys:${key.virtualKey}`, "benchmark_mode", "false");
        }
      }
    }

    await redis.disconnect();
    return NextResponse.json({ success: true, modifiedKeyIds });
  } catch (error: any) {
    await redis.disconnect();
    return NextResponse.json({ error: error.message }, { status: 500 });
  }
}
