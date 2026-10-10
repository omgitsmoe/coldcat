import { expect, test, type Page } from '@playwright/test';
import { contentFixture } from '../fixtures/details';

async function ready(page: Page) {
  await page.route('**/healthz', (r) => r.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (r) =>
    r.fulfill({
      json: {
        revision: '3',
        scope: 'current',
        disk_count: '3',
        file_count: '4',
        content_count: '2',
      },
    }),
  );
}
function result(id = '1', cursor: string | null = null, scope = 'current') {
  const item = contentFixture();
  item.id = id;
  item.scope = scope as 'current' | 'history';
  return {
    scope,
    revision: '3',
    filters: {
      disk_id: null,
      directory: '',
      replica_metric: 'disks',
      other_replicas: null,
      min_other_replicas: null,
      max_other_replicas: null,
    },
    items: [item],
    next_cursor: cursor,
  };
}

test('membership counts stay catalog-wide; history-only zero, huge, null and known-zero identities', async ({
  page,
}) => {
  await ready(page);
  const calls: URL[] = [];
  await page.route('**/api/v1/contents?**', (r) => {
    const url = new URL(r.request().url());
    calls.push(url);
    const body = result('1', null, url.searchParams.get('scope')!);
    body.items[0]!.current_disk_count = '9007199254740993';
    body.items[0]!.current_location_count = '9007199254740994';
    body.items[0]!.size = null;
    const retired = {
      ...body.items[0]!,
      id: '2',
      size: '0',
      current_disk_count: '0',
      current_location_count: '0',
    };
    body.items.push(retired);
    return r.fulfill({ json: body });
  });
  await page.goto('/contents?scope=history&disk_id=1&directory=foo');
  const rows = page.locator('tbody tr');
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toContainText('9,007,199,254,740,992');
  await expect(rows.first()).toContainText('Unknown');
  await expect(rows.last()).toContainText('0 B');
  await expect(rows.last()).not.toContainText('-1');
  await expect(page.getByText(/Counts are catalog-wide/)).toBeVisible();
  await expect(rows.first().getByRole('link')).toHaveAttribute('href', '/contents/1');
  expect(calls).toHaveLength(1);
  await page.getByLabel('Exact other replicas', { exact: true }).fill('0');
  await expect(rows).toHaveCount(0);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('current scope');
  expect(calls).toHaveLength(1);
  await page.getByRole('button', { name: 'Zero other disks', exact: true }).click();
  await expect(page.getByLabel('Exact other replicas', { exact: true })).toHaveValue('0');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(rows).toHaveCount(2);
  expect(calls.at(-1)!.searchParams.get('other_replicas')).toBe('0');
  expect(calls.at(-1)!.searchParams.get('scope')).toBe('current');
  await page.getByLabel('Minimum other replicas', { exact: true }).fill('1');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Exact bounds');
});

test('bounded later pages, retained failure retry and stale restart', async ({ page }) => {
  await ready(page);
  let failure = false;
  let stale = false;
  await page.route('**/api/v1/contents?**', (r) => {
    const cursor = new URL(r.request().url()).searchParams.get('cursor');
    if (cursor && (failure || stale))
      return r.fulfill({
        status: stale ? 409 : 500,
        json: { error: { code: stale ? 'stale_cursor' : 'internal_error', message: 'failed' } },
      });
    const n = cursor ? Number(cursor) : 1;
    return r.fulfill({ json: result(String(n), n === 12 ? null : String(n + 1)) });
  });
  await page.goto('/contents?limit=1');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  failure = true;
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  failure = false;
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  for (let n = 3; n <= 10; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.locator('tbody tr')).toHaveCount(n);
  }
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr a')).toHaveText('Content 11');
  stale = true;
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await expect(page.getByText(/Inventory changed/)).toBeVisible();
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.locator('tbody tr a')).toHaveText('Content 1');
});

test('draft changes cancel delayed pages and invalid bookmarked combinations never dispatch', async ({
  page,
}) => {
  await ready(page);
  let release!: () => void;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/api/v1/contents?**', async (r) => {
    await wait;
    await r.fulfill({ json: result() }).catch(() => {});
  });
  await page.goto('/contents');
  await page.getByLabel('Disk ID', { exact: true }).fill('2');
  release();
  await expect(page.getByText(/Draft filters/)).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await page.goto('/contents?scope=history&other_replicas=0');
  await expect(page.getByRole('alert')).toContainText('current scope');
});

test('obsolete continuation cannot append after a locations/range filter replacement', async ({
  page,
}) => {
  await ready(page);
  let pending: import('@playwright/test').Route | undefined;
  const calls: URL[] = [];
  await page.route('**/api/v1/contents?**', async (r) => {
    const url = new URL(r.request().url());
    calls.push(url);
    if (url.searchParams.has('cursor')) {
      pending = r;
      return;
    }
    return r.fulfill({
      json: result(url.searchParams.get('replica_metric') === 'locations' ? '9' : '1', 'next'),
    });
  });
  await page.goto('/contents');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect.poll(() => pending !== undefined).toBe(true);
  await page
    .getByRole('combobox', { name: 'Current replica metric', exact: true })
    .selectOption('locations');
  await page.getByLabel('Minimum other replicas', { exact: true }).fill('1');
  await page.getByLabel('Maximum other replicas', { exact: true }).fill('2');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(page.locator('tbody tr a')).toHaveText('Content 9');
  await pending!.fulfill({ json: result('2') }).catch(() => {});
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr a')).toHaveText('Content 9');
  expect(calls.at(-1)!.searchParams.get('min_other_replicas')).toBe('1');
  expect(calls.at(-1)!.searchParams.get('max_other_replicas')).toBe('2');
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
});
