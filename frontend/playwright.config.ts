import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  outputDir: './tests/screenshots-output',
  fullyParallel: false,
  retries: 0,
  workers: 1,
  reporter: 'list',

  use: {
    baseURL: process.env.BASE_URL ?? 'http://localhost:5173',
    viewport: { width: 1440, height: 900 },
    screenshot: 'off', // we take manual screenshots
    trace: 'off',
    // Auth cookie is preserved via storageState set in global setup
  },

  projects: [
    // Global setup: logs in and saves auth state
    {
      name: 'setup',
      testMatch: '**/auth.setup.ts',
    },
    // All screenshot tests depend on auth setup
    {
      name: 'screenshots',
      dependencies: ['setup'],
      use: {
        ...devices['Desktop Chrome'],
        storageState: './tests/.auth/state.json',
      },
      testMatch: '**/screenshot.test.ts',
    },
  ],
});
