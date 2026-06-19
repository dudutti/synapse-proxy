-- Zero-Log Mode column on ApiKey. When true, the proxy never persists
-- the prompt or response content anywhere (cache, DB, telemetry,
-- Model Radar samples). Token counts and metadata are still kept.
ALTER TABLE "ApiKey" ADD COLUMN IF NOT EXISTS "zeroLog" BOOLEAN NOT NULL DEFAULT false;
