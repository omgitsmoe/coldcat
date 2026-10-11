import type { Page } from '@playwright/test';

export async function shellFixtures(page: Page) {
  const calls: string[] = [];
  await page.route('**/healthz', (route) => {
    calls.push('/healthz');
    return route.fulfill({ json: { status: 'ready' } });
  });
  await page.route('**/api/v1/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    calls.push(path);
    if (path === '/api/v1/catalog') {
      return route.fulfill({
        json: {
          revision: '0',
          scope: 'current',
          disk_count: '0',
          file_count: '0',
          content_count: '0',
        },
      });
    }
    return route.fulfill({
      status: 404,
      json: { error: { code: 'not_found', message: 'Fixture resource not found' } },
    });
  });
  return calls;
}
