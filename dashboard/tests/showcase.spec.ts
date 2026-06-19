import { test, expect } from '@playwright/test';
import { PrismaClient } from '@prisma/client';
import bcrypt from 'bcryptjs';

const prisma = new PrismaClient();

test.describe.configure({ mode: 'serial' });

test.describe('Synapse Proxy Showcase', () => {
  let testUserEmail = 'test@example.com';

  test.beforeAll(async () => {
    // Force password to 'password123' so Playwright can log in
    const hashedPassword = await bcrypt.hash('password123', 10);
    await prisma.user.updateMany({
      where: { email: testUserEmail },
      data: { passwordHash: hashedPassword, emailVerified: new Date() }
    });
  });

  // Global settings for these showcase tests
  test.use({
    actionTimeout: 15000,
  });

  // =======================================================
  // VIDEO 1: LOGIN, DASHBOARD & SETTINGS
  // =======================================================
  test('Part 1 - Interface, Login & Settings', async ({ page }) => {
    test.setTimeout(60000); // 1 minute max for this clip
    
    await page.goto('/login');
    await page.fill('input[type="email"]', testUserEmail);
    await page.fill('input[type="password"]', 'password123');
    await page.waitForTimeout(1000);
    await page.click('button[type="submit"]');

    // Dashboard
    await expect(page).toHaveURL('/');
    await expect(page.locator('h2:has-text("Total Value Saved")')).toBeVisible();
    await page.waitForTimeout(2000); // Admire the Dashboard animations

    // Settings & Billing
    await page.goto('/settings');
    await expect(page.locator('h1:has-text("API Keys")')).toBeVisible();
    await page.waitForTimeout(1000);

    // Scroll to see the new Stripe plans we added
    await page.mouse.wheel(0, 600);
    await page.waitForTimeout(2000);
  });

  // =======================================================
  // VIDEO 2: BENCHMARK
  // =======================================================
  test('Part 2 - Benchmark & Reliability', async ({ page }) => {
    test.setTimeout(60000);
    
    // Login quickly without pausing
    await page.goto('/login');
    await page.fill('input[type="email"]', testUserEmail);
    await page.fill('input[type="password"]', 'password123');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL('/');

    // Go to Benchmark
    await page.goto('/benchmark');
    await page.waitForTimeout(2000); // Show the initial UI
    
    // Attempt to run the benchmark if the button is there
    const runBtn = page.locator('button:has-text("Run Full Benchmark")');
    if (await runBtn.isVisible()) {
      await runBtn.click();
      await page.waitForTimeout(5000); // Wait for the benchmark to run and show the evaluation results
    }
  });

  // =======================================================
  // VIDEO 3: PLAYGROUND & TELEMETRY
  // =======================================================
  test('Part 3 - Playground Live Cache & Telemetry', async ({ page }) => {
    test.setTimeout(90000); // 1.5 minutes max
    
    // Login
    await page.goto('/login');
    await page.fill('input[type="email"]', testUserEmail);
    await page.fill('input[type="password"]', 'password123');
    await page.click('button[type="submit"]');
    await expect(page).toHaveURL('/');

    // Go to Playground
    await page.goto('/playground');
    await expect(page.locator('h1:has-text("Playground")')).toBeVisible();
    await page.waitForTimeout(1500);

    const uniquePrompt = `Write a short poem about space. ID: ${Date.now()}`;
    const uniqueSemanticPrompt = `Can you give me a small poem regarding the cosmos? ID: ${Date.now()}`;

    // 1st Request: API Call (Miss)
    await page.fill('textarea[placeholder="Send a prompt to test the cache..."]', uniquePrompt);
    await page.waitForTimeout(500);
    await page.click('button[type="submit"]');
    await expect(page.locator('text=API Call')).toBeVisible({ timeout: 20000 });
    await page.waitForTimeout(2000);

    // 2nd Request: Exact Match (L1 Hit)
    await page.fill('textarea[placeholder="Send a prompt to test the cache..."]', uniquePrompt);
    await page.click('button[type="submit"]');
    await expect(page.locator('text=Cache Hit').first()).toBeVisible({ timeout: 10000 });
    await page.waitForTimeout(2000);

    // 3rd Request: Semantic Match (L2 Hit)
    await page.fill('textarea[placeholder="Send a prompt to test the cache..."]', uniqueSemanticPrompt);
    await page.click('button[type="submit"]');
    await expect(page.locator('text=Cache Hit').nth(1)).toBeVisible({ timeout: 10000 });
    await page.waitForTimeout(2500);

    // Return to Dashboard to see live logs and Token volume update
    await page.goto('/');
    await expect(page.locator('h2:has-text("Live Telemetry")')).toBeVisible();
    
    // Scroll slightly to frame the telemetry table properly
    await page.mouse.wheel(0, 400);
    await page.waitForTimeout(3000); // Admire the logs and metrics
  });
});
