const { PrismaClient } = require('@prisma/client');

async function main() {
  const prisma = new PrismaClient();
  
  // Promote the first user to SUPERADMIN
  const users = await prisma.user.findMany({ orderBy: { createdAt: 'asc' } });
  
  if (users.length === 0) {
    console.log('No users found!');
    process.exit(1);
  }

  const firstUser = users[0];
  console.log(`Promoting user: ${firstUser.email} (${firstUser.id}) to SUPERADMIN`);

  await prisma.user.update({
    where: { id: firstUser.id },
    data: { role: 'SUPERADMIN' }
  });

  console.log('Done! User is now SUPERADMIN.');

  // Initialize SystemConfig if it doesn't exist
  await prisma.systemConfig.upsert({
    where: { id: 'global' },
    update: {},
    create: { id: 'global', registrationOpen: false }
  });
  console.log('SystemConfig initialized (registrationOpen: false).');

  // Seed default model pricing for MiniMax
  await prisma.providerModel.upsert({
    where: { provider_modelName: { provider: 'minimax', modelName: 'MiniMax-M2.7' } },
    update: {},
    create: { provider: 'minimax', modelName: 'MiniMax-M2.7', costPromptPer1M: 1.10, costCompletionPer1M: 8.80 }
  });
  console.log('Default MiniMax pricing seeded.');

  await prisma.$disconnect();
}

main().catch(console.error);
