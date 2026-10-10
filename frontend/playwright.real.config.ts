import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/real',
  testMatch: '*.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  use: { browserName: 'chromium', trace: 'retain-on-failure' },
});
