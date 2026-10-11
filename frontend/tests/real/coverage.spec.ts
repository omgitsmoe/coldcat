import { test, expect } from './test';
import { fixture } from './fixture';

test('historical source has distributed current coverage, not one complete copy', async ({
  page,
  catalog,
}) => {
  const old = catalog.snapshots.find((snapshot) => snapshot.captured_at === fixture.oldCapture)!;
  const calls: URL[] = [];
  const failures: string[] = [];
  page.on('pageerror', (error) => failures.push(error.message));
  page.on('response', (response) => {
    const url = new URL(response.url());
    if (url.pathname.endsWith('/directory/coverage')) calls.push(url);
    if (url.pathname.startsWith('/api/v1/') && response.status() >= 400)
      failures.push(String(response.status()));
  });
  await page.goto(
    `/snapshots/${old.id}/directory?${new URLSearchParams({ path: fixture.coverageDirectory })}`,
  );
  await page.getByRole('button', { name: 'Content on other disks', exact: true }).click();
  expect(calls).toHaveLength(0);
  await page.getByLabel('Comparison page size').fill('1');
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  const coverage = page.getByRole('region', { name: 'Content on other disks' });
  await expect(coverage).toContainText(
    'Historical source inventory compared against current destination inventories.',
  );
  await expect(coverage).toContainText('Selected source files: 3; excluded source files: 0');
  await expect(coverage).toContainText('Selected distinct contents: 2');
  const rows = coverage.locator('.coverage-results tbody tr');
  const covered = rows.first().locator('td[data-label="Covered"]');
  const missing = rows.first().locator('td[data-label="Missing"]');
  await expect(covered).toContainText('Files: 2');
  await expect(missing).toContainText('Files: 1');
  await expect(covered).toContainText('Known covered source bytes: 26 B');
  await expect(missing).toContainText('Known missing source bytes: 0 B');
  await expect(covered).toContainText('Unknown-size files: 0');
  await expect(missing).toContainText('Unknown-size files: 1');
  await expect(coverage.getByText('Partial content coverage', { exact: true })).toHaveCount(1);
  expect(calls).toHaveLength(1);
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(coverage.getByText('Partial content coverage', { exact: true })).toHaveCount(2);
  const laterCovered = rows.last().locator('td[data-label="Covered"]');
  const laterMissing = rows.last().locator('td[data-label="Missing"]');
  await expect(laterCovered).toContainText('Files: 1');
  await expect(laterMissing).toContainText('Files: 2');
  await expect(laterCovered).toContainText('Unknown-size files: 1');
  await expect(laterMissing).toContainText('Unknown-size files: 0');
  await expect(coverage).toContainText('Distributed coverage is not one complete directory backup');
  await expect(coverage.getByText('Complete content coverage', { exact: true })).toHaveCount(0);
  expect(calls[0]!.searchParams.get('path')).toBe(fixture.coverageDirectory);
  expect(calls[1]!.searchParams.has('cursor')).toBe(true);
  await expect(coverage.getByRole('link', { name: /Backup β — Disk/ })).toBeVisible();
  await expect(coverage.getByRole('link', { name: /Third γ — Disk/ })).toBeVisible();
  await page.getByRole('button', { name: 'Add block rule' }).click();
  await page.getByLabel('Block pattern 1', { exact: true }).fill('**');
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(coverage.getByText('No files selected', { exact: true })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
  expect(failures).toEqual([]);
});
