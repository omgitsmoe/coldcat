import { test, expect } from './test';

test('default syntax combines wildcards and Boolean search with pagination and navigation', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/?q=foo*report.txt&field=path&limit=1');
  await expect(page.locator('#search-syntax-hint')).toContainText('Matches anywhere');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  const continuation = page.waitForResponse((r) => {
    const url = new URL(r.url());
    return url.pathname === '/api/v1/search' && url.searchParams.has('cursor');
  });
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  expect((await continuation).status()).toBe(200);
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.getByRole('combobox', { name: 'Match', exact: true })).toHaveValue('syntax');
  await expect(page.locator('tbody')).toContainText('match');
  const content = page.getByRole('link', { name: 'report.txt', exact: true }).first();
  const destination = new URL((await content.getAttribute('href'))!, page.url());
  const returnTo = new URL(destination.searchParams.get('return_to')!, page.url());
  expect(returnTo.searchParams.get('match')).toBeNull();
  expect(returnTo.searchParams.get('q')).toBe('foo*report.txt');
  await content.click();
  await expect(page).toHaveURL(/\/contents\/\d+\?/);
  await page.goBack();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await page.getByText('Search syntax help', { exact: true }).click();
  await expect(page.locator('.syntax-help')).toContainText('Escape literal characters');
  await page.getByRole('combobox', { name: 'Match', exact: true }).selectOption('syntax');
  await page.getByRole('searchbox').fill('(foobar*txt OR other*txt) AND report');
  const response = page.waitForResponse((r) => {
    const url = new URL(r.url());
    return url.pathname === '/api/v1/search' && url.searchParams.get('match') === 'syntax';
  });
  await page.getByRole('button', { name: 'Apply filters' }).click();
  expect((await response).status()).toBe(200);
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.locator('.applied-summary')).toContainText('Path · Search syntax');
  await expect(page.locator('#search-syntax-hint')).toContainText('AND binds before OR');
  await page.reload();
  await expect(page.getByRole('searchbox')).toHaveValue('(foobar*txt OR other*txt) AND report');
  await expect(page.getByRole('combobox', { name: 'Match', exact: true })).toHaveValue('syntax');
  await expect(page.locator('tbody tr')).toHaveCount(2);
  expect(errors).toEqual([]);
});

test('spaces imply AND while quoted phrases stay literal', async ({ page }) => {
  await page.goto('/?q=report%20foobar&field=path');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody')).toContainText('foobar/report.txt');
  await expect(page.locator('#search-syntax-hint')).toContainText('Spaces imply AND');
  await page.getByRole('searchbox').fill('"report foobar"');
  await expect(page.getByText('No matching observations.', { exact: false })).toBeVisible();
  await page.getByRole('searchbox').fill('"foobar/report.txt"');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByText('Search syntax help', { exact: true }).click();
  await expect(page.locator('.syntax-help')).toContainText('tax AND return');
});

test('whole-field and literal options remain explicit', async ({ page }) => {
  await page.goto('/?q=report.txt&field=path');
  await expect(page.locator('tbody tr')).toHaveCount(4);
  await page.getByRole('combobox', { name: 'Match', exact: true }).selectOption('whole');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.locator('#search-syntax-hint')).toContainText('Whole-field terms');
  await expect(page.getByText('No matching observations.', { exact: false })).toBeVisible();
  await page.getByRole('searchbox').fill('*report.txt');
  await expect(page.locator('tbody tr')).toHaveCount(4);
  await page.getByRole('combobox', { name: 'Match', exact: true }).selectOption('substring');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByText('No matching observations.', { exact: false })).toBeVisible();
  await page.getByRole('searchbox').fill('report.txt');
  await expect(page.locator('tbody tr')).toHaveCount(4);
  await page.getByRole('combobox', { name: 'Match', exact: true }).selectOption('exact');
  await page.getByRole('searchbox').fill('foobar/report.txt');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('.applied-summary')).toContainText('Path · Exact');
});
