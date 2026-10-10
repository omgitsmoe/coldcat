import { mkdir, writeFile } from 'node:fs/promises';
import { expect, test, type Page } from '@playwright/test';
import type { components } from '../../src/lib/api/generated/wire';
import { diskFixture, searchFixture } from '../fixtures/api';
import { contentFixture, locationsFixture } from '../fixtures/details';

const literalPath =
  'archive/2026/雪 e\u0301/literal ?#% <report>/very-long-directory-name-without-shortening/' +
  'nested-project-with-another-long-name/report-with-a-long-descriptive-basename.txt';
const directoryPath = literalPath.slice(0, literalPath.lastIndexOf('/'));
const revision = '9007199254740993';

function search(scope: 'current' | 'history', empty: boolean) {
  const result = searchFixture();
  result.scope = scope;
  result.items = empty
    ? []
    : ['9223372036854775807', null, '0'].map((size, index) => {
        const item = searchFixture().items[0]!;
        item.observation.id = String(index + 1);
        item.observation.content_id = String(index + 1);
        item.content.id = String(index + 1);
        item.content.hash.hex = ['ab', 'cd', 'ef'][index]!.repeat(32);
        item.snapshot.file_count = '3';
        item.snapshot.content_count = '3';
        item.observation.path = index === 0 ? literalPath : `archive/report-${index}.txt`;
        item.basename = item.observation.path.slice(item.observation.path.lastIndexOf('/') + 1);
        item.content.size = size;
        item.content.scope = scope;
        item.is_current = scope === 'current';
        return item;
      });
  return result;
}

function directorySummary(path: string): components['schemas']['DirectorySummary'] {
  return {
    path,
    file_count: '3',
    content_count: '3',
    known_bytes: '9223372036854775807',
    unknown_size_file_count: '1',
    size_complete: false,
    unique_content_known_bytes: '9223372036854775807',
    unknown_size_content_count: '1',
    unique_content_size_complete: false,
    max_known_mtime: null,
  };
}

async function fixtures(page: Page, summaryError: boolean, unexpected: string[]) {
  await page.route('**/healthz', (route) => route.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (route.request().method() !== 'GET') {
      unexpected.push(`${route.request().method()} ${path}`);
      return route.abort();
    }
    if (path === '/api/v1/catalog') {
      return route.fulfill({
        json: { revision, scope: 'current', disk_count: '2', file_count: '3', content_count: '3' },
      });
    }
    if (path === '/api/v1/search') {
      return route.fulfill({
        json: search(
          url.searchParams.get('scope') === 'history' ? 'history' : 'current',
          url.searchParams.get('q') === 'absent',
        ),
      });
    }
    if (path === '/api/v1/disks') {
      const disk = diskFixture();
      disk.id = '1';
      disk.label = 'Archive — long literal label <雪> with offline project collections';
      disk.latest_snapshot = searchFixture().items[0]!.snapshot;
      const unknown = diskFixture();
      unknown.id = '2';
      unknown.label = 'Uncataloged disk';
      unknown.capacity = '0';
      unknown.serial = '';
      unknown.notes = '';
      return route.fulfill({ json: { revision, items: [disk, unknown], next_cursor: null } });
    }
    if (path === '/api/v1/contents/1') {
      const content = contentFixture();
      content.size = null;
      return route.fulfill({ json: content });
    }
    if (path === '/api/v1/contents/1/observations') {
      const locations = locationsFixture();
      locations.scope = 'history';
      locations.items[0]!.observation.path = literalPath;
      locations.items[0]!.observation.snapshot_id = '2';
      locations.items[0]!.snapshot = {
        ...locations.items[0]!.snapshot,
        id: '2',
        captured_at: '1969-01-01T00:00:00.000000123Z',
      };
      locations.items[0]!.size = null;
      locations.items[0]!.is_current = false;
      locations.items[1]!.size = null;
      return route.fulfill({ json: locations });
    }
    if (path.startsWith('/api/v1/snapshots/1/directory')) {
      const base = {
        snapshot: searchFixture().items[0]!.snapshot,
        revision,
        is_current: false,
        replica_scope: 'current' as const,
      };
      const directory = url.searchParams.get('path')!;
      if (path.endsWith('/entries')) {
        const items: components['schemas']['DirectoryEntry'][] = [
          {
            path: `${directory}/child`,
            kind: 'directory',
            directory: directorySummary(`${directory}/child`),
            file: null,
          },
          {
            path: literalPath,
            kind: 'file',
            directory: null,
            file: {
              observation: {
                id: '1',
                snapshot_id: '1',
                content_id: '1',
                path: literalPath,
                mtime: null,
              },
              hash: { algorithm: 'sha256', hex: 'ab'.repeat(32) },
              size: null,
              other_disk_count: '0',
              other_location_count: '1',
            },
          },
        ];
        return route.fulfill({
          json: {
            ...base,
            filters: {
              path: directory,
              directories_only: false,
              recursive: false,
              replica_metric: 'disks',
              other_replicas: null,
              min_other_replicas: null,
              max_other_replicas: null,
            },
            items,
            next_cursor: null,
          } satisfies components['schemas']['DirectoryPage'],
        });
      }
      if (summaryError) {
        return route.fulfill({
          status: 500,
          json: { error: { code: 'internal_error', message: 'Baseline summary failure' } },
        });
      }
      return route.fulfill({
        json: {
          ...base,
          directory: directorySummary(directory),
          redundancy_histogram: [{ other_disk_count: '0', file_count: '3' }],
        } satisfies components['schemas']['DirectoryDetail'],
      });
    }
    unexpected.push(`${route.request().method()} ${path}`);
    await route.abort();
  });
}

test('capture U01 representative browser baseline', async ({ browser }, testInfo) => {
  const output = testInfo.outputPath('baseline');
  await mkdir(output, { recursive: true });
  const scenarios = [
    { name: 'search', url: '/?q=report', ready: 'report-1.txt' },
    { name: 'disks', url: '/disks', ready: 'Uncataloged disk' },
    {
      name: 'content-history',
      url: '/contents/1?scope=history',
      ready: 'Historical observations (all complete inventories)',
    },
    {
      name: 'directory-history',
      url: `/snapshots/1/directory?${new URLSearchParams({ path: directoryPath })}`,
      ready: 'Historical source inventory',
    },
    { name: 'search-empty', url: '/?q=absent', ready: 'No matching' },
    {
      name: 'directory-error',
      url: `/snapshots/1/directory?${new URLSearchParams({ path: directoryPath })}`,
      ready: 'Request failed: 500',
    },
  ];
  const records = [];
  for (const width of [1440, 1024, 768]) {
    for (const scenario of scenarios) {
      const page = await browser.newPage({
        viewport: { width, height: 900 },
        locale: 'en-US',
        timezoneId: 'UTC',
        deviceScaleFactor: 1,
        colorScheme: 'dark',
      });
      const unexpected: string[] = [];
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await fixtures(page, scenario.name === 'directory-error', unexpected);
      await page.goto(scenario.url);
      await expect(page.getByText(scenario.ready, { exact: false }).first()).toBeVisible();
      await expect(page.getByText('Backend ready', { exact: false })).toBeVisible();
      if (scenario.name.startsWith('search')) {
        await page.getByRole('searchbox').focus();
        await expect(page.getByRole('searchbox')).toBeFocused();
      }
      if (scenario.name.startsWith('directory')) {
        await expect(
          page
            .getByRole('region', { name: 'Directory entry listing' })
            .getByText(literalPath, { exact: true }),
        ).toBeVisible();
      }
      if (scenario.name === 'content-history') {
        await expect(page.getByRole('table')).toContainText(literalPath);
        await expect(
          page.getByText('History includes older inventories', { exact: false }),
        ).toBeVisible();
      }
      await page.evaluate(() => document.fonts.ready);
      await page.evaluate(() => window.scrollTo(0, 0));
      const measurements = await page.evaluate(() => ({
        documentWidth: document.documentElement.scrollWidth,
        documentHeight: document.documentElement.scrollHeight,
        firstTableTop: document.querySelector('table')?.getBoundingClientRect().top ?? null,
        headerHeight: document.querySelector('header')!.getBoundingClientRect().height,
      }));
      const file = `${scenario.name}-${width}.png`;
      await page.screenshot({ path: `${output}/${file}`, fullPage: true, animations: 'disabled' });
      expect(unexpected).toEqual([]);
      expect(errors).toEqual([]);
      records.push({
        scenario: scenario.name,
        url: scenario.url,
        file,
        viewport: { width, height: 900 },
        ...measurements,
      });
      await page.close();
    }
  }
  await writeFile(
    `${output}/manifest.json`,
    JSON.stringify(
      {
        purpose: 'U01 pre-redesign inspection; not golden assertions or real-backend acceptance',
        browser: 'Chromium',
        version: browser.version(),
        locale: 'en-US',
        timezone: 'UTC',
        deviceScaleFactor: 1,
        searchFocus: 'Explicitly focused query input at all widths for consistent capture state',
        fixtures: 'tests/visual/u01-baseline.spec.ts',
        records,
      },
      null,
      2,
    ) + '\n',
  );
});
