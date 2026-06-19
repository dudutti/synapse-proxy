import { NextResponse } from "next/server";
import { getServerSession } from "next-auth";
import { prisma } from "@/lib/prisma";
import crypto from "crypto";
import { createClient } from "redis";

// Encrypt a real API key with AES-256-GCM.
//
// Output format: <12-byte IV hex><16-byte auth tag hex><ciphertext hex>
// = "iv:tag:ciphertext" all in lowercase hex, IV and tag are random per call.
//
// The same algorithm + key is implemented in the Go proxy
// (proxy/internal/services/crypto.go) so the proxy can decrypt the
// real_key value it reads from Redis before forwarding upstream.
//
// IMPORTANT: ENCRYPTION_KEY MUST be exactly 32 bytes (64 hex chars).
// In production, set ENCRYPTION_KEY in the dashboard's .env (and in
// the proxy's .env ”” they MUST match).
function getEncryptionKey(): Buffer {
  const raw = process.env.ENCRYPTION_KEY || "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
  const buf = Buffer.from(raw, "hex");
  // Accept any length: derive a 32-byte key via SHA-256.
  // This avoids breaking setups where ENCRYPTION_KEY is 16 or 32 raw
  // bytes; the AES-256-GCM spec only requires a 32-byte key.
  if (buf.length === 32) return buf;
  const hash = require("crypto").createHash("sha256").update(buf).digest();
  return hash;
}

function encrypt(text: string): string {
  const key = getEncryptionKey();
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv("aes-256-gcm", key, iv);
  const ct = Buffer.concat([cipher.update(text, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();
  return iv.toString("hex") + tag.toString("hex") + ct.toString("hex");
}

function decrypt(payload: string): string {
  const key = getEncryptionKey();
  if (payload.length < 24 + 32) {
    throw new Error("ciphertext too short");
  }
  const iv = Buffer.from(payload.slice(0, 24), "hex");
  const tag = Buffer.from(payload.slice(24, 56), "hex");
  const ct = Buffer.from(payload.slice(56), "hex");
  const decipher = crypto.createDecipheriv("aes-256-gcm", key, iv);
  decipher.setAuthTag(tag);
  const pt = Buffer.concat([decipher.update(ct), decipher.final()]);
  return pt.toString("utf8");
}

export async function GET(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const user = await prisma.user.findUnique({ where: { email: session.user.email } });
  if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

    const keys = await prisma.apiKey.findMany({
    where: { userId: user.id },
    select: { id: true, virtualKey: true, provider: true, monthlyBudget: true, currentUsage: true, createdAt: true, benchmarkMode: true, semanticTolerance: true, cacheTtl: true, defaultModel: true, isolateCacheByUser: true, zeroLog: true, enableL1: true, enableL2: true, enableL3: true, killSwitch: true, sessionTokenLimit: true, allowedTools: true, blockUnknownTools: true, redactPII: true, toolTtls: true }
  });

  return NextResponse.json(keys);
}

export async function POST(req: Request) {
  const session = await getServerSession();
  if (!session || !session.user || !session.user.email) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  try {
    const user = await prisma.user.findUnique({ where: { email: session.user.email } });
    if (!user) return NextResponse.json({ error: "Unauthorized" }, { status: 401 });

    const { provider, realKey, fallbackProvider, fallbackKey, fallbackModel, defaultModel, isolateCacheByUser, zeroLog, enableL1, enableL2, enableL3, killSwitch, sessionTokenLimit, allowedTools, blockUnknownTools, redactPII, toolTtls } = await req.json();

    if (!provider || !realKey) {
      return NextResponse.json({ error: "Provider and Real Key are required" }, { status: 400 });
    }

    // Generate sk-opti-...
    const virtualKey = "sk-opti-" + crypto.randomBytes(16).toString("hex");
    const encryptedRealKey = encrypt(realKey);
    const encryptedFallbackKey = fallbackKey ? encrypt(fallbackKey) : null;

    // Both DB and Redis get the encrypted form. The proxy reads from
    // Redis (hot path) and decrypts with the same shared key. The DB
    // copy is the durable backup (used by zero-log rotation, etc.).
    const encryptedRealKeyForRedis = encryptedRealKey;
    const encryptedFallbackKeyForRedis = encryptedFallbackKey;

    const newKey = await prisma.apiKey.create({
      data: {
        userId: user.id,
        virtualKey,
        provider,
        realKeyEnc: encryptedRealKey,
        fallbackProvider: fallbackProvider || null,
        fallbackKeyEnc: encryptedFallbackKey,
        fallbackModel: fallbackModel || null,
        defaultModel: defaultModel || null,
        monthlyBudget: 100.0,
        isolateCacheByUser: !!isolateCacheByUser,
        zeroLog: !!zeroLog,
        enableL1: enableL1 ?? true,
        enableL2: enableL2 ?? true,
        enableL3: enableL3 ?? true,
        killSwitch: !!killSwitch,
        sessionTokenLimit: sessionTokenLimit ? parseInt(sessionTokenLimit) : null,
        allowedTools: allowedTools || null,
        blockUnknownTools: !!blockUnknownTools,
        redactPII: !!redactPII,
        toolTtls: toolTtls || "{}",
      }
    });

    // Push to Redis so the Go Proxy can route it instantly.
    // CRITICAL: Redis seeding is REQUIRED. If it fails, we roll back
    // the DB row so the user doesn't get a key that the proxy can't
    // see (silent 401s from the proxy). Better to fail loud here
    // than to create a broken key.
    //
    // We bound the Redis connect/hSet with a 5s timeout to avoid the
    // request hanging forever if Redis is unreachable.
    let redisClient: ReturnType<typeof createClient> | null = null;
    try {
      redisClient = createClient({
        url: process.env.REDIS_URL || 'redis://localhost:6379',
        socket: { connectTimeout: 5000 },
        // disableOfflineQueue so commands fail fast when the server is
        // unreachable (instead of buffering forever).
        disableOfflineQueue: true,
      });
      redisClient.on('error', (e) => console.error("[POST /api/keys] redis error:", e?.message || e));

      const redisOp = (async () => {
        await redisClient!.connect();
        const redisData: Record<string, string> = {
          // Store encrypted. The proxy decrypts with the shared key.
          real_key: encryptedRealKeyForRedis,
          provider: provider,
          benchmark_mode: "false",
          semantic_tolerance: "0.15",
          isolate_cache_by_user: isolateCacheByUser ? "true" : "false",
          zero_log: zeroLog ? "true" : "false",
          enable_l1: (enableL1 ?? true) ? "true" : "false",
          enable_l2: (enableL2 ?? true) ? "true" : "false",
          enable_l3: (enableL3 ?? true) ? "true" : "false",
          kill_switch: killSwitch ? "true" : "false",
          session_token_limit: sessionTokenLimit ? sessionTokenLimit.toString() : "0",
          allowed_tools: allowedTools || "",
          block_unknown_tools: blockUnknownTools ? "true" : "false",
          redact_pii: redactPII ? "true" : "false",
          tier: user.tier || "FREE",
          tool_ttls: toolTtls || "{}",
        };
        // Check if limits exceeded based on tier
        let limitExceeded = false;
        const tier = user.tier || "FREE";
        const currentMonthTokens = user.currentMonthTokens || 0;
        if (tier === 'FREE' && currentMonthTokens >= 10000000) {
          limitExceeded = true;
        } else if (tier === 'PRO_1' && currentMonthTokens >= 20000000) {
          limitExceeded = true;
        } else if (tier === 'PRO_2' && currentMonthTokens >= 100000000) {
          limitExceeded = true;
        }
        redisData.limit_exceeded = limitExceeded ? "true" : "false";
        if (fallbackProvider && fallbackKey) {
          redisData.fallback_provider = fallbackProvider;
          redisData.fallback_key = encryptedFallbackKeyForRedis || "";
        }
        if (fallbackModel) redisData.fallback_model = fallbackModel;
        if (defaultModel) redisData.default_model = defaultModel;
        await redisClient!.hSet(`synapse:keys:${virtualKey}`, redisData);
      })();

      const timeout = new Promise((_, reject) =>
        setTimeout(() => reject(new Error("redis op timed out after 5s")), 5000)
      );
      await Promise.race([redisOp, timeout]);
    } catch (redisErr) {
      console.error("[POST /api/keys] Redis seeding failed, rolling back DB insert:", redisErr);
      try { await prisma.apiKey.delete({ where: { id: newKey.id } }); } catch (e) { console.error("rollback failed:", e); }
      return NextResponse.json(
        { error: "Failed to provision key in cache. Please retry; if the problem persists, the cache is down." },
        { status: 503 }
      );
    } finally {
      if (redisClient) {
        try { await redisClient.disconnect(); } catch {}
      }
    }

    return NextResponse.json({
      id: newKey.id,
      virtualKey: newKey.virtualKey,
      provider: newKey.provider,
      monthlyBudget: newKey.monthlyBudget,
    });

  } catch (error) {
    console.error(error);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}
