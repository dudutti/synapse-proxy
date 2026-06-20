import { NextResponse } from "next/server";
import { headers } from "next/headers";
import Stripe from "stripe";
import { prisma } from "@/lib/prisma";
import { createClient } from "redis";

const stripe = new Stripe(process.env.STRIPE_SECRET_KEY || 'dummy_key_for_build', {
  apiVersion: "2024-04-10" as any,
});

const webhookSecret = process.env.STRIPE_WEBHOOK_SECRET || 'dummy_secret';

export async function POST(req: Request) {
  const body = await req.text();
  const signature = headers().get("Stripe-Signature") as string;

  let event: Stripe.Event;

  try {
    event = stripe.webhooks.constructEvent(body, signature, webhookSecret);
  } catch (err: any) {
    console.error(`Webhook Error: ${err.message}`);
    return NextResponse.json({ error: `Webhook Error: ${err.message}` }, { status: 400 });
  }

  try {
    switch (event.type) {
      case "checkout.session.completed": {
        const session = event.data.object as Stripe.Checkout.Session;
        const userId = session.client_reference_id || session.metadata?.userId;
        
        if (userId) {
          const subscription = await stripe.subscriptions.retrieve(session.subscription as string);
          const priceId = subscription.items.data[0].price.id;

          const plan = await prisma.stripePlan.findUnique({
            where: { priceId }
          });

          let newTier = "FREE";
          if (plan) {
            newTier = plan.tier;
          } else if (priceId === "price_mock_pro") {
            newTier = "PRO_1";
          } else if (priceId === "price_mock_enterprise") {
            newTier = "PRO_2";
          }
          
          await prisma.user.update({
            where: { id: userId },
            data: {
              stripeCustomerId: session.customer as string,
              stripeSubscriptionId: subscription.id,
              stripePriceId: priceId,
              stripeCurrentPeriodEnd: new Date(subscription.current_period_end * 1000),
              tier: newTier,
            },
          });

          // Sync keys to Redis
          const apiKeys = await prisma.apiKey.findMany({
            where: { userId }
          });

          if (apiKeys.length > 0) {
            const redisClient = createClient({ url: process.env.REDIS_URL || 'redis://localhost:6379' });
            await redisClient.connect();
            for (const key of apiKeys) {
              await redisClient.hSet(`synapse:keys:${key.virtualKey}`, {
                tier: newTier,
                limit_exceeded: "false"
              });
            }
            await redisClient.disconnect();
          }
        }
        break;
      }
      
      case "customer.subscription.updated":
      case "customer.subscription.deleted": {
        const subscription = event.data.object as Stripe.Subscription;
        const priceId = subscription.items.data[0].price.id;
        
        const plan = await prisma.stripePlan.findUnique({
          where: { priceId }
        });

        let newTier = "FREE";
        if (subscription.status === "active" || subscription.status === "trialing") {
          if (plan) {
            newTier = plan.tier;
          } else if (priceId === "price_mock_pro") {
            newTier = "PRO_1";
          } else if (priceId === "price_mock_enterprise") {
            newTier = "PRO_2";
          }
        }

        const users = await prisma.user.findMany({
          where: { stripeSubscriptionId: subscription.id },
          include: { apiKeys: true }
        });

        for (const user of users) {
          await prisma.user.update({
            where: { id: user.id },
            data: {
              stripePriceId: priceId,
              stripeCurrentPeriodEnd: new Date(subscription.current_period_end * 1000),
              tier: newTier,
            },
          });

          // Sync keys to Redis
          if (user.apiKeys.length > 0) {
            const redisClient = createClient({ url: process.env.REDIS_URL || 'redis://localhost:6379' });
            await redisClient.connect();
            for (const key of user.apiKeys) {
              await redisClient.hSet(`synapse:keys:${key.virtualKey}`, {
                tier: newTier,
                limit_exceeded: "false"
              });
            }
            await redisClient.disconnect();
          }
        }
        break;
      }
    }

    return NextResponse.json({ received: true });
  } catch (err: any) {
    console.error(`Error processing webhook: ${err.message}`);
    return NextResponse.json({ error: "Internal Server Error" }, { status: 500 });
  }
}
