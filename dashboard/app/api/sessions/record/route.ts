import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import { createClient } from "redis";

export const dynamic = "force-dynamic";

// Start or stop a "Record Session" for the current user.
//
// When the user clicks Start, the dashboard calls this route with
// `{ enable: true }`. The route:
//   1. Generates a stable session id (returned to the client so it
//      can be displayed in the Session Summary).
//   2. For every virtual key owned by the user, writes
//      `synapse:session:vk:<virtualKey> = <sessionId>` to Redis
//      with a 24h TTL as a safety net (the dashboard will
//      explicitly delete the keys on Stop).
//
// The Go proxy reads the key on every request and tags the
// resulting RequestLog row with the session id. This lets the
// user record a session transparently: any agent (Hermes, curl,
// Playground) using any of the user's virtual keys is recorded
// without the agent having to know about the session id.
//
// When the user clicks Stop, the route deletes the Redis keys
// (or, if a sessionId is supplied, only that specific session).

export async function POST(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

  const body = await req.json().catch(() => ({}));
  const { enable, sessionId, keyIds } = body ?? {};

  const redisUrl = process.env.REDIS_URL || "redis://localhost:6379";
  const redis = createClient({ url: redisUrl });
  await redis.connect();

  try {
    if (enable) {
      // Start: pick the keys the user wants to record. If keyIds
      // is not provided, we record on all of the user's active
      // virtual keys.
      const userKeys = keyIds && Array.isArray(keyIds) && keyIds.length > 0
        ? await prisma.apiKey.findMany({ where: { id: { in: keyIds }, userId: user.id } })
        : await prisma.apiKey.findMany({ where: { userId: user.id } });

      if (userKeys.length === 0) {
        await redis.disconnect();
        return NextResponse.json({ error: "No API keys available to record" }, { status: 400 });
      }

      // Use the sessionId provided by the client if any (so the
      // user can pre-allocate one) ”” otherwise generate a fresh
      // one. Both are short, opaque, and free of special chars.
      const sid = (sessionId && String(sessionId).length > 0)
        ? String(sessionId)
        : `session-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

      for (const key of userKeys) {
        // 24h TTL is a safety net: even if the user closes the
        // browser without clicking Stop, the session tag will
        // eventually expire instead of sticking forever.
        await redis.set(`synapse:session:vk:${key.virtualKey}`, sid, { EX: 24 * 60 * 60 });
      }

      return NextResponse.json({
        success: true,
        sessionId: sid,
        recordedKeyIds: userKeys.map((k) => k.id),
        recordedVirtualKeys: userKeys.map((k) => k.virtualKey),
      });
    } else {
      // Stop: if a sessionId is provided, delete only keys that
      // match it; otherwise delete every session tag for the
      // user's keys.
      const userKeys = await prisma.apiKey.findMany({ where: { userId: user.id } });
      let removedCount = 0;
      for (const key of userKeys) {
        const current = await redis.get(`synapse:session:vk:${key.virtualKey}`);
        if (!current) continue;
        if (sessionId && current !== sessionId) continue;
        await redis.del(`synapse:session:vk:${key.virtualKey}`);
        removedCount++;
      }
      return NextResponse.json({ success: true, removedCount });
    }
  } catch (error: any) {
    return NextResponse.json({ error: error.message ?? "session record failed" }, { status: 500 });
  } finally {
    try { await redis.disconnect(); } catch {}
  }
}
