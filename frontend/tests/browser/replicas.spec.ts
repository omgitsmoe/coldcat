import { expect, test, type Page, type Route } from '@playwright/test';
import { searchFixture } from '../fixtures/api';
import type { components } from '../../src/lib/api/generated/wire';

function fixture(url: URL): components['schemas']['DirectoryReplicaPage'] {
  const snapshot = searchFixture().items[0]!.snapshot;
  const empty = url.searchParams.getAll('block').includes('**');
  return {
    snapshot,
    revision: '1',
    is_current: false,
    replica_scope: 'current',
    filters: {
      path: '',
      allow: url.searchParams.getAll('allow'),
      block: url.searchParams.getAll('block'),
    },
    selection: {
      retained_file_count: empty ? '0' : '2',
      excluded_file_count: '1',
      content_count: '2',
      known_bytes: '8',
      unknown_size_file_count: '0',
      size_complete: true,
      empty_comparison: empty,
    },
    items: empty
      ? []
      : [
          {
            disk: searchFixture().items[0]!.disk,
            snapshot,
            path: url.searchParams.has('cursor') ? 'later' : 'renamed',
            same_disk: true,
            whole_tree_equal: url.searchParams.has('cursor'),
            retained_file_count: '2',
            excluded_file_count: '1',
          },
        ],
    next_cursor: empty || url.searchParams.has('cursor') ? null : 'next',
  };
}
async function setup(page: Page, delay?: (route: Route) => Promise<void>, pages = 2) {
  const calls: URL[] = [];
  await page.route('**/healthz', (r) => r.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (r) =>
    r.fulfill({
      json: {
        revision: '1',
        scope: 'current',
        disk_count: '1',
        file_count: '3',
        content_count: '2',
      },
    }),
  );
  await page.route('**/api/v1/snapshots/1/directory**', async (r) => {
    const url = new URL(r.request().url());
    if (url.pathname.endsWith('/replicas')) {
      calls.push(url);
      if (delay && calls.length === 1) return delay(r);
      const result = fixture(url);
      if (pages > 2) {
        const number = Number(url.searchParams.get('cursor') ?? '1');
        result.items[0]!.path = `copy-${number}`;
        result.next_cursor = number < pages ? String(number + 1) : null;
      }
      return r.fulfill({ json: result });
    }
    return r.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Not found' } } });
  });
  await page.goto('/snapshots/1/directory?path=');
  await page.getByRole('button', { name: 'Exact tree copies', exact: true }).click();
  return calls;
}
test('no preload; literal repeated rules, current destinations, equality and empty selection', async ({
  page,
}) => {
  const calls = await setup(page);
  expect(calls).toHaveLength(0);
  await page.getByRole('button', { name: 'Add allow rule' }).click();
  await page.getByLabel('Allow pattern 1', { exact: true }).fill(' **/a\\?.txt ');
  await page.getByRole('button', { name: 'Add allow rule' }).click();
  await page.getByLabel('Allow pattern 2', { exact: true }).fill('雪/{a,b}');
  expect(calls).toHaveLength(0);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByText('Equal under these filters', { exact: true })).toBeVisible();
  await expect(page.getByText('Same disk', { exact: true })).toBeVisible();
  expect(calls[0]!.searchParams.getAll('allow')).toEqual([' **/a\\?.txt ', '雪/{a,b}']);
  await expect(
    page.getByText(
      'Historical source inventory compared against current destination inventories.',
      { exact: true },
    ),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByText('Whole tree equal', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add block rule' }).click();
  await page.getByLabel('Block pattern 1', { exact: true }).fill('**');
  await expect(page.getByText('renamed', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByText('No files selected', { exact: true })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
});
test('bounded copies remain reachable without automatic continuation', async ({ page }) => {
  const calls = await setup(page, undefined, 12);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByText('copy-1', { exact: true })).toBeVisible();
  expect(calls).toHaveLength(1);
  for (let n = 2; n <= 10; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.getByText(`copy-${n}`, { exact: true })).toBeVisible();
  }
  await expect(page.getByRole('button', { name: 'Load more', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('copy-11', { exact: true })).toBeVisible();
  await expect(page.getByText('copy-1', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('copy-12', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Previous page', exact: true }).click();
  await expect(page.getByText('copy-11', { exact: true })).toBeVisible();
  expect(calls).toHaveLength(12);
});
test('invalid rows do not dispatch and cancel keeps editing available', async ({ page }) => {
  let release!: () => void;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  const calls = await setup(page, async (r) => {
    await wait;
    await r.fulfill({ json: fixture(new URL(r.request().url())) }).catch(() => {});
  });
  await page.getByRole('button', { name: 'Add allow rule' }).click();
  await expect(page.getByRole('button', { name: 'Apply and compare' })).toBeDisabled();
  await page.getByLabel('Allow pattern 1', { exact: true }).fill('雪'.repeat(342));
  await expect(page.getByRole('button', { name: 'Apply and compare' })).toBeDisabled();
  expect(calls).toHaveLength(0);
  await page.getByLabel('Allow pattern 1', { exact: true }).fill('**');
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await page.getByRole('button', { name: 'Cancel comparison' }).click();
  release();
  await expect(page.getByText('Rules changed; apply to compare.', { exact: true })).toBeVisible();
  await expect(page.getByText('renamed', { exact: true })).toHaveCount(0);
  await page.getByLabel('Allow pattern 1', { exact: true }).fill(' next ');
  expect(calls).toHaveLength(1);
});
test('changing rules during continuation clears cursors and late pages', async ({ page }) => {
  const calls = await setup(page);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByText('renamed', { exact: true })).toBeVisible();
  let release!: () => void;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/api/v1/snapshots/1/directory/replicas?**', async (route) => {
    const url = new URL(route.request().url());
    if (!url.searchParams.has('cursor')) return route.fallback();
    await wait;
    await route.fulfill({ json: fixture(url) }).catch(() => {});
  });
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Cancel comparison' })).toBeVisible();
  await page.getByRole('button', { name: 'Add block rule' }).click();
  await page.getByLabel('Block pattern 1', { exact: true }).fill('**');
  release();
  await expect(page.getByText('later', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByText('No files selected', { exact: true })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
});
test('leaving the comparison view retires pending work without preloading on return', async ({
  page,
}) => {
  let release!: () => void;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  const calls = await setup(page, async (route) => {
    await wait;
    await route.fulfill({ json: fixture(new URL(route.request().url())) }).catch(() => {});
  });
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByRole('button', { name: 'Cancel comparison' })).toBeVisible();
  await page.getByRole('button', { name: 'Entries', exact: true }).click();
  release();
  await page.getByRole('button', { name: 'Exact tree copies', exact: true }).click();
  await expect(
    page.getByText('Apply rules to run a comparison. No comparison has been requested.'),
  ).toBeVisible();
  await expect(page.getByText('renamed', { exact: true })).toHaveCount(0);
  expect(calls).toHaveLength(1);
});
test('draft edits cancel delayed work and obsolete responses never restore results', async ({
  page,
}) => {
  let release!: () => void;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  const calls = await setup(page, async (r) => {
    await wait;
    await r.fulfill({ json: fixture(new URL(r.request().url())) }).catch(() => {});
  });
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByRole('button', { name: 'Cancel comparison' })).toBeVisible();
  await page.getByRole('button', { name: 'Add allow rule' }).click();
  await page.getByLabel('Allow pattern 1', { exact: true }).fill('new');
  release();
  await expect(page.getByText('Rules changed; apply to compare.', { exact: true })).toBeVisible();
  await expect(page.getByText('renamed', { exact: true })).toHaveCount(0);
  expect(calls).toHaveLength(1);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByText('renamed', { exact: true })).toBeVisible();
  expect(calls[1]!.searchParams.has('cursor')).toBe(false);
});

test('page errors retain copies, stale cursors clear them and restart explicitly', async ({
  page,
}) => {
  const calls = await setup(page);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByText('renamed', { exact: true })).toBeVisible();
  let status = 500;
  await page.route('**/api/v1/snapshots/1/directory/replicas?**', (route) => {
    if (!new URL(route.request().url()).searchParams.has('cursor')) return route.fallback();
    return route.fulfill({
      status,
      json: {
        error: {
          code: status === 409 ? 'stale_cursor' : 'internal_error',
          message: 'Comparison page failed',
        },
      },
    });
  });
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByText('Previously loaded copies; not freshly verified.')).toBeVisible();
  await expect(page.getByText('renamed', { exact: true })).toBeVisible();
  status = 409;
  await page
    .getByRole('region', { name: 'Exact tree copies' })
    .getByRole('button', { name: 'Retry', exact: true })
    .click();
  await expect(page.getByText('Inventory changed; reload results', { exact: true })).toBeVisible();
  await expect(page.getByText('renamed', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.getByText('renamed', { exact: true })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
});
