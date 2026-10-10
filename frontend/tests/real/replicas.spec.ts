import { test, expect } from './test';
import { fixture } from './fixture';

test('historical literal tree compares to current copies, with explicit bounded pages and empty filters', async ({
  page,
  catalog,
}) => {
  const old = catalog.snapshots.find((snapshot) => snapshot.captured_at === fixture.oldCapture)!;
  const calls: URL[] = [];
  const failures: string[] = [];
  page.on('pageerror', (error) => failures.push(error.message));
  page.on('response', (response) => {
    const url = new URL(response.url());
    if (url.pathname.endsWith('/directory/replicas')) calls.push(url);
    if (url.pathname.startsWith('/api/v1/') && response.status() >= 400)
      failures.push(String(response.status()));
  });
  await page.goto(
    `/snapshots/${old.id}/directory?${new URLSearchParams({ path: fixture.directory })}`,
  );
  await page.getByRole('button', { name: 'Exact tree copies', exact: true }).click();
  expect(calls).toHaveLength(0);
  await page.getByLabel('Comparison page size').fill('1');
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  const copies = page.getByRole('region', { name: 'Exact tree copies' });
  await expect(copies.getByText('Same disk', { exact: true })).toBeVisible();
  await expect(copies.getByText('Whole tree equal', { exact: true })).toHaveCount(1);
  expect(calls).toHaveLength(1);
  expect(calls[0]!.searchParams.get('path')).toBe(fixture.directory);
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(copies.getByText('Whole tree equal', { exact: true })).toHaveCount(2);
  expect(calls).toHaveLength(2);
  expect(calls[1]!.searchParams.has('cursor')).toBe(true);
  await page.getByRole('button', { name: 'Add block rule' }).click();
  await page.getByLabel('Block pattern 1', { exact: true }).fill('**');
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(copies.getByText('No files selected', { exact: true })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
  await page.goto(`/snapshots/${old.id}/directory?path=foo`);
  await page.getByRole('button', { name: 'Exact tree copies', exact: true }).click();
  await page.getByRole('button', { name: 'Add allow rule' }).click();
  await page.getByLabel('Allow pattern 1', { exact: true }).fill('**/report.txt');
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(copies.getByText('Equal under these filters', { exact: true })).toBeVisible();
  await expect(
    copies.getByText(/Historical source inventory\s+compared against current/),
  ).toBeVisible();
  expect(failures).toEqual([]);
});
