import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/visual',
  testMatch: 'u01-baseline.spec.ts',
  workers: 1,
  timeout: 120_000,
  use: {
    baseURL: 'http://127.0.0.1:4173',
    browserName: 'chromium',
    locale: 'en-US',
    timezoneId: 'UTC',
    deviceScaleFactor: 1,
    colorScheme: 'dark',
  },
  webServer: {
    command: 'node tests/serve-built.ts',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: false,
  },
});
