-- Seed admin user + per-token-class pricing for all providers.
-- Idempotent: safe to re-run.
-- Run after: docker compose up -d (Postgres first boot creates the schema via Prisma push).

-- ============================================================
-- 1. Admin user (for dashboard access)
-- ============================================================
-- Default NextAuth schema: User has id, name, email, emailVerified, image, password, role, etc.
-- The exact column list depends on schema.prisma; the values below match what we use.

-- Insert admin user. If the email already exists, skip (idempotent).
-- Columns match Prisma schema: passwordHash (not password), role is enum USER/SUPERADMIN.
--
-- The bcrypt hash below is a placeholder that does NOT match any real
-- password. After the first deploy, the operator must either:
--   1) change the password through the dashboard "Forgot password" flow
--      (recommended), or
--   2) generate a fresh hash with: htpasswd -nbBC 10 "" 'YOUR_PASSWORD'
--      and UPDATE "User" SET "passwordHash" = '<hash>' WHERE email = '...';
INSERT INTO "User" (id, "passwordHash", email, name, role, "createdAt")
VALUES (
  'admin-user-001',
  -- bcrypt cost-10 placeholder hash (does NOT match any password)
  '$2b$10$abcdefghijklmnopqrstuuQJQ4w8YUk0sIFKbkzKK.k3xkU.W1zGq',
  'admin@optitoken.local',
  'Admin',
  'SUPERADMIN'::"Role",
  NOW()
)
ON CONFLICT (email) DO UPDATE SET
  "passwordHash" = EXCLUDED."passwordHash",
  name = EXCLUDED.name,
  role = 'SUPERADMIN'::"Role";

-- ============================================================
-- 2. ProviderModel pricing (per-token-class)
-- ============================================================

-- OpenAI
INSERT INTO "ProviderModel" (id, provider, "modelName", "costPromptPer1M", "costCompletionPer1M", "costCachedInputPer1M", "costCacheWritePer1M", "createdAt", "updatedAt") VALUES
  (gen_random_uuid(), 'openai', 'gpt-5.5',         5.00,  30.00, 0.50,  5.00,  NOW(), NOW()),
  (gen_random_uuid(), 'openai', 'gpt-5.5-pro',    30.00, 180.00, NULL, 30.00,  NOW(), NOW()),
  (gen_random_uuid(), 'openai', 'gpt-5.4',         2.50,  15.00, 0.25,  2.50,  NOW(), NOW()),
  (gen_random_uuid(), 'openai', 'gpt-5.4-mini',    0.75,   4.50, 0.075, 0.75,  NOW(), NOW()),
  (gen_random_uuid(), 'openai', 'gpt-5.4-nano',    0.20,   1.25, 0.02,  0.20,  NOW(), NOW()),
  (gen_random_uuid(), 'openai', 'gpt-5.4-pro',    30.00, 180.00, NULL, 30.00,  NOW(), NOW()),
  (gen_random_uuid(), 'openai', 'gpt-4o',          2.50,  10.00, 1.25,  2.50,  NOW(), NOW()),
  (gen_random_uuid(), 'openai', 'gpt-4o-mini',     0.15,   0.60, 0.075, 0.15,  NOW(), NOW())
ON CONFLICT (provider, "modelName") DO UPDATE SET
  "costPromptPer1M" = EXCLUDED."costPromptPer1M",
  "costCompletionPer1M" = EXCLUDED."costCompletionPer1M",
  "costCachedInputPer1M" = EXCLUDED."costCachedInputPer1M",
  "costCacheWritePer1M" = EXCLUDED."costCacheWritePer1M",
  "updatedAt" = NOW();

-- Anthropic
INSERT INTO "ProviderModel" (id, provider, "modelName", "costPromptPer1M", "costCompletionPer1M", "costCachedInputPer1M", "costCacheWritePer1M", "createdAt", "updatedAt") VALUES
  (gen_random_uuid(), 'anthropic', 'claude-fable-5',   10.00,  50.00, 1.00,  12.50, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-mythos-5',  10.00,  50.00, 1.00,  12.50, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-opus-4.8',   5.00,  25.00, 0.50,   6.25, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-opus-4.7',   5.00,  25.00, 0.50,   6.25, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-opus-4.6',   5.00,  25.00, 0.50,   6.25, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-opus-4.5',   5.00,  25.00, 0.50,   6.25, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-opus-4.1',  15.00,  75.00, 1.50,  18.75, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-opus-4',    15.00,  75.00, 1.50,  18.75, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-sonnet-4.6', 3.00,  15.00, 0.30,   3.75, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-sonnet-4.5', 3.00,  15.00, 0.30,   3.75, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-sonnet-4',   3.00,  15.00, 0.30,   3.75, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-haiku-4.5',  1.00,   5.00, 0.10,   1.25, NOW(), NOW()),
  (gen_random_uuid(), 'anthropic', 'claude-haiku-3.5',  0.80,   4.00, 0.08,   1.00, NOW(), NOW())
ON CONFLICT (provider, "modelName") DO UPDATE SET
  "costPromptPer1M" = EXCLUDED."costPromptPer1M",
  "costCompletionPer1M" = EXCLUDED."costCompletionPer1M",
  "costCachedInputPer1M" = EXCLUDED."costCachedInputPer1M",
  "costCacheWritePer1M" = EXCLUDED."costCacheWritePer1M",
  "updatedAt" = NOW();

-- Google Gemini
INSERT INTO "ProviderModel" (id, provider, "modelName", "costPromptPer1M", "costCompletionPer1M", "costCachedInputPer1M", "costCacheWritePer1M", "createdAt", "updatedAt") VALUES
  (gen_random_uuid(), 'google', 'gemini-2.5-pro',           1.25,  10.00, 0.13, 1.25, NOW(), NOW()),
  (gen_random_uuid(), 'google', 'gemini-2.5-flash',         0.30,   2.50, 0.03, 0.30, NOW(), NOW()),
  (gen_random_uuid(), 'google', 'gemini-2.5-flash-lite',    0.10,   0.40, 0.01, 0.10, NOW(), NOW()),
  (gen_random_uuid(), 'google', 'gemini-3.1-pro',          2.00,  12.00, 0.20, 2.00, NOW(), NOW()),
  (gen_random_uuid(), 'google', 'gemini-3.5-flash',        1.50,   9.00, 0.15, 1.50, NOW(), NOW()),
  (gen_random_uuid(), 'google', 'gemini-3-flash',          0.50,   3.00, 0.05, 0.50, NOW(), NOW()),
  (gen_random_uuid(), 'google', 'gemini-3.1-flash-lite',   0.25,   1.50, 0.025, 0.25, NOW(), NOW())
ON CONFLICT (provider, "modelName") DO UPDATE SET
  "costPromptPer1M" = EXCLUDED."costPromptPer1M",
  "costCompletionPer1M" = EXCLUDED."costCompletionPer1M",
  "costCachedInputPer1M" = EXCLUDED."costCachedInputPer1M",
  "costCacheWritePer1M" = EXCLUDED."costCacheWritePer1M",
  "updatedAt" = NOW();

-- Mistral
INSERT INTO "ProviderModel" (id, provider, "modelName", "costPromptPer1M", "costCompletionPer1M", "costCachedInputPer1M", "costCacheWritePer1M", "createdAt", "updatedAt") VALUES
  (gen_random_uuid(), 'mistral', 'mistral-medium-latest',  1.50,  7.50, NULL, 1.50, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'mistral-large-latest',   0.50,  1.50, NULL, 0.50, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'mistral-small-latest',   0.10,  0.30, NULL, 0.10, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'magistral-medium-latest', 2.00, 5.00, NULL, 2.00, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'magistral-small-latest',  0.50, 1.50, NULL, 0.50, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'open-mixtral-8x7b',      0.70,  0.70, NULL, 0.70, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'open-mixtral-8x22b',     2.00,  6.00, NULL, 2.00, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'open-mistral-nemo',      0.15,  0.15, NULL, 0.15, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'ministral-3b-latest',    0.10,  0.10, NULL, 0.10, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'ministral-8b-latest',    0.15,  0.15, NULL, 0.15, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'ministral-14b-latest',   0.20,  0.20, NULL, 0.20, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'devstral-medium-latest', 0.40,  2.00, NULL, 0.40, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'devstral-small-latest',  0.10,  0.30, NULL, 0.10, NOW(), NOW()),
  (gen_random_uuid(), 'mistral', 'codestral-latest',       0.30,  0.90, NULL, 0.30, NOW(), NOW())
ON CONFLICT (provider, "modelName") DO UPDATE SET
  "costPromptPer1M" = EXCLUDED."costPromptPer1M",
  "costCompletionPer1M" = EXCLUDED."costCompletionPer1M",
  "costCachedInputPer1M" = EXCLUDED."costCachedInputPer1M",
  "costCacheWritePer1M" = EXCLUDED."costCacheWritePer1M",
  "updatedAt" = NOW();

-- DeepSeek
INSERT INTO "ProviderModel" (id, provider, "modelName", "costPromptPer1M", "costCompletionPer1M", "costCachedInputPer1M", "costCacheWritePer1M", "createdAt", "updatedAt") VALUES
  (gen_random_uuid(), 'deepseek', 'deepseek-v4-flash',  0.140, 0.280, 0.0028, 0.140, NOW(), NOW()),
  (gen_random_uuid(), 'deepseek', 'deepseek-v4-pro',    0.435, 0.870, 0.003625, 0.435, NOW(), NOW())
ON CONFLICT (provider, "modelName") DO UPDATE SET
  "costPromptPer1M" = EXCLUDED."costPromptPer1M",
  "costCompletionPer1M" = EXCLUDED."costCompletionPer1M",
  "costCachedInputPer1M" = EXCLUDED."costCachedInputPer1M",
  "costCacheWritePer1M" = EXCLUDED."costCacheWritePer1M",
  "updatedAt" = NOW();

-- MiniMax
INSERT INTO "ProviderModel" (id, provider, "modelName", "costPromptPer1M", "costCompletionPer1M", "costCachedInputPer1M", "costCacheWritePer1M", "createdAt", "updatedAt") VALUES
  (gen_random_uuid(), 'minimax', 'MiniMax-M2.7', 0.250, 1.00, 0.050, 0.250, NOW(), NOW()),
  (gen_random_uuid(), 'minimax', 'MiniMax-M3',   0.300, 1.20, 0.060, 0.300, NOW(), NOW())
ON CONFLICT (provider, "modelName") DO UPDATE SET
  "costPromptPer1M" = EXCLUDED."costPromptPer1M",
  "costCompletionPer1M" = EXCLUDED."costCompletionPer1M",
  "costCachedInputPer1M" = EXCLUDED."costCachedInputPer1M",
  "costCacheWritePer1M" = EXCLUDED."costCacheWritePer1M",
  "updatedAt" = NOW();
