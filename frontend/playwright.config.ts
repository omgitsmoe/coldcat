import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/browser',
  fullyParallel: false,
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:4173', browserName: 'chromium' },
  webServer: {
    command: 'node tests/serve-built.ts',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: false,
  },
});
