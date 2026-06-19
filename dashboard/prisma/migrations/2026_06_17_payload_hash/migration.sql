-- AlterTable
ALTER TABLE "RequestLog" ADD COLUMN "payloadHash" TEXT;

-- Index for the groupBy in /api/admin/expensive-prompts
CREATE INDEX IF NOT EXISTS "RequestLog_payloadHash_idx" ON "RequestLog"("payloadHash");
