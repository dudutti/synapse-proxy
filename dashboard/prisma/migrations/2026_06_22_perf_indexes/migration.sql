-- Performance indexes for the dashboard hot paths.
--
-- Without these indexes the dashboard's high-frequency polling endpoints
-- (/api/admin/logs/stream @ 1Hz, /api/admin/status @ 5Hz, /api/analytics @ 5s)
-- each trigger a sequential scan of the entire RequestLog table. At a few
-- thousand rows this is invisible; at a few hundred thousand the dashboard
-- becomes unresponsive and the SUPERADMIN page looks "frozen/crashed".
--
-- Indexes added:
--   * RequestLog(apiKeyId, createdAt DESC)  — covers analytics home,
--     analytics/stream, analytics/session, analytics/export, expensive.
--   * RequestLog(createdAt DESC)            — covers admin/logs/stream SSE
--     poll (createdAt > $lastSeen), admin/status counts (createdAt >= now-1h,
--     now-24h), retention worker (createdAt < cutoff).
--   * RequestLog(cacheLevel, createdAt DESC) — covers admin/telemetry
--     groupBy(cacheLevel) and any "cacheLevel = X AND since" filter.
--   * RequestLog(sessionId, createdAt DESC)  — strengthens the existing
--     @@index([sessionId]) for time-bounded session drilldown.
--   * BenchmarkLog(apiKeyId, createdAt DESC) — /api/benchmark list.

CREATE INDEX IF NOT EXISTS "RequestLog_apiKeyId_createdAt_idx"
  ON "RequestLog" ("apiKeyId", "createdAt" DESC);

CREATE INDEX IF NOT EXISTS "RequestLog_createdAt_idx"
  ON "RequestLog" ("createdAt" DESC);

CREATE INDEX IF NOT EXISTS "RequestLog_cacheLevel_createdAt_idx"
  ON "RequestLog" ("cacheLevel", "createdAt" DESC);

CREATE INDEX IF NOT EXISTS "RequestLog_sessionId_createdAt_idx"
  ON "RequestLog" ("sessionId", "createdAt" DESC);

CREATE INDEX IF NOT EXISTS "BenchmarkLog_apiKeyId_createdAt_idx"
  ON "BenchmarkLog" ("apiKeyId", "createdAt" DESC);