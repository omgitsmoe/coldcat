import { expect, test, type Page, type Route } from '@playwright/test';
import { searchFixture } from '../fixtures/api';
import type { components } from '../../src/lib/api/generated/wire';

function fixture(url: URL): components['schemas']['DirectoryCoveragePage'] {
  const snapshot = searchFixture().items[0]!.snapshot;
  const empty = url.searchParams.getAll('block').includes('**');
  const number = Number(url.searchParams.get('cursor') ?? '1');
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
      retained_file_count: empty ? '0' : '9007199254740993',
      excluded_file_count: '1',
      content_count: empty ? '0' : '2',
      known_bytes: empty ? '0' : '9007199254740993',
      unknown_size_file_count: empty ? '0' : '1',
      size_complete: empty,
      empty_comparison: empty,
    },
    items: empty
      ? []
      : [
          {
            disk: {
              ...searchFixture().items[0]!.disk,
              id: String(number + 1),
              label: `partial-${number}`,
            },
            snapshot: { ...snapshot, id: String(number + 1), disk_id: String(number + 1) },
            covered_file_count: '9007199254740992',
            missing_file_count: '1',
            covered_content_count: '1',
            missing_content_count: '1',
            covered_known_bytes: '9007199254740993',
            missing_known_bytes: '0',
            covered_unknown_size_file_count: '0',
            missing_unknown_size_file_count: '1',
            complete: false,
          },
        ],
    next_cursor: empty || number === 12 ? null : String(number + 1),
  };
}

async function setup(page: Page, delay?: (route: Route) => Promise<void>) {
  const calls: URL[] = [];
  await page.route('**/healthz', (r) => r.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (r) =>
    r.fulfill({
      json: {
        revision: '1',
        scope: 'current',
        disk_count: '3',
        file_count: '3',
        content_count: '2',
      },
    }),
  );
  await page.route('**/api/v1/snapshots/1/directory**', async (r) => {
    const url = new URL(r.request().url());
    if (url.pathname.endsWith('/coverage')) {
      calls.push(url);
      if (delay && calls.length === 1) return delay(r);
      return r.fulfill({ json: fixture(url) });
    }
    return r.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Not found' } } });
  });
  await page.goto('/snapshots/1/directory?path=');
  await page.getByRole('button', { name: 'Content on other disks', exact: true }).click();
  return calls;
}

test('explicit coverage, literal rules, safe large counts, partial status and empty selection', async ({
  page,
}) => {
  const calls = await setup(page);
  const coverage = page.getByRole('region', { name: 'Content on other disks' });
  expect(calls).toHaveLength(0);
  await page.getByRole('button', { name: 'Add allow rule' }).click();
  await page.getByLabel('Allow pattern 1', { exact: true }).fill(' **/a\\?.txt ');
  await page.getByRole('button', { name: 'Add allow rule' }).click();
  await page.getByLabel('Allow pattern 2', { exact: true }).fill('雪/{a,b}');
  expect(calls).toHaveLength(0);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(coverage.getByText('Partial content coverage', { exact: true })).toBeVisible();
  await expect(coverage).toContainText(
    'Historical source inventory compared against current destination inventories.',
  );
  await expect(coverage).toContainText('9,007,199,254,740,993');
  await expect(coverage).toContainText('Covered files: 9,007,199,254,740,992; missing files: 1');
  await expect(coverage).toContainText(
    'Covered distinct contents: 1; missing distinct contents: 1',
  );
  await expect(coverage).toContainText('Known covered source bytes: 9,007,199,254,740,993 B');
  await expect(coverage).toContainText(
    'Covered unknown-size files: 0; missing unknown-size files: 1',
  );
  await expect(coverage).toContainText('Source byte total: Unknown');
  await expect(coverage).toContainText('50.0% of selected distinct contents (rounded)');
  await expect(coverage).toContainText('Distributed coverage is not one complete directory backup');
  await expect(coverage.getByRole('link', { name: 'partial-1 — Disk 2' })).toHaveAttribute(
    'href',
    '/disks/2',
  );
  expect(calls[0]!.searchParams.getAll('allow')).toEqual([' **/a\\?.txt ', '雪/{a,b}']);
  await page.getByRole('button', { name: 'Add block rule' }).click();
  await page.getByLabel('Block pattern 1', { exact: true }).fill('**');
  await expect(coverage.getByText('Partial content coverage', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(coverage.getByText('No files selected', { exact: true })).toBeVisible();
  await expect(coverage).not.toContainText('100%');
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
});

test('percentage uses large selected-content denominator and complete coverage is not tree equality', async ({
  page,
}) => {
  await setup(page);
  await page.route('**/api/v1/snapshots/1/directory/coverage?**', (route) => {
    const value = fixture(new URL(route.request().url()));
    value.selection.content_count = '18014398509481986';
    value.items[0]!.covered_content_count = '9007199254740993';
    value.items[0]!.missing_content_count = '9007199254740993';
    return route.fulfill({ json: value });
  });
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  const coverage = page.getByRole('region', { name: 'Content on other disks' });
  await expect(coverage).toContainText('50.0% of selected distinct contents (rounded)');
  await expect(coverage).toContainText('Selected distinct contents: 18,014,398,509,481,986');
  await page.route('**/api/v1/snapshots/1/directory/coverage?**', (route) => {
    const value = fixture(new URL(route.request().url()));
    value.selection = {
      retained_file_count: '1',
      excluded_file_count: '0',
      content_count: '1',
      known_bytes: '0',
      unknown_size_file_count: '0',
      size_complete: true,
      empty_comparison: false,
    };
    value.items[0] = {
      ...value.items[0]!,
      covered_file_count: '1',
      missing_file_count: '0',
      covered_content_count: '1',
      missing_content_count: '0',
      covered_known_bytes: '0',
      missing_known_bytes: '0',
      covered_unknown_size_file_count: '0',
      missing_unknown_size_file_count: '0',
      complete: true,
    };
    value.next_cursor = null;
    return route.fulfill({ json: value });
  });
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(coverage.getByText('Complete content coverage', { exact: true })).toBeVisible();
  await expect(coverage).toContainText('Source byte total: 0 B');
  await expect(coverage).toContainText('100.0% of selected distinct contents (rounded)');
  await expect(coverage).toContainText('does not mean an exact tree copy');
});

test('bounded coverage pages stay reachable without automatic loading', async ({ page }) => {
  const calls = await setup(page);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toBeVisible();
  expect(calls).toHaveLength(1);
  for (let n = 2; n <= 10; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.getByRole('link', { name: `partial-${n} — Disk ${n + 1}` })).toBeVisible();
  }
  await expect(page.getByRole('button', { name: 'Load more', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByRole('link', { name: 'partial-11 — Disk 12' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByRole('link', { name: 'partial-12 — Disk 13' })).toBeVisible();
  await page.getByRole('button', { name: 'Previous page', exact: true }).click();
  await expect(page.getByRole('link', { name: 'partial-11 — Disk 12' })).toBeVisible();
  expect(calls).toHaveLength(12);
});

for (const action of ['cancel', 'draft', 'leave'] as const) {
  test(`delayed coverage keeps controls responsive and ${action} rejects obsolete results`, async ({
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
    if (action === 'cancel') await page.getByRole('button', { name: 'Cancel comparison' }).click();
    if (action === 'draft') {
      await page.getByRole('button', { name: 'Add allow rule' }).click();
      await page.getByLabel('Allow pattern 1', { exact: true }).fill(' next ');
    }
    if (action === 'leave') {
      await page.getByRole('button', { name: 'Entries', exact: true }).click();
      await page.getByRole('button', { name: 'Content on other disks', exact: true }).click();
    }
    release();
    await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toHaveCount(0);
    expect(calls).toHaveLength(1);
    await page.getByRole('button', { name: 'Apply and compare' }).click();
    await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toBeVisible();
    expect(calls[1]!.searchParams.has('cursor')).toBe(false);
  });
}

test('draft edits cancel pending continuation and discard its cursor', async ({ page }) => {
  const calls = await setup(page);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toBeVisible();
  let release!: () => void;
  const wait = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/api/v1/snapshots/1/directory/coverage?**', async (route) => {
    const url = new URL(route.request().url());
    if (!url.searchParams.has('cursor')) return route.fallback();
    await wait;
    await route.fulfill({ json: fixture(url) }).catch(() => {});
  });
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Cancel comparison' })).toBeVisible();
  await page.getByLabel('Comparison page size').fill('1');
  release();
  await expect(page.getByRole('link', { name: 'partial-2 — Disk 3' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
  expect(calls.at(-1)!.searchParams.get('limit')).toBe('1');
});

test('failed continuation retains rows; stale cursor clears them and requires restart', async ({
  page,
}) => {
  const calls = await setup(page);
  await page.getByRole('button', { name: 'Apply and compare' }).click();
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toBeVisible();
  let status = 500;
  await page.route('**/api/v1/snapshots/1/directory/coverage?**', (route) => {
    if (!new URL(route.request().url()).searchParams.has('cursor')) return route.fallback();
    return route.fulfill({
      status,
      json: {
        error: {
          code: status === 409 ? 'stale_cursor' : 'internal_error',
          message: 'Coverage page failed',
        },
      },
    });
  });
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByText('Previously loaded coverage; not freshly verified.')).toBeVisible();
  status = 409;
  await page
    .getByRole('region', { name: 'Content on other disks' })
    .getByRole('button', { name: 'Retry', exact: true })
    .click();
  await expect(page.getByText('Inventory changed; reload results', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.getByRole('link', { name: 'partial-1 — Disk 2' })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
});
