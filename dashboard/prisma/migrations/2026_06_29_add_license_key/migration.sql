-- AlterTable: add licenseKey column with UUID default for secure license activation
ALTER TABLE "User" ADD COLUMN "licenseKey" TEXT;

-- Backfill existing users with unique UUIDs
UPDATE "User" SET "licenseKey" = gen_random_uuid()::text WHERE "licenseKey" IS NULL;

-- Now make it NOT NULL and UNIQUE
ALTER TABLE "User" ALTER COLUMN "licenseKey" SET NOT NULL;
ALTER TABLE "User" ALTER COLUMN "licenseKey" SET DEFAULT gen_random_uuid()::text;

-- CreateIndex
CREATE UNIQUE INDEX "User_licenseKey_key" ON "User"("licenseKey");
