// sync_keys.js - Syncs all API keys from Postgres to Redis at boot.
// Replaces the obsolete CBC version. Uses the same AES-256-GCM format
// as /api/keys/route.ts and the Go proxy.
//
// Run via: docker exec optitoken-dashboard node /app/sync_keys.js
// Or add to dashboard Dockerfile to run on each container start.
//
// The dashboard Dockerfile already runs this on container start with:
//
//   CMD ["sh", "-c", "node sync_keys.js || echo '[WARN] sync_keys.js failed (continuing anyway)' ; npm start"]
//
// so the script is best-effort: if it fails (e.g. DB unreachable, bad
// ENCRYPTION_KEY), the dashboard still starts up and the user can
// recover from the /api/keys page.
//
// IMPORTANT: The proxy decrypts real_key itself (see auth.go:75).
// Therefore we must write the RAW CIPHERTEXT (realKeyEnc) to Redis,
// NOT the decrypted plaintext. Previous versions decrypted first,
// causing the proxy to fail with "decrypt real_key failed".

const { PrismaClient } = require('@prisma/client');
const redis = require('redis');

const prisma = new PrismaClient();

// redis@6 client configuration: parse REDIS_URL manually into host/port
function parseRedisTarget() {
  const raw = process.env.REDIS_URL || 'redis://redis:6379';
  const stripped = raw.replace(/^redis:\/\//, '').replace(/^rediss:\/\//, '');
  let userinfo = '';
  let rest = stripped;
  const at = stripped.indexOf('@');
  if (at >= 0) {
    userinfo = stripped.slice(0, at);
    rest = stripped.slice(at + 1);
  }
  let password = '';
  if (userinfo) {
    const colon = userinfo.indexOf(':');
    password = colon >= 0 ? userinfo.slice(colon + 1) : userinfo;
  }
  if (process.env.REDIS_PASSWORD) {
    password = process.env.REDIS_PASSWORD;
  }
  const [host, portStr] = rest.split(':');
  return {
    host: host || 'redis',
    port: parseInt(portStr, 10) || 6379,
    password,
  };
}

async function createAndConnectRedis(maxRetries = 5) {
  for (let i = 0; i < maxRetries; i++) {
    const client = redis.createClient({
      socket: { host: target.host, port: target.port },
      ...(target.password ? { password: target.password } : {}),
    });
    client.on('error', () => {}); // suppress unhandled error events
    try {
      await client.connect();
      return client;
    } catch (e) {
      console.log(`[sync_keys] Redis connect attempt ${i + 1}/${maxRetries} failed: ${e.message}`);
      try { await client.disconnect(); } catch {}
      if (i < maxRetries - 1) {
        await new Promise(r => setTimeout(r, 2000));
      } else {
        throw e;
      }
    }
  }
}

const target = parseRedisTarget();
console.log(`[sync_keys] Target redis: ${target.host}:${target.port}${target.password ? ' (with auth)' : ''}`);

async function sync() {
  const redisClient = await createAndConnectRedis();
  console.log('[sync_keys] Connected to Redis');
  const keys = await prisma.apiKey.findMany();
  console.log(`[sync_keys] Found ${keys.length} API keys in DB`);
  for (const k of keys) {
    // IMPORTANT: Write the raw ciphertext (realKeyEnc) to Redis.
    // The proxy decrypts it using DecryptRealKey() in auth.go.
    // Do NOT decrypt here — that was the old bug.
    const data = {
      real_key: k.realKeyEnc || '',
      provider: k.provider || '',
      benchmark_mode: k.benchmarkMode ? 'true' : 'false',
      semantic_tolerance: k.semanticTolerance ? k.semanticTolerance.toString() : '0.15',
      cache_ttl: k.cacheTtl ? k.cacheTtl.toString() : '86400',
      isolate_cache_by_user: k.isolateCacheByUser ? 'true' : 'false',
      zero_log: k.zeroLog ? 'true' : 'false',
      default_model: k.defaultModel || '',
      fallback_model: k.fallbackModel || '',
      fallback_provider: k.fallbackProvider || '',
      fallback_key: k.fallbackKeyEnc || '',
      enable_l1: k.enableL1 ? 'true' : 'false',
      enable_l2: k.enableL2 ? 'true' : 'false',
      enable_l3: k.enableL3 ? 'true' : 'false',
      kill_switch: k.killSwitch ? 'true' : 'false',
      fingerprint_loop_detect: k.fingerprintLoopDetect ? 'true' : 'false',
      session_token_limit: String(k.sessionTokenLimit || 0),
      allowed_tools: k.allowedTools || '',
      block_unknown_tools: k.blockUnknownTools ? 'true' : 'false',
      redact_pii: k.redactPII ? 'true' : 'false',
      tool_ttls: k.toolTtls || '{}',
      limit_exceeded: 'false',
    };
    await redisClient.hSet(`synapse:keys:${k.virtualKey}`, data);
    console.log(`[sync_keys] Synced ${k.virtualKey} (provider=${data.provider}, L1=${data.enable_l1} L2=${data.enable_l2} L3=${data.enable_l3})`);
  }
  await redisClient.disconnect();
  await prisma.$disconnect();
  console.log('[sync_keys] Sync complete.');
}

sync().catch((e) => {
  console.error('[sync_keys] Sync failed:', e);
  process.exit(1);
});