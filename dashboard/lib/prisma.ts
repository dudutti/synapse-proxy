import { PrismaClient } from '@prisma/client';

const globalForPrisma = global as unknown as { prisma: PrismaClient };

// IMPORTANT: log: ['query'] must NEVER run in production.
// It can write several MB/s of SQL + parameter logs to stdout, which
// is enough to stall a Node.js process on a busy deployment and is a
// known cause of "dashboard looks frozen" symptoms.
const isProduction = process.env.NODE_ENV === 'production';

export const prisma =
  globalForPrisma.prisma ||
  new PrismaClient({
    log: isProduction ? ['error', 'warn'] : ['query', 'error', 'warn'],
  });

if (!isProduction) globalForPrisma.prisma = prisma;