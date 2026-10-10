import { expect, test } from '@playwright/test';

test('built shell loads on entry and nested reload without fake data', async ({ page }) => {
  await page.route('**/healthz', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: '{"status":"ready"}',
    }),
  );
  await page.route('**/api/v1/catalog', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        revision: '0',
        scope: 'current',
        disk_count: '0',
        file_count: '0',
        content_count: '0',
      }),
    }),
  );
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Search', exact: true })).toBeVisible();
  await expect(page.getByRole('status')).toHaveText('Backend ready');
  await page.goto('/unknown/nested?path=literal%2F%E9%9B%AA');
  await page.reload();
  await expect(page.getByRole('main')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Coldcat' })).toHaveAttribute('href', '/');
  await expect(page.getByRole('link', { name: 'Search', exact: true })).toHaveAttribute(
    'href',
    '/',
  );
});

test('backend HTTP failure is visible', async ({ page }) => {
  await page.route('**/healthz', (route) => route.fulfill({ status: 503, body: 'Unavailable' }));
  await page.goto('/');
  await expect(page.getByRole('status')).toContainText('Backend unavailable');
  await expect(page.getByRole('button', { name: 'Retry connection' })).toBeVisible();
});

test('health alone is not catalog readiness and manual retry refreshes context', async ({
  page,
}) => {
  let available = false;
  let catalogChecks = 0;
  await page.route('**/healthz', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: '{"status":"ready"}',
    }),
  );
  await page.route('**/api/v1/catalog', (route) => {
    catalogChecks++;
    return route.fulfill(
      available
        ? {
            contentType: 'application/json',
            body: JSON.stringify({
              revision: '9007199254740993',
              scope: 'current',
              disk_count: '0',
              file_count: '0',
              content_count: '0',
            }),
          }
        : {
            status: 503,
            contentType: 'application/json',
            body: '{"error":{"code":"unavailable","message":"offline"}}',
          },
    );
  });
  await page.goto('/');
  await expect(page.getByRole('status')).toContainText('Backend unavailable');
  available = true;
  await page.getByRole('button', { name: 'Retry connection' }).click();
  await expect(page.getByRole('status')).toHaveText('Backend ready');
  expect(catalogChecks).toBeGreaterThanOrEqual(2);
});
