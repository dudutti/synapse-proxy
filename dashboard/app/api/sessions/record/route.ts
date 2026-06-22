import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import { getRedis } from "@/lib/redis";

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

  // Use the shared singleton client. The previous implementation opened
  // a fresh Redis connection on every Start/Stop click — wasteful when
  // the user toggles several keys in a row.
  const redis = await getRedis();

  try {
    if (enable) {
      // Start: pick the keys the user wants to record. If keyIds
      // is not provided, we record on all of the user's active
      // virtual keys.
      const userKeys = keyIds && Array.isArray(keyIds) && keyIds.length > 0
        ? await prisma.apiKey.findMany({ where: { id: { in: keyIds }, userId: user.id } })
        : await prisma.apiKey.findMany({ where: { userId: user.id } });

      if (userKeys.length === 0) {
        return NextResponse.json({ error: "No API keys available to record" }, { status: 400 });
      }

      // Use the sessionId provided by the client if any (so the
      // user can pre-allocate one) — otherwise generate a fresh
      // one. Both are short, opaque, and free of special chars.
      const sid = (sessionId && String(sessionId).length > 0)
        ? String(sessionId)
        : `session-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

      if (redis) {
        // Pipeline: send all SETs in one round-trip.
        const pipeline = redis.multi();
        for (const key of userKeys) {
          // 24h TTL is a safety net: even if the user closes the
          // browser without clicking Stop, the session tag will
          // eventually expire instead of sticking forever.
          pipeline.set(`synapse:session:vk:${key.virtualKey}`, sid, { EX: 24 * 60 * 60 });
        }
        await pipeline.exec();
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

      if (redis) {
        // Pipeline the GET+DEL pairs in one round-trip.
        const pipeline = redis.multi();
        for (const key of userKeys) {
          const rk = `synapse:session:vk:${key.virtualKey}`;
          if (sessionId) {
            // Only delete if value matches the session id.
            const lua = `
              if redis.call("GET", KEYS[1]) == ARGV[1] then
                return redis.call("DEL", KEYS[1])
              else
                return 0
              end`;
            pipeline.eval(lua, { keys: [rk], arguments: [sessionId] });
          } else {
            pipeline.del(rk);
          }
        }
        const results = await pipeline.exec();
        const removedCount = Array.isArray(results)
          ? results.reduce((sum: number, r: any) => sum + (Number(r) || 0), 0)
          : 0;
        return NextResponse.json({ success: true, removedCount });
      }

      // No Redis available (e.g. dev mode without REDIS_URL).
      return NextResponse.json({ success: true, removedCount: 0, redisDisabled: true });
    }
  } catch (error: any) {
    return NextResponse.json({ error: error.message ?? "session record failed" }, { status: 500 });
  }
}
