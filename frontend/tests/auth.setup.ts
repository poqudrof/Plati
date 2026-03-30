import { test as setup, expect } from '@playwright/test';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const authFile = path.join(__dirname, '.auth/state.json');

setup('authenticate as admin', async ({ page }) => {
  const password = process.env.ADMIN_PASSWORD;
  if (!password) throw new Error('ADMIN_PASSWORD env var is required');

  await page.goto('/login');

  // Wait for SvelteKit hydration so the onsubmit handler is attached
  await page.waitForLoadState('networkidle');
  await page.waitForSelector('input[type="password"]', { state: 'visible' });

  await page.fill('input[type="password"]', password);

  // Submit via keyboard to avoid click-timing hydration races
  await page.keyboard.press('Enter');

  // Wait for redirect to dashboard
  await expect(page).toHaveURL(/\/dashboard/, { timeout: 15_000 });

  // Persist cookies so all screenshot tests start authenticated
  await page.context().storageState({ path: authFile });
});
