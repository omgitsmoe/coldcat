import { test, expect } from './test';

test('search return context survives child navigation, filters and file details', async ({
  page,
}) => {
  await page.goto('/?target=directories&q=foo&match=exact');
  await page.getByRole('link', { name: 'foo', exact: true }).click();
  await expect(page).toHaveURL(/\/snapshots\/\d+\/directory\?/);
  const returnTo = new URL(page.url()).searchParams.get('return_to');
  expect(returnTo).toBe('/?target=directories&q=foo&match=exact');
  await page.getByRole('link', { name: 'foo/child', exact: true }).click();
  await expect(page.getByRole('link', { name: 'Back to search', exact: true })).toBeVisible();
  expect(new URL(page.url()).searchParams.get('return_to')).toBe(returnTo);
  await page.getByRole('checkbox', { name: 'Recursive files', exact: true }).check();
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(page).toHaveURL(/recursive=true/);
  expect(new URL(page.url()).searchParams.get('return_to')).toBe(returnTo);
  await page
    .locator('[aria-label="Directory entry listing"] tbody a')
    .filter({ hasText: /^Content / })
    .first()
    .click();
  await page.getByRole('link', { name: 'Return to directory', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Directory browser', exact: true })).toBeVisible();
  expect(new URL(page.url()).searchParams.get('return_to')).toBe(returnTo);
  await page.getByRole('link', { name: 'Back to search', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Directories', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(1);
});

test('directory search toggles result type, paginates and restores directory navigation', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/?q=foo&field=path&limit=1&other_replicas=2');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  const dispatch = page.waitForResponse(
    (r) => new URL(r.url()).pathname === '/api/v1/search/directories',
  );
  await page.getByRole('button', { name: 'Directories', exact: true }).click();
  expect((await dispatch).status()).toBe(200);
  await expect(page.getByRole('button', { name: 'Directories', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page).toHaveURL(/target=directories/);
  expect(new URL(page.url()).searchParams.has('other_replicas')).toBe(false);
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(4);
  await expect(page.getByRole('combobox', { name: 'Other-copy metric' })).toHaveCount(0);
  await page.goto('/?target=directories&q=foo&limit=1');
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(1);
  const continuation = page.waitForResponse((r) => {
    const url = new URL(r.url());
    return url.pathname === '/api/v1/search/directories' && url.searchParams.has('cursor');
  });
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  expect((await continuation).status()).toBe(200);
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(2);
  await page.getByRole('searchbox').focus();
  await page.keyboard.press('ArrowDown');
  await expect(
    page.getByRole('button', { name: 'Open selected directory', exact: true }),
  ).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Directory browser', exact: true })).toBeVisible();
  await expect(page.locator('[aria-label="Directory breadcrumbs"]')).toContainText('foo');
  await page.getByRole('link', { name: 'Back to search', exact: true }).click();
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(2);
  await expect(page.locator('.directory-search-results tr.active')).toHaveCount(1);
  await expect(page.getByRole('searchbox')).toHaveValue('foo');
  await page.reload();
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(1);
  await page.getByRole('searchbox').fill('report.txt');
  await expect(page.getByText('No matching directories.', { exact: false })).toBeVisible();
  await page.getByRole('button', { name: 'Files', exact: true }).click();
  await expect(page.locator('.search-results tbody tr')).toHaveCount(4);
  expect(errors).toEqual([]);
});

test('directory literal, path, history and mobile results remain distinct from files', async ({
  page,
}) => {
  await page.setViewportSize({ width: 768, height: 900 });
  await page.goto('/?target=directories&q=foo&match=exact');
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(1);
  await expect(page.locator('.directory-search-results tbody')).toContainText('foo');
  await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('history');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(3);
  await expect(page.locator('.directory-search-results tbody')).toContainText('Historical');
  await page.goto('/?target=directories&q=foo&field=path');
  await expect(page.locator('.directory-search-results tbody tr')).toHaveCount(4);
  await page.getByRole('searchbox').fill('absent');
  await expect(page.getByText('No matching directories.', { exact: false })).toBeVisible();
  await page.getByRole('searchbox').fill('*');
  await expect(page.locator('.directory-search-results tbody')).toContainText('Root');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
});
