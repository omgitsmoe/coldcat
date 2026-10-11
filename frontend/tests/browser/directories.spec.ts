import { expect, test, type Page } from '@playwright/test';
import type { components } from '../../src/lib/api/generated/wire';
import { searchFixture } from '../fixtures/api';
import { contentFixture, locationsFixture, observationFixture } from '../fixtures/details';
import { mkdir, writeFile } from 'node:fs/promises';

function summary(path: string): components['schemas']['DirectorySummary'] {
  return {
    path,
    file_count: '3',
    content_count: '2',
    known_bytes: '9007199254740993',
    unknown_size_file_count: '1',
    size_complete: false,
    unique_content_known_bytes: '4',
    unknown_size_content_count: '1',
    unique_content_size_complete: false,
    max_known_mtime: null,
  };
}

async function mock(page: Page, failure: 'summary' | 'entries' | null = null) {
  const calls: URL[] = [];
  await page.route('**/healthz', (r) => r.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (r) =>
    r.fulfill({
      json: {
        revision: '9007199254740993',
        scope: 'current',
        disk_count: '3',
        file_count: '3',
        content_count: '2',
      },
    }),
  );
  await page.route('**/api/v1/snapshots/1/directory**', (r) => {
    const url = new URL(r.request().url());
    calls.push(url);
    const entries = url.pathname.endsWith('/entries');
    if (failure === (entries ? 'entries' : 'summary'))
      return r.fulfill({
        status: 500,
        json: { error: { code: 'internal_error', message: 'Section failed' } },
      });
    const path = url.searchParams.get('path')!;
    const base = {
      snapshot: { ...searchFixture().items[0]!.snapshot, id: '1', disk_id: '2' },
      revision: '9007199254740993',
      is_current: false,
      replica_scope: 'current' as const,
    };
    if (!entries)
      return r.fulfill({
        json: {
          ...base,
          directory: summary(path),
          redundancy_histogram: [
            { other_disk_count: '0', file_count: '1' },
            { other_disk_count: '2', file_count: '2' },
          ],
        } satisfies components['schemas']['DirectoryDetail'],
      });
    const prefix = path ? `${path}/` : '';
    const continued = url.searchParams.has('cursor');
    const filePath = prefix + (continued ? 'zero' : 'unknown');
    const file: components['schemas']['DirectoryEntry'] = {
      path: filePath,
      kind: 'file',
      directory: null,
      file: {
        observation: {
          id: continued ? '4' : '3',
          snapshot_id: '1',
          content_id: '1',
          path: filePath,
          mtime: null,
        },
        hash: { algorithm: 'sha256', hex: 'ab'.repeat(32) },
        size: continued ? '0' : null,
        other_disk_count: '0',
        other_location_count: '2',
      },
    };
    return r.fulfill({
      json: {
        ...base,
        filters: {
          path,
          directories_only: false,
          recursive: url.searchParams.get('recursive') === 'true',
          replica_metric: 'disks',
          other_replicas: url.searchParams.get('other_replicas'),
          min_other_replicas: null,
          max_other_replicas: null,
        },
        items:
          continued || url.searchParams.get('recursive') === 'true'
            ? [file]
            : [
                {
                  path: prefix + 'child',
                  kind: 'directory',
                  directory: summary(prefix + 'child'),
                  file: null,
                },
                file,
              ],
        next_cursor: continued ? null : 'next',
      } satisfies components['schemas']['DirectoryPage'],
    });
  });
  return calls;
}

test('literal slash breadcrumbs preserve root, case, Unicode, backslash and segment boundaries', async ({
  page,
}) => {
  const calls = await mock(page);
  const path = 'foo/É e\u0301\\x ?#%';
  await page.goto(`/snapshots/1/directory?${new URLSearchParams({ path })}`);
  const crumbs = page.getByRole('navigation', { name: 'Directory breadcrumbs' });
  await expect(crumbs.getByRole('link', { name: 'Root — Disk 2' })).toBeVisible();
  await expect(crumbs.getByRole('link', { name: 'foo', exact: true })).toHaveAttribute(
    'href',
    /path=foo(?:&|$)/,
  );
  await expect(crumbs.getByText('É e\u0301\\x ?#%', { exact: true })).toBeVisible();
  expect(calls).toHaveLength(2);
  expect(calls.every((u) => u.searchParams.get('path') === path)).toBe(true);
  await crumbs.getByRole('link', { name: 'foo', exact: true }).click();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  expect(calls.slice(-2).every((u) => u.searchParams.get('path') === 'foo')).toBe(true);
  await page.reload();
  await expect(crumbs.getByText('foo', { exact: true })).toBeVisible();
  await crumbs.getByRole('link', { name: 'Root — Disk 2' }).click();
  await expect(page.getByRole('link', { name: 'child', exact: true })).toBeVisible();
  expect(calls.slice(-2).every((u) => u.searchParams.get('path') === '')).toBe(true);
});

test('historical source uses current replicas; sizes, histogram, filters, directory rows and return links', async ({
  page,
}) => {
  const calls = await mock(page);
  await page.goto('/snapshots/1/directory?path=foo');
  await expect(page.locator('.source-context')).toContainText('Historical source inventory');
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText(
    '9,007,199,254,740,993 B',
  );
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText('4 B');
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText('Unknown');
  await page.getByText('Detailed statistics', { exact: true }).click();
  await expect(page.getByRole('region', { name: 'Current other-disk redundancy' })).toContainText(
    '2 other disks: 2 files',
  );
  await page.getByRole('combobox', { name: 'Other-copy matching' }).selectOption('exact');
  await page.getByLabel('Exact other copies').fill('0');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  expect(calls.at(-1)!.searchParams.get('other_replicas')).toBe('0');
  const link = page.getByRole('link', { name: 'Content 1', exact: true }).first();
  const href = new URL((await link.getAttribute('href'))!, 'http://local');
  expect(href.searchParams.get('return_to')).toContain('/snapshots/1/directory?');
  expect(href.searchParams.get('observation')).toBe('3');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('table', { name: 'Directory entries' })).toContainText('0 B');
  expect(calls.at(-1)!.searchParams.get('cursor')).toBe('next');
  expect(calls.at(-1)!.searchParams.get('other_replicas')).toBe('0');
  await page.getByLabel('Recursive files').check();
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('table', { name: 'Directory entries' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toHaveCount(0);
  expect(calls.at(-1)!.searchParams.get('recursive')).toBe('true');
  expect(calls.at(-1)!.searchParams.has('cursor')).toBe(false);
});

for (const failure of ['summary', 'entries'] as const) {
  test(`${failure} failure does not erase the independent successful section`, async ({ page }) => {
    await mock(page, failure);
    await page.goto('/snapshots/1/directory?path=');
    await expect(page.getByRole('alert')).toContainText('500 · internal_error');
    if (failure === 'summary')
      await expect(page.getByRole('link', { name: 'child', exact: true })).toBeVisible();
    else await expect(page.getByRole('region', { name: 'Recursive sizes' })).toBeVisible();
  });
}

test('ordinary content and observation links return to the same directory filters', async ({
  page,
}) => {
  await mock(page);
  await page.route('**/api/v1/contents/1?**', (r) => r.fulfill({ json: contentFixture() }));
  await page.route('**/api/v1/contents/1/observations?**', (r) =>
    r.fulfill({ json: locationsFixture() }),
  );
  await page.route('**/api/v1/observations/3', (r) => {
    const item = observationFixture();
    item.observation.id = '3';
    return r.fulfill({ json: item });
  });
  await page.goto('/snapshots/1/directory?path=foo&other_replicas=0');
  await page.getByRole('link', { name: 'Content 1', exact: true }).click();
  await expect(page.getByRole('link', { name: 'Return to directory' })).toBeVisible();
  await page.getByRole('link', { name: 'Return to directory' }).click();
  await expect(page.getByLabel('Exact other copies')).toHaveValue('0');
  await page.getByRole('link', { name: 'Observation 3', exact: true }).click();
  await page.getByRole('link', { name: 'Return to directory' }).click();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
});

test('continuation failure retains rows; stale reset never mixes pages and filter drafts retire entries', async ({
  page,
}) => {
  await mock(page);
  let stale = false;
  await page.route('**/api/v1/snapshots/1/directory/entries?**', (r) => {
    if (!new URL(r.request().url()).searchParams.has('cursor')) return r.fallback();
    return r.fulfill({
      status: stale ? 409 : 500,
      json: {
        error: {
          code: stale ? 'stale_cursor' : 'internal_error',
          message: stale ? 'Inventory changed' : 'Page failed',
        },
      },
    });
  });
  await page.goto('/snapshots/1/directory?path=foo');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('500 · internal_error');
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  stale = true;
  await page
    .getByRole('region', { name: 'Directory entry listing' })
    .getByRole('button', { name: 'Retry', exact: true })
    .click();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toBeVisible();
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Other-copy matching' }).selectOption('exact');
  await page.getByLabel('Exact other copies').fill('1');
  await expect(page.getByRole('table', { name: 'Directory entries' })).toHaveCount(0);
  await page.getByRole('combobox', { name: 'Other-copy matching' }).selectOption('any');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('table', { name: 'Directory entries' })).toBeVisible();
});

test('compact unfiltered summary and full deep-path records at approved desktop widths', async ({
  page,
}) => {
  const calls = await mock(page);
  const path = 'archive/É e\u0301\\x ?#%/' + 'literal-long-segment-'.repeat(8);
  await mkdir('/tmp/opencode/u11-screenshots', { recursive: true });
  const measurements = [];
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`/snapshots/1/directory?${new URLSearchParams({ path, other_replicas: '0' })}`);
    const table = page.getByRole('table', { name: 'Directory entries' });
    await expect(table).toBeVisible();
    await expect(table.getByRole('link', { name: `${path}/child`, exact: true })).toBeVisible();
    await expect(table).toContainText(`${path}/unknown`);
    const sizes = page.getByRole('region', { name: 'Recursive sizes' });
    await expect(sizes).toContainText(
      'All descendants (unfiltered): 3 files / 2 distinct contents',
    );
    await expect(sizes).toContainText('Incomplete');
    await expect(sizes).toContainText('Known file-byte subtotal');
    await expect(sizes).toContainText('1 unknown-size files');
    await expect(
      page.getByRole('region', { name: 'Current other-disk redundancy' }),
    ).not.toBeVisible();
    await expect(page.getByLabel('Exact other copies')).toHaveValue('0');
    const bounds = await table.boundingBox();
    const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(documentWidth).toBe(width);
    expect(bounds!.y).toBeLessThan(900);
    const cells = await table
      .locator('tbody tr')
      .first()
      .locator('td')
      .evaluateAll((elements) =>
        elements.map((element) => {
          const rect = element.getBoundingClientRect();
          return { top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right };
        }),
      );
    for (let n = 1; n < cells.length; n++) {
      if (width <= 900) expect(cells[n]!.top).toBeGreaterThanOrEqual(cells[n - 1]!.bottom);
      else expect(cells[n]!.left).toBeGreaterThanOrEqual(cells[n - 1]!.right);
    }
    if (width <= 900) await expect(table.locator('.narrow-label').first()).toBeVisible();
    expect(
      await table
        .locator('tbody tr')
        .first()
        .locator('td.numeric')
        .first()
        .evaluate((cell) => getComputedStyle(cell).textAlign),
    ).toBe(width <= 900 ? 'left' : 'right');
    await page.screenshot({
      path: `/tmp/opencode/u11-screenshots/entries-${width}.png`,
      fullPage: true,
    });
    measurements.push({ width, tableOrigin: bounds!.y, documentWidth });
    const before = calls.length;
    await page.getByText('Detailed statistics', { exact: true }).click();
    await expect(page.getByRole('region', { name: 'Current other-disk redundancy' })).toContainText(
      '2 other disks: 2 files',
    );
    expect(calls).toHaveLength(before);
  }
  await writeFile(
    '/tmp/opencode/u11-screenshots/measurements.json',
    JSON.stringify(measurements, null, 2),
  );
});

test('replica range validation retires entries without changing the independent summary', async ({
  page,
}) => {
  const calls = await mock(page);
  await page.goto('/snapshots/1/directory?path=foo&min_other_replicas=0&max_other_replicas=2');
  await expect(page.getByLabel('Minimum other copies')).toHaveValue('0');
  await expect(page.getByLabel('Maximum other copies')).toHaveValue('2');
  await page.getByLabel('Minimum other copies').fill('3');
  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('table', { name: 'Directory entries' })).toHaveCount(0);
  await expect(
    page.getByRole('region', { name: 'Directory entry listing' }).getByRole('status'),
  ).toContainText('Unapplied changes');
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toBeVisible();
  await page.getByRole('button', { name: 'Apply filters' }).click();
  expect(calls).toHaveLength(2);
  await page.getByRole('combobox', { name: 'Other-copy matching' }).selectOption('any');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  expect(calls.at(-1)!.searchParams.has('min_other_replicas')).toBe(false);
});

test('summary loading and retry remain independent of entry loading and retry', async ({
  page,
}) => {
  const calls = await mock(page);
  let release!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  let attempts = 0;
  await page.route('**/api/v1/snapshots/1/directory?**', async (route) => {
    if (++attempts === 1) {
      await pending;
      return route.fulfill({
        status: 500,
        json: { error: { code: 'internal_error', message: 'Summary failed' } },
      });
    }
    return route.fallback();
  });
  await page.goto('/snapshots/1/directory?path=foo');
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Directory summary' })).toContainText('Loading');
  await expect(page.locator('.source-context')).toContainText('Historical source inventory');
  release();
  const summaryRegion = page.getByRole('region', { name: 'Directory summary' });
  await expect(summaryRegion.getByRole('alert')).toBeVisible();
  await expect(summaryRegion.getByRole('button')).toHaveCount(1);
  const entryCalls = calls.filter((url) => url.pathname.endsWith('/entries')).length;
  await summaryRegion.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toBeVisible();
  expect(calls.filter((url) => url.pathname.endsWith('/entries'))).toHaveLength(entryCalls);
  const summaryCalls = attempts;
  await page.getByRole('button', { name: 'Reload entries', exact: true }).click();
  await expect(page.getByRole('link', { name: 'foo/child', exact: true })).toBeVisible();
  expect(attempts).toBe(summaryCalls);
});

test('directory traversal reaches retained-page navigation without automatic exhaustion', async ({
  page,
}) => {
  const calls = await mock(page);
  const cursors: string[] = [];
  await page.route('**/api/v1/snapshots/1/directory/entries?**', async (route) => {
    const url = new URL(route.request().url());
    const n = Number(url.searchParams.get('cursor') ?? '0');
    cursors.push(String(n));
    const response = {
      snapshot: { ...searchFixture().items[0]!.snapshot, id: '1', disk_id: '2' },
      revision: '9007199254740993',
      is_current: false,
      replica_scope: 'current' as const,
      filters: {
        path: 'foo',
        recursive: false,
        directories_only: false,
        replica_metric: 'disks' as const,
        other_replicas: null,
        min_other_replicas: null,
        max_other_replicas: null,
      },
      items: [
        {
          kind: 'directory' as const,
          path: `foo/child-${n}`,
          directory: summary(`foo/child-${n}`),
          file: null,
        },
      ],
      next_cursor: n === 11 ? null : String(n + 1),
    } satisfies components['schemas']['DirectoryPage'];
    return route.fulfill({ json: response });
  });
  await page.goto('/snapshots/1/directory?path=foo&limit=1');
  const table = page.getByRole('table', { name: 'Directory entries' });
  await expect(table).toBeVisible();
  expect(cursors).toHaveLength(1);
  for (let n = 1; n < 10; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(table.locator('tbody tr')).toHaveCount(n + 1);
  }
  for (const n of [10, 11]) {
    await page.getByRole('button', { name: 'Next page', exact: true }).click();
    await expect(table.getByRole('link', { name: `foo/child-${n}`, exact: true })).toBeVisible();
  }
  expect(cursors).toHaveLength(12);
  await page.getByRole('button', { name: 'Previous page', exact: true }).click();
  await expect(table.getByRole('link', { name: 'foo/child-10', exact: true })).toBeVisible();
  expect(cursors).toHaveLength(12);
  expect(calls.filter((url) => !url.pathname.endsWith('/entries'))).toHaveLength(1);
});
