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

const { PrismaClient } = require('@prisma/client');
const redis = require('redis');
const crypto = require('crypto');

const prisma = new PrismaClient();

// redis@6 client configuration: parse REDIS_URL manually into host/port
// to avoid version-specific URL parsing bugs (we hit one where the v6
// client silently fell back to localhost). Format:
//   redis://host:port  ->  socket: { host, port }
//   host:port          ->  socket: { host, port }
//   host               ->  socket: { host, port: 6379 }
function parseRedisTarget() {
  // Format: redis://[:password@]host:port[/db]
  // SECURITY: this script used to silently drop the password
  // when REDIS_URL was redis://:password@host:port. The fix
  // is below: we extract the password from the userinfo part
  // of the URL. If REDIS_PASSWORD is set explicitly, that
  // wins (it's the most common way to inject creds in Docker).
  const raw = process.env.REDIS_URL || 'redis://redis:6379';
  // strip scheme
  const stripped = raw.replace(/^redis:\/\//, '').replace(/^rediss:\/\//, '');
  // Look for userinfo (everything before the first @)
  let userinfo = '';
  let rest = stripped;
  const at = stripped.indexOf('@');
  if (at >= 0) {
    userinfo = stripped.slice(0, at);
    rest = stripped.slice(at + 1);
  }
  // userinfo can be `:password` or `user:password`
  let password = '';
  if (userinfo) {
    const colon = userinfo.indexOf(':');
    password = colon >= 0 ? userinfo.slice(colon + 1) : userinfo;
  }
  // env override
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
const redisClient = redis.createClient({
  socket: { host: target.host, port: target.port },
  ...(target.password ? { password: target.password } : {}),
});
console.log(`[sync_keys] Connecting to redis at ${target.host}:${target.port}${target.password ? ' (with auth)' : ''}`);

function getEncryptionKey() {
  const raw = process.env.ENCRYPTION_KEY || '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const buf = Buffer.from(raw, 'hex');
  if (buf.length === 32) return buf;
  return crypto.createHash('sha256').update(buf).digest();
}

function decrypt(payload) {
  if (!payload || payload.length < 24 + 32) throw new Error('ciphertext too short');
  const key = getEncryptionKey();
  const iv = Buffer.from(payload.slice(0, 24), 'hex');
  const tag = Buffer.from(payload.slice(24, 56), 'hex');
  const ct = Buffer.from(payload.slice(56), 'hex');
  const decipher = crypto.createDecipheriv('aes-256-gcm', key, iv);
  decipher.setAuthTag(tag);
  const pt = Buffer.concat([decipher.update(ct), decipher.final()]);
  return pt.toString('utf8');
}

async function sync() {
  await redisClient.connect();
  const keys = await prisma.apiKey.findMany();
  console.log(`[sync_keys] Found ${keys.length} API keys in DB`);
  for (const k of keys) {
    let realKey = '';
    try {
      realKey = decrypt(k.realKeyEnc);
    } catch (e) {
      console.error(`[sync_keys] Failed to decrypt real_key for ${k.virtualKey}: ${e.message}`);
      continue;
    }
    const data = {
      real_key: realKey,
      provider: k.provider || '',
      benchmark_mode: k.benchmarkMode ? 'true' : 'false',
      semantic_tolerance: k.semanticTolerance ? k.semanticTolerance.toString() : '0.15',
      cache_ttl: k.cacheTtl ? k.cacheTtl.toString() : '86400',
      isolate_cache_by_user: k.isolateCacheByUser ? 'true' : 'false',
      zero_log: k.zeroLog ? 'true' : 'false',
      default_model: k.defaultModel || '',
      fallback_model: k.fallbackModel || '',
      fallback_provider: k.fallbackProvider || '',
    };
    if (k.fallbackKeyEnc) {
      try {
        data.fallback_key = decrypt(k.fallbackKeyEnc);
      } catch (e) {
        console.error(`[sync_keys] Failed to decrypt fallback_key for ${k.virtualKey}: ${e.message}`);
      }
    }
    await redisClient.hSet(`synapse:keys:${k.virtualKey}`, data);
    console.log(`[sync_keys] Synced ${k.virtualKey} (provider=${data.provider}, benchmark=${data.benchmark_mode})`);
  }
  await redisClient.disconnect();
  await prisma.$disconnect();
  console.log('[sync_keys] Sync complete.');
}

sync().catch((e) => {
  console.error('[sync_keys] Sync failed:', e);
  process.exit(1);
});