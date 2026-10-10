import { expect, test } from '@playwright/test';

test('built shell loads on entry and nested reload without fake data', async ({ page }) => {
  await page.route('**/healthz', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: '{"status":"ready"}',
    }),
  );
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Coldcat frontend foundation' })).toBeVisible();
  await expect(page.getByRole('status')).toHaveText('Backend ready');
  await page.goto('/snapshots/123/directory?path=literal%2F%E9%9B%AA');
  await page.reload();
  await expect(page.getByRole('main')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Coldcat' })).toHaveAttribute('href', '/');
});

test('backend HTTP failure is visible', async ({ page }) => {
  await page.route('**/healthz', (route) => route.fulfill({ status: 503, body: 'Unavailable' }));
  await page.goto('/');
  await expect(page.getByRole('status')).toContainText('Backend unavailable');
  await expect(page.getByRole('button', { name: 'Retry connection' })).toBeVisible();
});
