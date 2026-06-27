// seed-admin-direct.js — creates a SUPERADMIN user directly in
// the DB so you can log in to the dashboard without going through
// the registration flow.
const { PrismaClient } = require('@prisma/client');
const bcrypt = require('bcryptjs');

const EMAIL = process.env.SEED_EMAIL || 'admin@synapse.local';
const PASSWORD = process.env.SEED_PASSWORD || 'Admin!Synapse2026!';
const NAME = process.env.SEED_NAME || 'Admin Synapse';

async function main() {
  const prisma = new PrismaClient();
  const passwordHash = await bcrypt.hash(PASSWORD, 12);

  const user = await prisma.user.upsert({
    where: { email: EMAIL },
    update: {
      passwordHash,
      name: NAME,
      role: 'SUPERADMIN',
      emailVerified: new Date(),
      tier: 'TIER2',
    },
    create: {
      email: EMAIL,
      passwordHash,
      name: NAME,
      role: 'SUPERADMIN',
      emailVerified: new Date(),
      tier: 'TIER2',
    },
  });

  await prisma.systemConfig.upsert({
    where: { id: 'global' },
    update: {},
    create: { id: 'global', registrationOpen: false },
  });

  // Seed default pricing.
  await prisma.providerModel.upsert({
    where: { provider_modelName_userId: { provider: 'openai', modelName: 'gpt-4o-mini', userId: 'global' } },
    update: {},
    create: { userId: 'global', provider: 'openai', modelName: 'gpt-4o-mini', costPromptPer1M: 0.15, costCompletionPer1M: 0.60 },
  });
  await prisma.providerModel.upsert({
    where: { provider_modelName_userId: { provider: 'openai', modelName: 'gpt-4o', userId: 'global' } },
    update: {},
    create: { userId: 'global', provider: 'openai', modelName: 'gpt-4o', costPromptPer1M: 5.0, costCompletionPer1M: 15.0 },
  });

  console.log('\n=== Admin user ready ===');
  console.log('  email:    ' + EMAIL);
  console.log('  password: ' + PASSWORD);
  console.log('  role:     SUPERADMIN');
  console.log('  tier:     TIER2');
  console.log('  id:       ' + user.id);
  console.log('\nDashboard: http://localhost:3000');
  console.log('Proxy:     http://localhost:8080\n');
}

main().catch(e => { console.error(e); process.exit(1); });