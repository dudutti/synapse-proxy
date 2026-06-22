import { createClient, RedisClientType } from "redis";

// Singleton Redis client for read-heavy dashboard endpoints.
// Connection is lazy (first call), with a hard timeout so a flaky Redis
// never wedges the route handler. Each route handler uses short-lived
// `cacheJson` calls and tolerates cache misses by recomputing.
let client: RedisClientType | null = null;
let connecting: Promise<RedisClientType> | null = null;

export async function getRedis(): Promise<RedisClientType | null> {
  if (client) return client;
  if (!connecting) {
    const url = process.env.REDIS_URL || "redis://localhost:6379";
    const c = createClient({ url });
    c.on("error", (err) => {
      console.error("[redis] client error:", err?.message || err);
    });
    connecting = c.connect().then(() => {
      client = c as RedisClientType;
      return client;
    }).catch((err) => {
      console.error("[redis] connect failed:", err?.message || err);
      connecting = null;
      return null;
    });
  }
  return connecting;
}

/**
 * cacheJson wraps a slow async loader behind a Redis JSON cache.
 * Returns the cached value if present and fresh (within ttlSec),
 * otherwise calls loader(), stores its result, and returns it.
 *
 * Designed to be safe to call concurrently and to never throw on
 * Redis failure (falls back to calling loader() directly).
 */
export async function cacheJson<T>(
  key: string,
  ttlSec: number,
  loader: () => Promise<T>,
): Promise<T> {
  try {
    const c = await getRedis();
    if (c) {
      const hit = await Promise.race([
        c.get(key),
        new Promise<null>((resolve) => setTimeout(() => resolve(null), 200)),
      ]);
      if (hit) {
        try { return JSON.parse(hit) as T; } catch { /* corrupt cache: ignore */ }
      }
    }
  } catch (err) {
    console.error(`[cacheJson] read failed for ${key}:`, (err as Error)?.message);
  }

  const fresh = await loader();

  try {
    const c = await getRedis();
    if (c) {
      await Promise.race([
        c.set(key, JSON.stringify(fresh), { EX: ttlSec }),
        new Promise<void>((resolve) => setTimeout(() => resolve(), 200)),
      ]);
    }
  } catch (err) {
    console.error(`[cacheJson] write failed for ${key}:`, (err as Error)?.message);
  }

  return fresh;
}

/**
 * invalidate removes a cached value. Safe to call even if Redis is down.
 */
export async function invalidate(key: string): Promise<void> {
  try {
    const c = await getRedis();
    if (!c) return;
    await Promise.race([
      c.del(key),
      new Promise<number>((resolve) => setTimeout(() => resolve(0), 200)),
    ]);
  } catch (err) {
    console.error(`[invalidate] failed for ${key}:`, (err as Error)?.message);
  }
}