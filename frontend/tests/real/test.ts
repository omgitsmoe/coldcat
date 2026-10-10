import { test as base, expect } from '@playwright/test';
import { startCatalog, type Catalog } from './harness';

export const test = base.extend<object, { catalog: Catalog }>({
  catalog: [
    async ({ browserName }, use) => {
      if (browserName !== 'chromium') throw new Error('Core integration targets Chromium');
      const catalog = await startCatalog();
      try {
        await use(catalog);
      } finally {
        await catalog.close();
      }
    },
    { scope: 'worker', timeout: 180_000 },
  ],
  baseURL: async ({ catalog }, use) => {
    await use(catalog.origin);
  },
});

export { expect };
