import { mkdir, writeFile } from 'node:fs/promises';
import { expect, test, type Page } from '@playwright/test';
import { diskFixture, searchFixture } from '../fixtures/api';
import { contentFixture, locationsFixture, observationFixture } from '../fixtures/details';

const revision = '9007199254740993';
const literalPath = `archive/雪 e\u0301/literal ?#% <report>/${'long-segment-'.repeat(12)}/report.txt`;
const directoryPath = literalPath.slice(0, literalPath.lastIndexOf('/'));
const output = '/tmp/opencode/u14-screenshots';

async function fixtures(page: Page, summaryError: boolean) {
  const unexpected: string[] = [];
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const item = searchFixture().items[0]!;
  item.observation.id = '1';
  item.observation.path = literalPath;
  item.basename = 'report.txt';
  item.content.size = '9223372036854775807';
  item.disk.label = `Archive <雪> ${'literal-label-'.repeat(8)}`;
  const disk = diskFixture();
  disk.id = '1';
  disk.label = item.disk.label;
  disk.latest_snapshot = item.snapshot;
  disk.notes = literalPath;
  disk.cataloged = {
    file_count: '2',
    content_count: '1',
    known_bytes: '9223372036854775807',
    unknown_size_file_count: '1',
    size_complete: false,
  };
  const directory = {
    path: directoryPath,
    ...disk.cataloged,
    unique_content_known_bytes: '9223372036854775807',
    unknown_size_content_count: '1',
    unique_content_size_complete: false,
    max_known_mtime: null,
  };
  await page.route('**/healthz', (route) => route.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (route.request().method() !== 'GET') {
      unexpected.push(`${route.request().method()} ${path}`);
      return route.abort();
    }
    if (path === '/api/v1/catalog')
      return route.fulfill({
        json: { revision, scope: 'current', disk_count: '2', file_count: '2', content_count: '1' },
      });
    if (path === '/api/v1/search')
      return route.fulfill({
        json: { ...searchFixture(), items: url.searchParams.get('q') === 'absent' ? [] : [item] },
      });
    if (path === '/api/v1/disks')
      return route.fulfill({ json: { revision, items: [disk], next_cursor: null } });
    if (path === '/api/v1/disks/1') return route.fulfill({ json: disk });
    if (path === '/api/v1/disks/1/snapshots')
      return route.fulfill({
        json: { revision, disk_id: '1', items: [item.snapshot], next_cursor: null },
      });
    if (path === '/api/v1/snapshots/1')
      return route.fulfill({ json: { ...item.snapshot, input_path: literalPath } });
    if (path === '/api/v1/contents')
      return route.fulfill({
        json: {
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
          items: [item.content],
          next_cursor: null,
        },
      });
    if (path === '/api/v1/contents/1')
      return route.fulfill({ json: { ...contentFixture(), size: null } });
    if (path === '/api/v1/contents/1/observations') {
      const locations = locationsFixture();
      locations.scope = 'history';
      locations.items[0]!.observation.path = literalPath;
      locations.items[0]!.is_current = false;
      locations.items[0]!.size = null;
      return route.fulfill({ json: locations });
    }
    if (path === '/api/v1/observations/1')
      return route.fulfill({
        json: { ...observationFixture(), observation: item.observation, disk: item.disk },
      });
    const base = { revision, snapshot: item.snapshot, is_current: false, replica_scope: 'current' };
    if (path === '/api/v1/snapshots/1/directory/entries')
      return route.fulfill({
        json: {
          ...base,
          filters: {
            path: directoryPath,
            directories_only: false,
            recursive: false,
            replica_metric: 'disks',
            other_replicas: null,
            min_other_replicas: null,
            max_other_replicas: null,
          },
          items: [
            {
              kind: 'file',
              path: literalPath,
              directory: null,
              file: {
                observation: item.observation,
                hash: item.content.hash,
                size: null,
                other_disk_count: '0',
                other_location_count: '1',
              },
            },
          ],
          next_cursor: null,
        },
      });
    if (path === '/api/v1/snapshots/1/directory')
      return summaryError
        ? route.fulfill({
            status: 500,
            json: { error: { code: 'internal_error', message: 'Layout summary failure' } },
          })
        : route.fulfill({
            json: {
              ...base,
              directory,
              redundancy_histogram: [{ other_disk_count: '0', file_count: '2' }],
            },
          });
    unexpected.push(`${route.request().method()} ${path}`);
    return route.abort();
  });
  return { unexpected, errors };
}

const directoryURL = `/snapshots/1/directory?${new URLSearchParams({ path: directoryPath })}`;
const scenarios = [
  { name: 'search', url: '/?q=report', ready: 'report.txt', tableTop: 450 },
  { name: 'search-empty', url: '/?q=absent', ready: 'No matching', tableTop: null },
  { name: 'contents', url: '/contents', ready: 'Content 1', tableTop: 650 },
  { name: 'content-history', url: '/contents/1?scope=history', ready: literalPath, tableTop: 800 },
  { name: 'observation', url: '/observations/1', ready: literalPath, tableTop: null },
  { name: 'disks', url: '/disks', ready: 'Archive <雪>', tableTop: 300 },
  { name: 'create', url: '/disks', ready: 'Archive <雪>', tableTop: 300 },
  { name: 'disk-detail', url: '/disks/1', ready: 'Archive <雪>', tableTop: null },
  { name: 'inventory', url: '/snapshots/1', ready: 'Browse root', tableTop: null },
  { name: 'provenance', url: '/snapshots/1', ready: 'Browse root', tableTop: null },
  { name: 'directory', url: directoryURL, ready: literalPath, tableTop: 950 },
  { name: 'directory-error', url: directoryURL, ready: 'Request failed: 500', tableTop: 950 },
];

for (const width of [1440, 1024, 768]) {
  for (const scenario of scenarios) {
    test(`layout ${scenario.name} at ${width}px`, async ({ browser }) => {
      const page = await browser.newPage({
        viewport: { width, height: 900 },
        locale: 'en-US',
        timezoneId: 'UTC',
        deviceScaleFactor: 1,
        colorScheme: 'dark',
      });
      try {
        const evidence = await fixtures(page, scenario.name === 'directory-error');
        await page.goto(scenario.url);
        await expect(page.getByText(scenario.ready, { exact: false }).first()).toBeVisible();
        await expect(page.locator('header')).toContainText('Backend ready');
        if (scenario.name === 'disk-detail')
          await expect(page.getByRole('table').locator('tbody tr')).toHaveCount(1);
        if (scenario.name === 'create') {
          await page.getByRole('button', { name: 'Create disk', exact: true }).click();
          await page.getByLabel('Label', { exact: true }).fill(literalPath);
          await page.getByLabel('Capacity amount').fill('9223372036854775807');
          await expect(page.getByText('9223372036854775807', { exact: true })).toBeVisible();
        }
        if (scenario.name === 'provenance')
          await page.getByText('Import and capture provenance', { exact: true }).click();
        if (scenario.name === 'directory-error') {
          await expect(page.getByRole('table')).toContainText(literalPath);
          await expect(page.getByRole('button', { name: 'Retry', exact: true })).toHaveCount(1);
        }
        await page.evaluate(() => document.fonts.ready);
        await page.evaluate(() => window.scrollTo(0, 0));
        const measurements = await page.evaluate(() => {
          const visible = (element: HTMLElement) => element.getClientRects().length > 0;
          const cells = Array.from(document.querySelectorAll<HTMLElement>('tbody td')).filter(
            visible,
          );
          const controls = Array.from(
            document.querySelectorAll<HTMLElement>(
              'main button, main input, main select, main textarea',
            ),
          ).filter(visible);
          return {
            documentWidth: document.documentElement.scrollWidth,
            headerHeight: document.querySelector('header')!.getBoundingClientRect().height,
            gutter: parseFloat(getComputedStyle(document.querySelector('main')!).paddingLeft),
            firstTableTop: document.querySelector('table')?.getBoundingClientRect().top ?? null,
            clippedCells: cells.filter(
              (cell) =>
                cell.scrollHeight > cell.clientHeight + 1 ||
                cell.scrollWidth > cell.clientWidth + 1,
            ).length,
            overlappingCells: cells.filter((cell, index) => {
              const a = cell.getBoundingClientRect();
              return cells.slice(index + 1).some((other) => {
                const b = other.getBoundingClientRect();
                return (
                  Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 &&
                  Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1
                );
              });
            }).length,
            undersizedControls: controls
              .filter((control) => {
                if (control.matches('input[type=checkbox], input[type=radio]')) return false;
                const minimum = control.classList.contains('button-compact')
                  ? 28
                  : control.classList.contains('input-search')
                    ? 36
                    : 32;
                return control.getBoundingClientRect().height < minimum - 0.5;
              })
              .map((control) => control.outerHTML),
            clippedControls: controls
              .filter((control) => control.scrollHeight > control.clientHeight + 1)
              .map((control) => control.outerHTML),
          };
        });
        expect(measurements.documentWidth).toBe(width);
        expect(measurements.headerHeight).toBe(57);
        expect(measurements.gutter).toBe(width === 768 ? 16 : 24);
        expect(measurements.clippedCells).toBe(0);
        expect(measurements.overlappingCells).toBe(0);
        expect(measurements.undersizedControls).toEqual([]);
        expect(measurements.clippedControls).toEqual([]);
        if (scenario.tableTop !== null) {
          expect(measurements.firstTableTop).not.toBeNull();
          expect(measurements.firstTableTop!).toBeLessThan(scenario.tableTop);
        }
        expect(evidence.unexpected).toEqual([]);
        expect(evidence.errors).toEqual([]);
        await mkdir(output, { recursive: true });
        await writeFile(
          `${output}/${scenario.name}-${width}.json`,
          JSON.stringify({ scenario: scenario.name, width, ...measurements }, null, 2) + '\n',
        );
        await page.screenshot({
          path: `${output}/${scenario.name}-${width}.png`,
          fullPage: true,
          animations: 'disabled',
        });
      } finally {
        await page.close();
      }
    });
  }
}
