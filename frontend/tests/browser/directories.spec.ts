import { expect, test, type Page } from '@playwright/test';
import type { components } from '../../src/lib/api/generated/wire';
import { searchFixture } from '../fixtures/api';
import { contentFixture, locationsFixture, observationFixture } from '../fixtures/details';

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
  await expect(page.getByRole('region', { name: 'Directory summary' })).toContainText(
    'Historical source inventory',
  );
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText(
    '9,007,199,254,740,993 B',
  );
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText('4 B');
  await expect(page.getByRole('region', { name: 'Recursive sizes' })).toContainText('Unknown');
  await expect(page.getByRole('region', { name: 'Current other-disk redundancy' })).toContainText(
    '2 other disks: 2 files',
  );
  await page.getByLabel('Exact other replicas').fill('0');
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
  await expect(page.getByLabel('Exact other replicas')).toHaveValue('0');
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
  await page.getByLabel('Exact other replicas').fill('1');
  await expect(page.getByRole('table', { name: 'Directory entries' })).toHaveCount(0);
  await page.getByLabel('Exact other replicas').fill('');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('table', { name: 'Directory entries' })).toBeVisible();
});
