const { PrismaClient } = require('@prisma/client');

async function main() {
  const prisma = new PrismaClient();
  
  // Promote the first user to SUPERADMIN
  const users = await prisma.user.findMany({ orderBy: { createdAt: 'asc' } });
  
  if (users.length > 0) {
    const firstUser = users[0];
    console.log(`Promoting user: ${firstUser.email} (${firstUser.id}) to SUPERADMIN`);

    await prisma.user.update({
      where: { id: firstUser.id },
      data: { role: 'SUPERADMIN' }
    });
    console.log('Done! User is now SUPERADMIN.');
  }

  // Initialize SystemConfig if it doesn't exist
  await prisma.systemConfig.upsert({
    where: { id: 'global' },
    update: {},
    create: { id: 'global', registrationOpen: false }
  });
  console.log('SystemConfig initialized (registrationOpen: false).');

  // Seed default model pricing
  await prisma.providerModel.upsert({
    where: { provider_modelName_userId: { provider: 'minimax', modelName: 'MiniMax-M2.7', userId: 'global' } },
    update: {},
    create: { userId: 'global', provider: 'minimax', modelName: 'MiniMax-M2.7', costPromptPer1M: 1.10, costCompletionPer1M: 8.80 }
  });
  console.log('Default MiniMax pricing seeded.');

  // Seed Email Templates
  const emailTemplates = [
    {
      id: 'welcome_email',
      subject: 'Bienvenue sur Synapse Proxy 🚀',
      html: `
        <div style="font-family: sans-serif; max-width: 600px; margin: 0 auto; background: #0a0a0a; color: #f4f4f5; padding: 40px; border-radius: 12px; border: 1px solid #27272a;">
          <div style="text-align: center; margin-bottom: 30px;">
            <img src="https://synapse-proxy.com/logo.png" alt="Synapse Proxy" style="max-width: 180px; height: auto;" />
          </div>
          <h1 style="color: #34d399;">Bienvenue, {{NAME}} !</h1>
          <p>Votre compte Synapse Proxy a bien été créé. Vous pouvez dès à présent générer vos clés API virtuelles et profiter du cache multiniveaux et de la compression de contexte.</p>
          <div style="margin: 30px 0;">
            <a href="{{LOGIN_URL}}" style="background: #10b981; color: #000; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold;">Accéder au Dashboard</a>
          </div>
          <p style="color: #a1a1aa; font-size: 12px;">L'équipe Synapse Proxy</p>
        </div>
      `
    },
    {
      id: 'password_reset',
      subject: 'Réinitialisation de votre mot de passe - Synapse Proxy',
      html: `
        <div style="font-family: sans-serif; max-width: 600px; margin: 0 auto; background: #0a0a0a; color: #f4f4f5; padding: 40px; border-radius: 12px; border: 1px solid #27272a;">
          <div style="text-align: center; margin-bottom: 30px;">
            <img src="https://synapse-proxy.com/logo.png" alt="Synapse Proxy" style="max-width: 180px; height: auto;" />
          </div>
          <h1 style="color: #60a5fa;">Réinitialisation de mot de passe</h1>
          <p>Bonjour,</p>
          <p>Nous avons reçu une demande de réinitialisation de mot de passe pour votre compte. Cliquez sur le bouton ci-dessous pour créer un nouveau mot de passe :</p>
          <div style="margin: 30px 0;">
            <a href="{{RESET_URL}}" style="background: #3b82f6; color: #fff; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold;">Réinitialiser mon mot de passe</a>
          </div>
          <p style="color: #a1a1aa; font-size: 12px;">Si vous n'êtes pas à l'origine de cette demande, vous pouvez ignorer cet e-mail en toute sécurité.</p>
        </div>
      `
    },
    {
      id: 'weekly_report',
      subject: 'Votre rapport d\'économies hebdomadaire 📊',
      html: `
        <div style="font-family: sans-serif; max-width: 600px; margin: 0 auto; background: #0a0a0a; color: #f4f4f5; padding: 40px; border-radius: 12px; border: 1px solid #27272a;">
          <div style="text-align: center; margin-bottom: 30px;">
            <img src="https://synapse-proxy.com/logo.png" alt="Synapse Proxy" style="max-width: 180px; height: auto;" />
          </div>
          <h1 style="color: #a78bfa;">Rapport Hebdomadaire</h1>
          <p>Bonjour {{NAME}}, voici le résumé de votre semaine sur Synapse Proxy :</p>
          <div style="background: #18181b; padding: 20px; border-radius: 8px; margin: 20px 0;">
            <p><strong>Tokens Économisés :</strong> {{TOKENS_SAVED}}</p>
            <p><strong>Valeur Économisée :</strong> \${{DOLLARS_SAVED}}</p>
            <p><strong>Hit Rate Cache :</strong> {{CACHE_HIT_RATE}}%</p>
          </div>
          <div style="margin: 30px 0;">
            <a href="{{DASHBOARD_URL}}" style="background: #8b5cf6; color: #fff; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold;">Voir les détails</a>
          </div>
          <p style="color: #a1a1aa; font-size: 12px;">Vous recevez cet e-mail car vous avez activé les rapports hebdomadaires.</p>
        </div>
      `
    }
  ];

  for (const template of emailTemplates) {
    await prisma.emailTemplate.upsert({
      where: { id: template.id },
      update: {
        subject: template.subject,
        html: template.html
      },
      create: template
    });
  }
  console.log('Email templates seeded.');

  await prisma.$disconnect();
}

main().catch(console.error);
