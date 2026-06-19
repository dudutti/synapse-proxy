-- Agent detection columns for live telemetry grouping.
-- Filled by the proxy (proxy/internal/utils/agent_detector.go) from
-- request headers and body, with no client-side cooperation required.

ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "agentId" TEXT NOT NULL DEFAULT '';
ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "agentLabel" TEXT NOT NULL DEFAULT '';
ALTER TABLE "RequestLog" ADD COLUMN IF NOT EXISTS "sessionId" TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS "RequestLog_agentId_idx" ON "RequestLog" ("agentId");
CREATE INDEX IF NOT EXISTS "RequestLog_sessionId_idx" ON "RequestLog" ("sessionId");
CREATE INDEX IF NOT EXISTS "RequestLog_agentId_createdAt_idx" ON "RequestLog" ("agentId", "createdAt" DESC);
