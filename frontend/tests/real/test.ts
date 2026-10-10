import { test as base, expect } from '@playwright/test';
import { startCatalog, type Catalog } from './harness';
import { resolve } from 'node:path';

export const test = base.extend<object, { catalog: Catalog }>({
  page: async ({ page }, use, info) => {
    const operations = new Set<string>();
    page.on('response', (response) => {
      const path = new URL(response.url()).pathname.replace(/\/\d+(?=\/|$)/g, '/{id}');
      if (response.ok() && (path.startsWith('/api/v1/') || path === '/healthz'))
        operations.add(`${response.request().method()} ${path}`);
    });
    await use(page);
    await info.attach('real-operations', {
      body: JSON.stringify([...operations]),
      contentType: 'application/json',
    });
  },
  catalog: [
    async ({ browserName }, use, workerInfo) => {
      if (browserName !== 'chromium') throw new Error('Core integration targets Chromium');
      const catalog = await startCatalog(
        undefined,
        workerInfo.project.name === 'deployed' ? resolve('build') : undefined,
      );
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
