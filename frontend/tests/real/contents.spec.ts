import { test, expect } from './test';
import { fixture } from './fixture';

test('distinct contents: aliases deduplicate, membership counts stay global, history-only is zero-safe', async ({
  page,
  catalog,
}) => {
  const failures: string[] = [];
  page.on('pageerror', (error) => failures.push(error.message));
  page.on('response', (response) => {
    if (new URL(response.url()).pathname.startsWith('/api/v1/') && response.status() >= 400)
      failures.push(String(response.status()));
  });
  const members = `/contents?disk_id=${catalog.diskID}&directory=foo`;
  await page.goto(members);
  const rows = page.locator('tbody tr');
  await expect(rows).toHaveCount(3);
  const shared = rows.filter({ hasText: fixture.sharedHash });
  await expect(shared).toHaveCount(1);
  await expect(shared).toContainText('3 current disks');
  await expect(shared).toContainText('5 current locations');
  await expect(shared).toContainText('2 other disks');
  await page
    .getByRole('combobox', { name: 'Current replica metric', exact: true })
    .selectOption('locations');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(shared).toContainText('4 other locations');
  await page.getByRole('button', { name: 'Zero other disks', exact: true }).click();
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(rows).toHaveCount(2);
  await expect(shared).toHaveCount(0);
  await expect(rows.filter({ hasText: 'Unknown' })).toHaveCount(1);
  await expect(rows.filter({ hasText: '0 B' })).toHaveCount(1);
  await page.goto(`${members}&scope=history`);
  await expect(rows).toHaveCount(4);
  const retired = rows.filter({ hasText: '0 current disks' });
  await expect(retired).toHaveCount(1);
  await expect(retired).toContainText('0 other disks');
  await retired.getByRole('link').click();
  await expect(
    page.getByText('No current locations in complete inventories.', { exact: true }),
  ).toBeVisible();
  await page.goto(`${members}&limit=1`);
  await expect(rows).toHaveCount(1);
  for (let n = 2; n <= 3; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(rows).toHaveCount(n);
  }
  await expect(page.getByRole('button', { name: 'Load more', exact: true })).toHaveCount(0);
  expect(failures).toEqual([]);
});
