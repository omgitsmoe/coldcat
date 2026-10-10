import type { Locator, Page } from '@playwright/test';
import { test, expect } from './test';
import { fixture, reportPath } from './fixture';

const covered = new Set<string>();
const exposedReads = [
  '/healthz',
  '/api/v1/catalog',
  '/api/v1/search',
  '/api/v1/contents/lookup',
  '/api/v1/contents/{id}',
  '/api/v1/contents/{id}/observations',
  '/api/v1/observations/{id}',
  '/api/v1/disks',
  '/api/v1/disks/{id}',
  '/api/v1/disks/{id}/snapshots',
  '/api/v1/snapshots/{id}',
  '/api/v1/snapshots/{id}/directory',
  '/api/v1/snapshots/{id}/directory/entries',
];

test.beforeEach(async ({ page }) => {
  const unexpected: string[] = [];
  page.on('pageerror', (error) => unexpected.push(error.message));
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/v1/') && request.method() !== 'GET')
      unexpected.push(`Unexpected write: ${request.method()} ${path}`);
    if (/\/(replicas|coverage|directories)$/.test(path))
      unexpected.push(`Unexposed operation preloaded: ${path}`);
  });
  page.on('response', (response) => {
    const path = new URL(response.url()).pathname.replace(/\/\d+(?=\/|$)/g, '/{id}');
    if (response.status() === 200 && exposedReads.includes(path)) covered.add(path);
    if (path.startsWith('/api/v1/') && response.status() >= 400)
      unexpected.push(`${response.status()} ${path}`);
  });
  guardErrors.set(page, unexpected);
});

const guardErrors = new WeakMap<Page, string[]>();
test.afterEach(async ({ page }) => {
  expect(guardErrors.get(page), 'browser errors and unrequested operations').toEqual([]);
});

test.afterAll(() => {
  expect([...covered].sort(), 'successful browser requests for every F3–F6 exposed read').toEqual(
    [...exposedReads].sort(),
  );
});

async function keyboardOpen(page: Page, link: Locator) {
  await expect(link).toBeVisible();
  for (let tab = 0; tab < 100; tab++) {
    if (await link.evaluate((element) => element === document.activeElement)) {
      const destination = new URL((await link.getAttribute('href'))!, page.url()).href;
      await page.keyboard.press('Enter');
      await expect(page).toHaveURL(destination);
      return;
    }
    await page.keyboard.press('Tab');
  }
  throw new Error(`Link not reachable by Tab: ${await link.textContent()}`);
}

function value(page: Page, label: string) {
  return page
    .locator('dt')
    .filter({ hasText: new RegExp(`^${label}$`) })
    .locator('+ dd');
}

test('keyboard search → linked locations → observation → disk → inventory → literal directory', async ({
  page,
  catalog,
}) => {
  await page.goto('/');
  const query = page.getByRole('searchbox', { name: 'Filename or disk-relative path' });
  await expect(query).toBeFocused();
  await page.keyboard.type('REPORT');
  await expect(page.locator('tbody tr')).toHaveCount(4);
  const searchURL = page.url();
  await page.keyboard.press('ArrowDown');
  const selected = page.locator('[data-selected-content="true"]');
  await expect(selected).toBeVisible();
  const contentURL = (await selected.getAttribute('href'))!;
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(new URL(contentURL, catalog.origin).href);
  const identity = page.getByRole('region', { name: 'Content identity' });
  await expect(identity).toContainText(fixture.sharedHash);
  await expect(value(page, 'Current disks')).toHaveText('3');
  await expect(value(page, 'Current locations')).toHaveText('5');
  await expect(value(page, 'Historical observations \\(all complete inventories\\)')).toHaveText(
    '7',
  );
  await expect(page.locator('tbody tr')).toHaveCount(5);
  const featureCalls: string[] = [];
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/v1/')) featureCalls.push(path);
  });
  await page.goBack();
  await expect(query).toHaveValue('REPORT');
  await expect(page.locator('[data-selected-content="true"]')).toBeVisible();
  await query.press('Enter');
  await expect(page).toHaveURL(new URL(contentURL, catalog.origin).href);
  await expect(page.locator('tbody tr')).toHaveCount(5);
  expect(featureCalls.filter((path) => /^\/api\/v1\/observations\//.test(path))).toEqual([]);
  expect(featureCalls.filter((path) => /^\/api\/v1\/contents\//.test(path))).toHaveLength(2);
  expect(featureCalls.filter((path) => path === '/api/v1/search')).toEqual([]);
  const selectedRow = page.locator('tr[aria-current="true"]');
  await expect(selectedRow).toContainText(reportPath);
  const observation = selectedRow.getByRole('link', { name: /^Observation / });
  await keyboardOpen(page, observation);
  await expect(page.getByText(reportPath, { exact: true })).toBeVisible();
  await expect(value(page, 'Other current disks')).toHaveText('2');
  await expect(value(page, 'Other current locations')).toHaveText('4');
  await expect(page.locator(`time[datetime="${fixture.mtime}"]`)).toBeVisible();
  await expect(page.locator(`time[datetime="${fixture.currentCapture}"]`)).toBeVisible();
  const latest = catalog.snapshots[0]!;
  expect(latest.captured_at).toBe(fixture.currentCapture);
  await expect(page.locator(`time[datetime="${latest.imported_at}"]`)).toBeVisible();
  await expect(page.getByText(/Source mtime:/)).toBeVisible();
  await expect(page.getByText(/Imported at:/)).toBeVisible();
  const observationURL = page.url();
  await page.reload();
  await expect(page.getByText(reportPath, { exact: true })).toBeVisible();
  await keyboardOpen(page, page.getByRole('link', { name: `Disk ${fixture.label}`, exact: true }));
  await expect(value(page, 'Declared capacity')).toHaveText('9,007,199,254,740,993 B');
  await expect(value(page, 'Serial')).toHaveText(fixture.serial);
  await expect(value(page, 'Notes')).toHaveText(fixture.notes);
  await expect(page.getByRole('region', { name: 'Latest complete inventory' })).toContainText(
    `Inventory ${latest.id}`,
  );
  await expect(page.getByRole('region', { name: 'Cataloged inventory size' })).toContainText(
    'Unknown',
  );
  const latestLink = page
    .getByRole('region', { name: 'Latest complete inventory' })
    .getByRole('link', { name: `Inventory ${latest.id}`, exact: true });
  await keyboardOpen(page, latestLink);
  await expect(value(page, 'Capture provenance')).toHaveText('Explicit capture time');
  await expect(value(page, 'Source input path \\(backend provenance\\)')).toHaveText(
    latest.input_path,
  );
  await expect(value(page, 'Source input format')).toHaveText('cshd');
  await expect(value(page, 'Files')).toHaveText('8');
  await keyboardOpen(page, page.getByRole('link', { name: 'Browse root', exact: true }));
  await expect(page.getByRole('region', { name: 'Directory summary' })).toContainText(
    'Current source inventory',
  );
  await keyboardOpen(page, page.getByRole('link', { name: 'foo', exact: true }));
  await keyboardOpen(page, page.getByRole('link', { name: fixture.directory, exact: true }));
  await expect(page.getByRole('table', { name: 'Directory entries' })).toContainText(reportPath);
  expect(new URL(page.url()).searchParams.get('path')).toBe(fixture.directory);
  await page.reload();
  await expect(page.getByRole('table', { name: 'Directory entries' })).toContainText(reportPath);
  await page.goto(observationURL);
  await keyboardOpen(page, page.getByRole('link', { name: 'Containing directory', exact: true }));
  expect(new URL(page.url()).searchParams.get('path')).toBe(fixture.directory);
  await page.goBack();
  await keyboardOpen(page, page.getByRole('link', { name: 'Return to search', exact: true }));
  await expect(query).toHaveValue('REPORT');
  await expect(page).toHaveURL(searchURL);
  await expect(page.locator('tbody tr')).toHaveCount(4);
  await page.reload();
  await expect(page.locator('tbody tr')).toHaveCount(4);
});

test('same names, aliases, repeated history, historical-only identities, zero and unknown', async ({
  page,
}) => {
  await page.goto('/?q=REPORT');
  await expect(page.locator('tbody tr')).toHaveCount(4);
  const different = page.locator('tbody tr').filter({ hasText: 'foobar/report.txt' });
  const differentContent = await different
    .getByRole('link', { name: 'report.txt', exact: true })
    .getAttribute('href');
  await page
    .locator('tbody tr')
    .filter({ hasText: reportPath })
    .getByRole('link', { name: 'report.txt', exact: true })
    .click();
  expect(new URL(page.url()).pathname).not.toBe(new URL(differentContent!, page.url()).pathname);
  await expect(value(page, 'Current locations')).toHaveText('5');
  await page.getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(7);
  await expect(page.getByText('Historical', { exact: true })).toHaveCount(2);
  await expect(value(page, 'Current locations')).toHaveText('5');
  await page.goto('/?q=alias');
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await page
    .locator('tbody tr')
    .first()
    .getByRole('link', { name: 'alias.txt', exact: true })
    .click();
  await expect(page.getByText(fixture.sharedHash, { exact: true })).toBeVisible();
  await page.goto('/?q=retired');
  await expect(page.getByText(/No matching observations/)).toBeVisible();
  await page.goto('/?q=retired&scope=history');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr')).toContainText('Historical');
  await page.getByRole('link', { name: 'retired.txt', exact: true }).click();
  await expect(value(page, 'Current disks')).toHaveText('0');
  await expect(value(page, 'Current locations')).toHaveText('0');
  await expect(
    page.getByText('No current locations in complete inventories.', { exact: true }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page
    .locator('tbody tr')
    .getByRole('link', { name: /^Observation / })
    .click();
  await expect(value(page, 'Other current disks')).toHaveText('0');
  await expect(value(page, 'Other current locations')).toHaveText('0');
  await expect(page.getByText('Source mtime: Unknown', { exact: true })).toBeVisible();
  for (const [q, size] of [
    ['unknown', 'Unknown'],
    ['zero', '0 B'],
  ]) {
    await page.goto(`/?q=${q}`);
    await expect(page.locator('tbody tr')).toHaveCount(1);
    await expect(page.locator('tbody tr').locator('td').nth(2)).toHaveText(size!);
    await page.getByRole('link', { name: `${q}.txt`, exact: true }).click();
    await expect(value(page, 'Size')).toHaveText(size!);
  }
  await page.goto('/?q=短&match=exact');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr')).toContainText('0 B');
  const [tab] = await Promise.all([
    page.context().waitForEvent('page'),
    page.getByRole('link', { name: '短', exact: true }).click({ modifiers: ['Control'] }),
  ]);
  try {
    await expect(value(tab, 'Size')).toHaveText('0 B');
    await expect(value(tab, 'Current disks')).toHaveText('1');
  } finally {
    await tab.close();
  }
  await page.goto(`/?${new URLSearchParams({ q: reportPath, field: 'path', match: 'exact' })}`);
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr')).toContainText(reportPath);
});

test('real read pagination, capture order, directory boundaries, literal paths and replica filters', async ({
  page,
  catalog,
}) => {
  await page.goto('/disks?limit=1');
  await expect(page.locator('main article')).toHaveCount(1);
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.locator('main article')).toHaveCount(2);
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.locator('main article')).toHaveCount(3);
  await page.getByRole('link', { name: fixture.label, exact: true }).click();
  await page.goto(`/disks/${catalog.diskID}?limit=1`);
  const history = page.getByRole('region', { name: 'Inventory history' });
  await expect(history.locator('article')).toHaveCount(1);
  for (let n = 2; n <= 3; n++) {
    await history.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(history.locator('article')).toHaveCount(n);
  }
  for (const [i, capture] of [
    fixture.currentCapture,
    fixture.middleCapture,
    fixture.oldCapture,
  ].entries()) {
    await expect(
      history.locator('article').nth(i).locator(`time[datetime="${capture}"]`),
    ).toBeVisible();
  }
  const latest = catalog.snapshots[0]!;
  const browse = (path: string, extra = {}) =>
    `/snapshots/${latest.id}/directory?${new URLSearchParams({ path, ...extra })}`;
  await page.goto(browse('foo', { limit: '1' }));
  const entries = page.getByRole('table', { name: 'Directory entries' });
  await expect(entries.locator('tbody tr')).toHaveCount(1);
  for (let n = 2; n <= 5; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(entries.locator('tbody tr')).toHaveCount(n);
  }
  await expect(entries).not.toContainText('foobar');
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText('21 B');
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText('7 B');
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText('Unknown');
  await page.getByLabel('Exact other replicas').fill('0');
  await page.getByLabel('Page limit', { exact: true }).fill('50');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(entries.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  await expect(entries.getByRole('link', { name: fixture.directory, exact: true })).toBeVisible();
  await page.goto(browse('foo', { recursive: 'true', other_replicas: '0' }));
  await expect(entries.locator('tbody tr')).toHaveCount(2);
  await expect(entries).toContainText('foo/unknown.txt');
  await expect(entries).toContainText('foo/zero.txt');
  const unknown = entries.locator('tbody tr').filter({ hasText: 'foo/unknown.txt' });
  await unknown.getByRole('link', { name: /^Observation / }).click();
  await page.getByRole('link', { name: 'Return to directory', exact: true }).click();
  await expect(entries.locator('tbody tr')).toHaveCount(2);
  await page.goto(browse(fixture.longDirectory));
  await expect(entries).toContainText(`${fixture.longDirectory}/long-report.txt`);
  await page.reload();
  await expect(entries).toContainText(`${fixture.longDirectory}/long-report.txt`);
  const oldest = catalog.snapshots[2]!;
  await page.goto(`/snapshots/${oldest.id}/directory?path=foo&recursive=true`);
  await expect(page.getByRole('region', { name: 'Directory summary' })).toContainText(
    'Historical source inventory',
  );
  await expect(entries.locator('tbody tr')).toHaveCount(2);
  await expect(entries.locator('tbody tr').filter({ hasText: reportPath })).toContainText(
    '2 other disks',
  );
  await expect(entries.locator('tbody tr').filter({ hasText: 'foo/retired.txt' })).toContainText(
    '0 other disks',
  );
  await page.goto('/contents');
  await page.getByLabel('Algorithm', { exact: true }).selectOption('sha256');
  await page.getByLabel('Digest (hex)', { exact: true }).fill(fixture.sharedHash.toUpperCase());
  await page.getByRole('button', { name: 'Look up content', exact: true }).click();
  await expect(page.getByText(/Known content:/)).toContainText(fixture.sharedHash);
  await page.getByRole('link', { name: /^Open content / }).click();
  await expect(value(page, 'Current locations')).toHaveText('5');
  const contentID = new URL(page.url()).pathname.split('/').at(-1)!;
  await page.goto(`/contents/${contentID}?scope=history&limit=2`);
  await expect(page.locator('tbody tr')).toHaveCount(2);
  for (const n of [4, 6, 7]) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.locator('tbody tr')).toHaveCount(n);
  }
  await page.goto(`/?q=REPORT&limit=1&disk_id=${catalog.diskID}&directory=foo`);
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr')).toContainText(reportPath);
  await expect(page.getByRole('button', { name: 'Load more', exact: true })).toHaveCount(0);
  await page.goto('/?q=REPORT&limit=1');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  for (let n = 2; n <= 4; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.locator('tbody tr')).toHaveCount(n);
  }
});
