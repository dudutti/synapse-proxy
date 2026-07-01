// sync_keys_with_retry.js - same as dashboard/sync_keys.js but
// waits for Redis to be ready (up to 30s) and retries on
// transient failures. Use as the dashboard entrypoint before
// starting the Next.js server so the proxy always has fresh
// API key material on boot.

const { PrismaClient } = require('@prisma/client');
const redis = require('redis');

const prisma = new PrismaClient();

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

const target = parseRedisTarget();
console.log(`[sync_keys] Target redis: ${target.host}:${target.port}${target.password ? ' (with auth)' : ''}`);

async function createAndConnectRedis(maxRetries = 15) {
  // Retry on transient errors. Each attempt waits a bit longer.
  // 15 attempts × ~3s = 45s max, well within the dashboard's
  // startup budget.
  let lastErr;
  for (let i = 0; i < maxRetries; i++) {
    const client = redis.createClient({
      socket: { host: target.host, port: target.port, reconnectStrategy: false },
      ...(target.password ? { password: target.password } : {}),
    });
    client.on('error', () => {});
    try {
      await client.connect();
      return client;
    } catch (e) {
      lastErr = e;
      console.log(`[sync_keys] Redis connect attempt ${i + 1}/${maxRetries} failed: ${e.message}`);
      try { await client.disconnect(); } catch {}
      // Back off: 1s, 2s, 4s ... up to 8s.
      const wait = Math.min(8000, 1000 * Math.pow(2, i));
      await new Promise(r => setTimeout(r, wait));
    }
  }
  throw lastErr || new Error('Redis connect failed after retries');
}

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