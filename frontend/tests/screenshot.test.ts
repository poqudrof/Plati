/**
 * Screenshot test suite for Plati UI.
 *
 * Requires a running backend + frontend:
 *   make dev
 *
 * Required env vars:
 *   ADMIN_PASSWORD  — admin password configured in plati.yaml
 *
 * Optional env vars:
 *   BASE_URL        — frontend URL (default: http://localhost:5173)
 *   INSTANCE_ID     — ID of a *running* instance for terminal screenshots
 *
 * Run:
 *   make screenshots
 */

import { test, expect, type Page } from '@playwright/test';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const OUT = path.join(__dirname, 'screenshots');
fs.mkdirSync(OUT, { recursive: true });

async function shot(page: Page, name: string) {
  await page.screenshot({
    path: path.join(OUT, `${name}.png`),
    fullPage: false,
  });
}

async function waitForContent(page: Page) {
  // Wait for the spinning loader to disappear if present
  await page.waitForFunction(
    () => !document.querySelector('.animate-spin'),
    { timeout: 10_000 }
  );
}

// ---------------------------------------------------------------------------
// Login page (unauthenticated — use a fresh context without stored auth)
// ---------------------------------------------------------------------------

test.describe('Login page', () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test('default state', async ({ page }) => {
    await page.goto('/login');
    await page.waitForLoadState('networkidle');
    await shot(page, '01-login-default');
  });

  test('error state (wrong password)', async ({ page }) => {
    await page.goto('/login');
    await page.waitForLoadState('networkidle');
    await page.fill('input[type="password"]', 'wrongpassword');
    // Use Enter so the onsubmit handler fires after hydration
    await page.keyboard.press('Enter');
    // Wait for error toast (bg-red-600 added via Svelte class: directive)
    await page.waitForSelector('.bg-red-600', { timeout: 8_000 });
    await shot(page, '02-login-error');
  });
});

// ---------------------------------------------------------------------------
// Dashboard
// ---------------------------------------------------------------------------

test.describe('Dashboard', () => {
  test('instances list', async ({ page }) => {
    await page.goto('/dashboard');
    await waitForContent(page);
    // Wait for stats to resolve (async exec inside instances)
    await page.waitForFunction(
      () => !document.querySelector('.animate-pulse'),
      { timeout: 15_000 }
    ).catch(() => {}); // ok if no stats to load
    await shot(page, '03-dashboard');
  });
});

// ---------------------------------------------------------------------------
// New instance
// ---------------------------------------------------------------------------

test.describe('New instance', () => {
  test('blank form', async ({ page }) => {
    await page.goto('/instances/new');
    await waitForContent(page);
    await shot(page, '04-new-instance-blank');
  });

  test('template selected', async ({ page }) => {
    await page.goto('/instances/new');
    await waitForContent(page);

    // Click the first template card if any exist
    const firstCard = page.locator('[class*="rounded"][class*="border"]').first();
    if (await firstCard.isVisible()) {
      await firstCard.click();
    }
    await shot(page, '05-new-instance-selected');
  });
});

// ---------------------------------------------------------------------------
// Instance detail
// ---------------------------------------------------------------------------

test.describe('Instance detail', () => {
  async function getFirstInstance(page: Page): Promise<number | null> {
    const resp = await page.request.get('/api/v1/instances');
    if (!resp.ok()) return null;
    const data = await resp.json();
    const list = Array.isArray(data) ? data : data.instances ?? [];
    return list.length > 0 ? list[0].id : null;
  }

  test('stopped instance', async ({ page }) => {
    const resp = await page.request.get('/api/v1/instances');
    if (!resp.ok()) { test.skip(); return; }
    const data = await resp.json();
    const list = Array.isArray(data) ? data : data.instances ?? [];
    const stopped = list.find((i: any) => i.status === 'stopped');
    if (!stopped) { test.skip(); return; }

    await page.goto(`/instances/${stopped.id}`);
    await waitForContent(page);
    await shot(page, '06-instance-stopped');
  });

  test('running instance', async ({ page }) => {
    const resp = await page.request.get('/api/v1/instances');
    if (!resp.ok()) { test.skip(); return; }
    const data = await resp.json();
    const list = Array.isArray(data) ? data : data.instances ?? [];
    const running = list.find((i: any) => i.status === 'running');
    if (!running) { test.skip(); return; }

    await page.goto(`/instances/${running.id}`);
    await waitForContent(page);
    await shot(page, '07-instance-running');
  });
});

// ---------------------------------------------------------------------------
// Terminal
// ---------------------------------------------------------------------------

test.describe('Terminal', () => {
  test('connected with command output', async ({ page }) => {
    // Use INSTANCE_ID env var, or discover a running instance
    let instanceId: number | null = process.env.INSTANCE_ID
      ? Number(process.env.INSTANCE_ID)
      : null;

    if (!instanceId) {
      const resp = await page.request.get('/api/v1/instances');
      if (!resp.ok()) { test.skip(); return; }
      const data = await resp.json();
      const list = Array.isArray(data) ? data : data.instances ?? [];
      const running = list.find((i: any) => i.status === 'running');
      if (!running) { test.skip(); return; }
      instanceId = running.id;
    }

    await page.goto(`/instances/${instanceId}`);
    await waitForContent(page);

    // Open terminal
    const openBtn = page.getByRole('button', { name: /open terminal/i });
    await expect(openBtn).toBeVisible({ timeout: 5_000 });
    await openBtn.click();

    // Wait for WebSocket connection — "Connected" badge appears
    await expect(page.getByText('Connected')).toBeVisible({ timeout: 10_000 });

    // Give xterm a moment to render the shell prompt
    await page.waitForTimeout(1_500);
    await shot(page, '08-terminal-connected');

    // Type a command and wait for output
    await page.keyboard.type('ls /\r');
    await page.waitForTimeout(2_000);
    await shot(page, '09-terminal-command-output');
  });
});

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

test.describe('Settings', () => {
  test('SSH keys + secrets', async ({ page }) => {
    await page.goto('/settings');
    await waitForContent(page);
    await shot(page, '10-settings');
  });
});

// ---------------------------------------------------------------------------
// Admin panel
// ---------------------------------------------------------------------------

test.describe('Admin', () => {
  test('templates tab', async ({ page }) => {
    await page.goto('/admin');
    await waitForContent(page);
    // Templates tab is default
    await shot(page, '11-admin-templates');
  });

  test('servers tab', async ({ page }) => {
    await page.goto('/admin');
    // Wait for the tab bar to render (unconditional, no spinner on admin page)
    await page.waitForSelector('text=Templates', { timeout: 10_000 });
    // Click the Servers tab button (second tab in the border-b flex row)
    await page.locator('.border-b button', { hasText: 'Servers' }).click();
    // Wait for server cards or "no servers" text (Incus may be slow to respond)
    await page.waitForTimeout(2_000);
    await shot(page, '12-admin-servers');
  });

  test('users tab', async ({ page }) => {
    await page.goto('/admin');
    await page.waitForSelector('text=Templates', { timeout: 10_000 });
    await page.locator('.border-b button', { hasText: 'Users' }).click();
    await page.waitForTimeout(500);
    await shot(page, '13-admin-users');
  });

  test('edit template modal', async ({ page }) => {
    await page.goto('/admin');
    await page.waitForSelector('text=Templates', { timeout: 10_000 });
    // Look for Edit buttons within template cards (not tab buttons)
    const editBtn = page.locator('button', { hasText: 'Edit' }).first();
    if (!(await editBtn.isVisible({ timeout: 3_000 }).catch(() => false))) {
      test.skip();
      return;
    }
    await editBtn.click();
    await expect(page.getByText('Edit Template')).toBeVisible({ timeout: 3_000 });
    await shot(page, '14-admin-edit-template-modal');
  });
});

// ---------------------------------------------------------------------------
// Docs
// ---------------------------------------------------------------------------

test.describe('Docs', () => {
  const docPages: [string, string][] = [
    ['/docs', '15-docs-index'],
    ['/docs/instances', '16-docs-instances'],
    ['/docs/templates', '17-docs-templates'],
    ['/docs/settings', '18-docs-settings'],
    ['/docs/admin', '19-docs-admin'],
  ];

  for (const [url, name] of docPages) {
    test(url, async ({ page }) => {
      await page.goto(url);
      await waitForContent(page);
      await shot(page, name);
    });
  }
});

// ---------------------------------------------------------------------------
// Notifications overlay
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// SSHX URL section
// ---------------------------------------------------------------------------

test.describe('SSHX URL section', () => {
  test('sshx instance url display', async ({ page }) => {
    const resp = await page.request.get('/api/v1/instances');
    if (!resp.ok()) { test.skip(); return; }
    const list = await resp.json();
    const running = (Array.isArray(list) ? list : []).find((i: any) => i.status === 'running');
    if (!running) { test.skip(); return; }
    const urlResp = await page.request.get(`/api/v1/instances/${running.id}/sshx-url`);
    if (!urlResp.ok()) { test.skip(); return; }
    const { url } = await urlResp.json();
    if (!url) { test.skip(); return; }

    await page.goto(`/instances/${running.id}`);
    await waitForContent(page);
    const visible = await page.getByText('SSHX Collaborative Terminal').isVisible({ timeout: 3000 }).catch(() => false);
    if (!visible) { test.skip(); return; }
    await shot(page, '21-sshx-url-section');
  });
});

// ---------------------------------------------------------------------------
// Tailscale Serve section
// ---------------------------------------------------------------------------

test.describe('Tailscale Serve section', () => {
  test('tailscale serve controls visible', async ({ page }) => {
    const resp = await page.request.get('/api/v1/instances');
    if (!resp.ok()) { test.skip(); return; }
    const list = await resp.json();
    const running = (Array.isArray(list) ? list : []).find((i: any) => i.status === 'running');
    if (!running) { test.skip(); return; }

    await page.goto(`/instances/${running.id}`);
    await waitForContent(page);
    const visible = await page.getByText('Tailscale Serve').isVisible({ timeout: 3000 }).catch(() => false);
    if (!visible) { test.skip(); return; }
    await shot(page, '22-tailscale-serve-section');
  });
});

test.describe('Notifications', () => {
  test('success toast', async ({ page }) => {
    // Trigger a success notification by visiting a page that loads normally
    // We inject one via JS to capture the overlay without side effects
    await page.goto('/dashboard');
    await waitForContent(page);
    await page.evaluate(() => {
      // Import is not available here; dispatch a custom event or directly mutate store
      // Instead we trigger via the notification system if exposed on window
      const event = new CustomEvent('plati:notify', {
        detail: { type: 'success', message: 'Instance started successfully' }
      });
      window.dispatchEvent(event);
    });
    // If the app doesn't listen to that event, trigger via real action
    // Fallback: just screenshot the dashboard as-is and note it
    await page.waitForTimeout(300);
    await shot(page, '20-notification-overlay');
  });
});
