import Stripe from 'stripe';

// Create a singleton instance of the Stripe client
export const stripe = new Stripe(process.env.STRIPE_SECRET_KEY || 'sk_test_mock', {
  apiVersion: '2024-04-10', // Or whichever the latest supported version is
  appInfo: {
    name: 'Synapse Proxy',
    version: '1.0.0',
  },
});
