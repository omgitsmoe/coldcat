import { expect, test, type Page, type Route } from '@playwright/test';
import { mkdir, writeFile } from 'node:fs/promises';
import { diskFixture, searchFixture } from '../fixtures/api';
import { contentFixture } from '../fixtures/details';

const revision = '9007199254740993';
function disk(id: string) {
  return { ...diskFixture(), id, label: `Archive 雪 <${id}> / literal label` };
}
function inventory(id: string, disk_id = '1') {
  return {
    ...searchFixture().items[0]!.snapshot,
    id,
    disk_id,
    captured_at: id === '2' ? '1969-12-01T00:00:00Z' : '1970-01-01T00:00:10.000000123Z',
  };
}
function contentPage() {
  return {
    revision,
    scope: 'current',
    filters: {
      disk_id: null,
      directory: '',
      replica_metric: 'disks',
      other_replicas: null,
      min_other_replicas: null,
      max_other_replicas: null,
    },
    items: [contentFixture()],
    next_cursor: null,
  };
}
async function setup(page: Page, options?: (route: Route, url: URL) => unknown) {
  const calls: URL[] = [];
  const unexpected: string[] = [];
  await page.route('**/healthz', (r) => r.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/**', (r) => {
    const url = new URL(r.request().url());
    calls.push(url);
    if (url.pathname === '/api/v1/catalog')
      return r.fulfill({
        json: {
          revision,
          scope: 'current',
          disk_count: '3',
          file_count: '4',
          content_count: '2',
        },
      });
    if (url.pathname === '/api/v1/search') return r.fulfill({ json: searchFixture() });
    if (url.pathname === '/api/v1/contents') return r.fulfill({ json: contentPage() });
    if (
      url.pathname === '/api/v1/disks' ||
      /^\/api\/v1\/disks\/\d+\/snapshots$/.test(url.pathname)
    ) {
      if (options) return options(r, url);
      return r.fulfill({
        json: {
          revision,
          ...(url.pathname.endsWith('snapshots') ? { disk_id: url.pathname.split('/')[4] } : {}),
          items: url.pathname.endsWith('snapshots')
            ? [inventory('3'), inventory('2')]
            : [disk('1')],
          next_cursor: null,
        },
      });
    }
    unexpected.push(url.pathname);
    return r.fulfill({
      status: 500,
      json: { error: { code: 'internal_error', message: 'unexpected read' } },
    });
  });
  return {
    calls,
    unexpected,
    options: () => calls.filter((u) => u.pathname.startsWith('/api/v1/disks')),
    results: () => calls.filter((u) => ['/api/v1/search', '/api/v1/contents'].includes(u.pathname)),
  };
}

test('search lazy pickers retain bookmark IDs; opening and paging do not edit or apply', async ({
  page,
}) => {
  const state = await setup(page, (r, url) =>
    r.fulfill({
      json: {
        revision,
        items: [disk(url.searchParams.has('cursor') ? '2' : '1')],
        next_cursor: url.searchParams.has('cursor') ? null : 'next',
      },
    }),
  );
  await page.goto('/?q=archive&disk_id=9007199254740993');
  await expect(page.getByRole('button', { name: 'Select', exact: false })).toBeVisible();
  expect(state.options()).toHaveLength(0);
  await expect(page.getByLabel('Disk ID', { exact: true })).toHaveValue('9007199254740993');
  await expect(page.getByText(/Disk ID 9007199254740993: unknown/)).toBeVisible();
  const url = page.url();
  await page.getByRole('button', { name: 'Choose disk', exact: true }).click();
  const options = page.getByRole('region', { name: 'Disk options', exact: true });
  await expect(options.getByText('Archive 雪 <1> / literal label')).toBeVisible();
  expect(state.options()).toHaveLength(1);
  expect(state.results()).toHaveLength(1);
  await expect(page.getByText(/Unapplied changes/)).toHaveCount(0);
  await options.getByRole('button', { name: 'Load more options' }).click();
  await expect(options.getByRole('button', { name: 'Use disk 2' })).toBeVisible();
  expect(state.options().map((u) => u.searchParams.get('limit'))).toEqual(['25', '25']);
  expect(state.options()[1]!.searchParams.get('cursor')).toBe('next');
  await expect(page).toHaveURL(url);
  expect(state.results()).toHaveLength(1);
  await options.getByRole('button', { name: 'Use disk 2' }).click();
  await expect(page.getByText(/Unapplied changes/)).toBeVisible();
  await expect(page.getByLabel('Disk ID', { exact: true })).toHaveValue('2');
  expect(state.results()).toHaveLength(1);
  await expect(page).toHaveURL(url);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect.poll(() => state.results().length).toBe(2);
  expect(state.results()[1]!.searchParams.get('disk_id')).toBe('2');
  expect(page.url()).not.toContain('Archive');
  expect(state.unexpected).toEqual([]);
});

test('capture choices and standalone snapshot bookmarks never switch scope or apply', async ({
  page,
}) => {
  const state = await setup(page);
  await page.goto('/?q=archive&snapshot_id=99');
  await expect(page.getByLabel('Snapshot ID', { exact: true })).toHaveValue('99');
  await page.getByRole('button', { name: 'Choose inventory', exact: true }).click();
  await expect(page.getByText(/Enter a valid Disk ID first/)).toBeVisible();
  expect(state.options()).toHaveLength(0);
  await page.getByLabel('Disk ID', { exact: true }).fill('1');
  const options = page.getByRole('region', { name: 'Inventory options', exact: true });
  await expect(options.getByRole('button', { name: 'Use inventory 2' })).toBeVisible();
  await expect(options.locator('time').first()).toHaveAttribute(
    'datetime',
    '1970-01-01T00:00:10.000000123Z',
  );
  await options.getByRole('button', { name: 'Use inventory 2' }).click();
  await expect(page.getByText('Older inventory', { exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Scope', exact: true })).toHaveValue('current');
  await expect(page.getByLabel('Snapshot ID', { exact: true })).toHaveValue('2');
  expect(state.results()).toHaveLength(1);
  await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('history');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect.poll(() => state.results().length).toBe(2);
  expect(state.results()[1]!.searchParams.get('snapshot_id')).toBe('2');
  expect(state.results()[1]!.searchParams.get('scope')).toBe('history');
  expect(state.options()).toHaveLength(1);
  expect(state.unexpected).toEqual([]);
});

test('contents uses disk labels only, preserves root draft and corrects only malformed membership bookmarks', async ({
  page,
}) => {
  const state = await setup(page);
  await page.goto('/contents?disk_id=not-an-id&directory=');
  await expect(page.getByLabel('Disk ID', { exact: true })).toHaveValue('not-an-id');
  await expect(page.getByText(/Bookmarked draft \(not applied\)/)).toBeVisible();
  await expect(page.getByLabel('Filter directory membership')).toBeChecked();
  await expect(page.getByLabel('Directory', { exact: true })).toHaveValue('');
  expect(state.results()).toHaveLength(0);
  expect(state.options()).toHaveLength(0);
  await page.getByRole('button', { name: 'Choose disk', exact: true }).click();
  await page.getByRole('button', { name: 'Use disk 1', exact: true }).click();
  expect(state.results()).toHaveLength(0);
  await expect(page.getByRole('button', { name: 'Choose inventory' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect.poll(() => state.results().length).toBe(1);
  expect(state.results()[0]!.searchParams.get('disk_id')).toBe('1');
  expect(state.results()[0]!.searchParams.get('directory')).toBe('');
  await page.goto('/contents?disk_id=bad&scope=history&other_replicas=0');
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByLabel('Disk ID', { exact: true })).toHaveCount(0);
  expect(state.results()).toHaveLength(1);
  expect(state.unexpected).toEqual([]);
});

test('contents opening leaves applied rows intact; selected disk is an unapplied ID draft', async ({
  page,
}) => {
  const state = await setup(page);
  await page.goto('/contents?disk_id=9007199254740993');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  expect(state.options()).toHaveLength(0);
  await page.getByRole('button', { name: 'Choose disk', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Use disk 1' })).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.getByText(/Unapplied changes/)).toHaveCount(0);
  await page.getByRole('button', { name: 'Use disk 1' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await expect(page.getByText(/Unapplied changes/)).toBeVisible();
  expect(state.results()).toHaveLength(1);
  await page.getByRole('button', { name: 'Close disk' }).click();
  expect(state.results()).toHaveLength(1);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page).toHaveURL(/disk_id=1/);
  expect(state.results()).toHaveLength(2);
  expect(state.unexpected).toEqual([]);
});

test('continuation failure keeps unverified rows; stale cursor clears only picker options', async ({
  page,
}) => {
  let mode = 'first';
  const state = await setup(page, (r, url) => {
    if (url.searchParams.has('cursor'))
      return r.fulfill({
        status: mode === 'failure' ? 500 : 409,
        json: {
          error: {
            code: mode === 'failure' ? 'internal_error' : 'stale_cursor',
            message: 'changed',
          },
        },
      });
    return r.fulfill({ json: { revision, items: [disk('1')], next_cursor: 'next' } });
  });
  await page.goto('/contents?disk_id=99');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: 'Choose disk' }).click();
  const options = page.getByRole('region', { name: 'Disk options', exact: true });
  await expect(options.getByRole('button', { name: 'Use disk 1' })).toBeVisible();
  mode = 'failure';
  await options.getByRole('button', { name: 'Load more options' }).click();
  await expect(options.getByRole('alert')).toContainText('not freshly verified');
  await expect(options.getByRole('button', { name: 'Use disk 1' })).toBeDisabled();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  mode = 'stale';
  await options.getByRole('button', { name: 'Retry options' }).click();
  await expect(options.getByRole('alert')).toContainText('Old cursors are retired');
  await expect(options.getByRole('button', { name: 'Use disk 1' })).toHaveCount(0);
  await expect(page.getByLabel('Disk ID', { exact: true })).toHaveValue('99');
  await options.getByRole('button', { name: 'Reload options' }).click();
  await expect(options.getByRole('button', { name: 'Use disk 1' })).toBeVisible();
  expect(state.options().map((u) => u.searchParams.get('cursor'))).toEqual([
    null,
    'next',
    'next',
    null,
  ]);
  expect(state.results()).toHaveLength(1);
  expect(state.unexpected).toEqual([]);
});

test('changing disk retires delayed inventory reads without clearing the direct snapshot draft', async ({
  page,
}) => {
  let pending: Route | undefined;
  const state = await setup(page, (r, url) => {
    if (url.pathname.includes('/1/snapshots')) {
      pending = r;
      return;
    }
    return r.fulfill({
      json: { disk_id: '2', revision, items: [inventory('8', '2')], next_cursor: null },
    });
  });
  await page.goto('/?q=archive&disk_id=1&snapshot_id=99');
  await page.getByRole('button', { name: 'Choose inventory' }).click();
  await expect(page.getByText('Loading inventory options…')).toBeVisible();
  await expect.poll(() => Boolean(pending)).toBe(true);
  await page.getByLabel('Disk ID', { exact: true }).fill('2');
  await expect(page.getByRole('button', { name: 'Use inventory 8' })).toBeVisible();
  await pending!.fulfill({
    json: { disk_id: '1', revision, items: [inventory('3')], next_cursor: null },
  });
  await expect(page.getByRole('button', { name: 'Use inventory 3' })).toHaveCount(0);
  await expect(page.getByLabel('Snapshot ID', { exact: true })).toHaveValue('99');
  expect(state.results()).toHaveLength(1);
  expect(state.unexpected).toEqual([]);
});

test('empty and missing inventory lists are distinct, and direct IDs remain editable', async ({
  page,
}) => {
  let missing = false;
  const state = await setup(page, (r) =>
    missing
      ? r.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'disk missing' } } })
      : r.fulfill({ json: { disk_id: '1', revision, items: [], next_cursor: null } }),
  );
  await page.goto('/?q=archive&disk_id=1&snapshot_id=99');
  await page.getByRole('button', { name: 'Choose inventory' }).click();
  const options = page.getByRole('region', { name: 'Inventory options', exact: true });
  await expect(options.getByText('No complete inventories for this disk.')).toBeVisible();
  missing = true;
  await options.getByRole('button', { name: 'Reload options' }).click();
  await expect(options.getByRole('alert')).toContainText('not found');
  await expect(options.getByText('No complete inventories for this disk.')).toHaveCount(0);
  await expect(page.getByLabel('Snapshot ID', { exact: true })).toHaveValue('99');
  await page.getByLabel('Snapshot ID', { exact: true }).fill('9007199254740993');
  await expect(page.getByLabel('Snapshot ID', { exact: true })).toHaveValue('9007199254740993');
  expect(state.results()).toHaveLength(1);
  expect(state.unexpected).toEqual([]);
});

test('picker paging is explicitly bounded and later options use retained-page navigation', async ({
  page,
}) => {
  const state = await setup(page, (r, url) => {
    const n = Number(url.searchParams.get('cursor') ?? '1');
    return r.fulfill({
      json: { revision, items: [disk(String(n))], next_cursor: n === 12 ? null : String(n + 1) },
    });
  });
  await page.goto('/contents?disk_id=99');
  await page.getByRole('button', { name: 'Choose disk' }).click();
  const options = page.getByRole('region', { name: 'Disk options', exact: true });
  await expect(options.getByRole('button', { name: 'Use disk 1', exact: true })).toBeVisible();
  expect(state.options()).toHaveLength(1);
  for (let n = 2; n <= 10; n++) {
    await options.getByRole('button', { name: 'Load more options' }).click();
    await expect(options.getByRole('button', { name: `Use disk ${n}`, exact: true })).toBeVisible();
  }
  await expect(options.locator('li')).toHaveCount(10);
  expect(state.options()).toHaveLength(10);
  for (const n of [11, 12]) {
    await options.getByRole('button', { name: 'Next options', exact: true }).click();
    await expect(options.getByRole('button', { name: `Use disk ${n}`, exact: true })).toBeVisible();
    await expect(options.locator('li')).toHaveCount(1);
  }
  await expect(options.getByRole('button', { name: 'Next options' })).toBeDisabled();
  await options.getByRole('button', { name: 'Previous options' }).click();
  await expect(options.getByRole('button', { name: 'Use disk 11', exact: true })).toBeVisible();
  expect(state.options()).toHaveLength(12);
  expect(state.results()).toHaveLength(1);
  expect(state.unexpected).toEqual([]);
});

test('reconnect keeps disk and snapshot drafts but never resumes sibling options or old cursors', async ({
  page,
}) => {
  await page.clock.install();
  await page.clock.pauseAt(new Date());
  let unavailable = false;
  const state = await setup(page, (r, url) => {
    if (url.pathname.endsWith('/snapshots'))
      return r.fulfill({
        json: {
          disk_id: '1',
          revision,
          items: [inventory('3')],
          next_cursor: 'inventory-next',
        },
      });
    if (unavailable)
      return r.fulfill({
        status: 503,
        json: { error: { code: 'unavailable', message: 'offline' } },
      });
    return r.fulfill({ json: { revision, items: [disk('1')], next_cursor: 'disk-next' } });
  });
  await page.goto('/?q=archive&disk_id=1&snapshot_id=99');
  await page.getByRole('button', { name: 'Choose disk', exact: true }).click();
  await page.getByRole('button', { name: 'Choose inventory', exact: true }).click();
  const disks = page.getByRole('region', { name: 'Disk options', exact: true });
  const inventories = page.getByRole('region', { name: 'Inventory options', exact: true });
  await expect(disks.getByRole('button', { name: 'Use disk 1' })).toBeVisible();
  await expect(inventories.getByRole('button', { name: 'Use inventory 3' })).toBeVisible();
  await page.getByLabel('Snapshot ID', { exact: true }).fill('88');
  unavailable = true;
  await disks.getByRole('button', { name: 'Load more options' }).click();
  await expect(page.getByRole('button', { name: 'Retry connection' })).toBeVisible();
  await expect(inventories.getByRole('button', { name: 'Use inventory 3' })).toBeDisabled();
  unavailable = false;
  await page.getByRole('button', { name: 'Retry connection' }).click();
  await expect(page.locator('.shell-connection')).toHaveText('Backend ready');
  await expect(disks.getByText(/Options invalidated/)).toBeVisible();
  await expect(inventories.getByText(/Options invalidated/)).toBeVisible();
  expect(state.options()).toHaveLength(3);
  expect(state.results()).toHaveLength(1);
  await expect(page.getByLabel('Disk ID', { exact: true })).toHaveValue('1');
  await expect(page.getByLabel('Snapshot ID', { exact: true })).toHaveValue('88');
  await expect(page.getByText(/Unapplied changes/)).toBeVisible();
  await disks.getByRole('button', { name: 'Reload options' }).click();
  await expect(disks.getByRole('button', { name: 'Use disk 1' })).toBeVisible();
  expect(state.options()).toHaveLength(4);
  expect(state.options()[3]!.searchParams.has('cursor')).toBe(false);
  await expect(inventories.getByText(/Options invalidated/)).toBeVisible();
  expect(state.results()).toHaveLength(1);
  expect(state.unexpected).toEqual([]);
});

test('open label and capture pickers wrap at approved desktop widths with external evidence', async ({
  page,
}) => {
  const state = await setup(page);
  const root = '/tmp/opencode/u09-screenshots';
  await mkdir(root, { recursive: true });
  const measurements = [];
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/?q=archive&disk_id=1&snapshot_id=2&scope=history');
    await page.getByRole('button', { name: 'Choose disk' }).click();
    await page.getByRole('button', { name: 'Choose inventory' }).click();
    await expect(page.getByRole('button', { name: 'Use inventory 2' })).toBeVisible();
    const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(documentWidth).toBe(width);
    await page.screenshot({ path: `${root}/search-${width}.png`, fullPage: true });
    measurements.push({ scenario: 'search', width, documentWidth });
    await page.goto('/contents?disk_id=99&directory=');
    await page.getByRole('button', { name: 'Choose disk' }).click();
    await expect(page.getByRole('button', { name: 'Use disk 1' })).toBeVisible();
    const contentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(contentWidth).toBe(width);
    await page.screenshot({ path: `${root}/contents-${width}.png`, fullPage: true });
    measurements.push({ scenario: 'contents', width, documentWidth: contentWidth });
  }
  await writeFile(`${root}/measurements.json`, JSON.stringify(measurements, null, 2));
  expect(state.unexpected).toEqual([]);
});
