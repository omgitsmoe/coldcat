import { test, expect } from './test';

test('wildcard and Boolean search use the deployed API and retain bookmark syntax', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/?q=foo*report.txt&field=path&match=wildcard&limit=1');
  await expect(page.locator('#search-syntax-hint')).toContainText(
    'Wildcard matches the whole field',
  );
  await expect(page.locator('tbody tr')).toHaveCount(1);
  const continuation = page.waitForResponse((r) => {
    const url = new URL(r.url());
    return url.pathname === '/api/v1/search' && url.searchParams.has('cursor');
  });
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  expect((await continuation).status()).toBe(200);
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.getByRole('combobox', { name: 'Match', exact: true })).toHaveValue('wildcard');
  await expect(page.locator('tbody')).toContainText('match');
  const content = page.getByRole('link', { name: 'report.txt', exact: true }).first();
  const destination = new URL((await content.getAttribute('href'))!, page.url());
  const returnTo = new URL(destination.searchParams.get('return_to')!, page.url());
  expect(returnTo.searchParams.get('match')).toBe('wildcard');
  expect(returnTo.searchParams.get('q')).toBe('foo*report.txt');
  await content.click();
  await expect(page).toHaveURL(/\/contents\/\d+\?/);
  await page.goBack();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await page.getByText('Search syntax help', { exact: true }).click();
  await expect(page.locator('.syntax-help')).toContainText('Escape literal characters');
  await page.getByRole('combobox', { name: 'Match', exact: true }).selectOption('expression');
  await page.getByRole('searchbox').fill('(foobar OR other) AND report');
  const response = page.waitForResponse((r) => {
    const url = new URL(r.url());
    return url.pathname === '/api/v1/search' && url.searchParams.get('match') === 'expression';
  });
  await page.getByRole('button', { name: 'Apply filters' }).click();
  expect((await response).status()).toBe(200);
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.locator('.applied-summary')).toContainText('Path · AND / OR');
  await expect(page.locator('#search-syntax-hint')).toContainText('AND binds before OR');
  await page.reload();
  await expect(page.getByRole('searchbox')).toHaveValue('(foobar OR other) AND report');
  await expect(page.getByRole('combobox', { name: 'Match', exact: true })).toHaveValue(
    'expression',
  );
  await expect(page.locator('tbody tr')).toHaveCount(2);
  expect(errors).toEqual([]);
});
