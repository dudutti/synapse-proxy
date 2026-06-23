// bootstrap-key.js - One-shot admin script to seed a virtual key
// directly into Postgres + Redis, bypassing the dashboard form.
//
// Use when:
//   * The dashboard /api/keys POST is failing (e.g. Redis seeding 500
//     causes a rollback that deletes the DB row before you can see
//     it).
//   * The dashboard input field appears to truncate the key
//     (we hit this with a 125-char Minimax key that got cut to 36).
//   * You need to seed a real key fast without going through the UI.
//
// Usage (run inside the dashboard container, with env_file vars set):
//   node bootstrap-key.js
//
// Reads the real key from REAL_KEY (hardcoded below for convenience —
// change it to read from a file/secret store in production). Encrypts
// with the shared ENCRYPTION_KEY, writes to Postgres, pushes the same
// ciphertext to Redis under synapse:keys:<virtualKey>.
//
// IMPORTANT: this script DELETES any existing ApiKey owned by
// julien@webetech.fr before inserting. So running it twice is safe.
// The virtual key is freshly generated each run.
//
// NOT for production: in production, the operator should generate
// keys through the /settings page so the audit log + tool_ttls +
// multi-tenant logic are all applied. This is an escape hatch for
// bringing the dashboard back online after a broken deploy.

const { PrismaClient } = require('@prisma/client');
const redis = require('redis');
const crypto = require('crypto');

const prisma = new PrismaClient();

// CHANGE THIS to the real provider API key. Trimmed keys won't
// authenticate with the provider, so paste the FULL key here.
const REAL_KEY = process.env.REAL_KEY_OVERRIDE || "";

function getEncryptionKey() {
  const raw = process.env.ENCRYPTION_KEY || "";
  const buf = Buffer.from(raw, "hex");
  if (buf.length === 32) return buf;
  return crypto.createHash("sha256").update(buf).digest();
}

function encrypt(text) {
  const key = getEncryptionKey();
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv("aes-256-gcm", key, iv);
  const ct = Buffer.concat([cipher.update(text, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();
  return iv.toString("hex") + tag.toString("hex") + ct.toString("hex");
}

async function main() {
  if (!REAL_KEY) {
    console.error("Set REAL_KEY_OVERRIDE in the env or edit this file.");
    process.exit(1);
  }
  console.log("[bootstrap] Real key length:", REAL_KEY.length);

  const user = await prisma.user.findUnique({ where: { email: "julien@webetech.fr" } });
  if (!user) { console.error("user not found"); process.exit(1); }

  const virtualKey = "sk-opti-" + crypto.randomBytes(16).toString("hex");
  const encryptedRealKey = encrypt(REAL_KEY);

  // Wipe any existing key for this user so we don't leave stale ones.
  await prisma.apiKey.deleteMany({ where: { userId: user.id } });

  const newKey = await prisma.apiKey.create({
    data: {
      userId: user.id,
      virtualKey,
      provider: "minimax",
      realKeyEnc: encryptedRealKey,
      monthlyBudget: 100.0,
      defaultModel: "MiniMax-M2.7",
      isolateCacheByUser: false,
      zeroLog: false,
      enableL1: true,
      enableL2: true,
      enableL3: true,
      killSwitch: false,
      fingerprintLoopDetect: false,
      allowedTools: null,
      blockUnknownTools: false,
      redactPII: false,
      toolTtls: "{}",
    },
  });
  console.log("[bootstrap] Created key:", newKey.virtualKey);

  // Push the same ciphertext to Redis (the proxy reads from Redis
  // on the hot path; it decrypts with the shared key).
  const r = redis.createClient({ socket: { host: "redis", port: 6379 } });
  await r.connect();
  const rkey = "synapse:keys:" + virtualKey;
  await r.hSet(rkey, {
    real_key: encryptedRealKey,
    provider: "minimax",
    benchmark_mode: "false",
    semantic_tolerance: "0.15",
    cache_ttl: "86400",
    isolate_cache_by_user: "false",
    zero_log: "false",
    default_model: "MiniMax-M2.7",
    fallback_model: "",
    fallback_provider: "",
    enable_l1: "true",
    enable_l2: "true",
    enable_l3: "true",
    kill_switch: "false",
    fingerprint_loop_detect: "false",
    session_token_limit: "0",
    allowed_tools: "",
    block_unknown_tools: "false",
    redact_pii: "false",
    tier: "FREE",
    tool_ttls: "{}",
    limit_exceeded: "false",
  });
  console.log("[bootstrap] Pushed to Redis key:", rkey);

  await r.disconnect();
  await prisma.$disconnect();
  console.log("[bootstrap] Done.");
}

main().catch((e) => { console.error("FATAL", e); process.exit(1); });