import { expect, test, type Page } from '@playwright/test';
import { diskFixture, searchFixture } from '../fixtures/api';

const snapshot = () => searchFixture().items[0]!.snapshot;

async function ready(page: Page) {
  await page.route('**/healthz', (r) => r.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (r) =>
    r.fulfill({
      json: {
        revision: '9007199254740993',
        scope: 'current',
        disk_count: '3',
        file_count: '4',
        content_count: '2',
      },
    }),
  );
}

test('disk pages use embedded latest capture, literal metadata and no fabricated empty root', async ({
  page,
}) => {
  await ready(page);
  const calls: URL[] = [];
  await page.route('**/api/v1/disks?**', (r) => {
    const url = new URL(r.request().url());
    calls.push(url);
    const disk = diskFixture();
    disk.id = url.searchParams.has('cursor') ? '2' : '1';
    disk.label = '<img src=x onerror=alert(1)>';
    if (disk.id === '1') disk.latest_snapshot = snapshot();
    return r.fulfill({
      json: {
        revision: '9007199254740993',
        items: [disk],
        next_cursor: disk.id === '1' ? 'disk-next' : null,
      },
    });
  });
  await page.goto('/disks?limit=1');
  await expect(
    page.getByRole('link', { name: '<img src=x onerror=alert(1)>', exact: true }),
  ).toBeVisible();
  await expect(page.locator('main img')).toHaveCount(0);
  await expect(page.locator('time').first()).toHaveAttribute(
    'datetime',
    '1970-01-01T00:00:10.000000123Z',
  );
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByText('No complete inventory', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Browse root' })).toHaveCount(1);
  expect(calls.map((u) => u.searchParams.get('limit'))).toEqual(['1', '1']);
  expect(calls[1]!.searchParams.get('cursor')).toBe('disk-next');
  await expect(page).toHaveURL(/\/disks\?limit=1$/);
});

test('bounded disk traversal still reaches later pages without catalog-sized preloading', async ({
  page,
}) => {
  await ready(page);
  let calls = 0;
  await page.route('**/api/v1/disks?**', (r) => {
    calls++;
    const number = Number(new URL(r.request().url()).searchParams.get('cursor') ?? '1');
    const disk = diskFixture();
    disk.id = String(number);
    disk.label = `Disk row ${number}`;
    return r.fulfill({
      json: {
        revision: '9007199254740993',
        items: [disk],
        next_cursor: number === 12 ? null : String(number + 1),
      },
    });
  });
  await page.goto('/disks?limit=1');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  expect(calls).toBe(1);
  for (let n = 2; n <= 10; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.locator('tbody tr')).toHaveCount(n);
  }
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await expect(page.locator('tbody tr')).toContainText('Disk row 11');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('tbody tr')).toContainText('Disk row 12');
  await page.getByRole('button', { name: 'Previous page', exact: true }).click();
  await expect(page.locator('tbody tr')).toContainText('Disk row 11');
  expect(calls).toBe(12);
});

test('snapshot missing resource is not an empty inventory and invalid returns make no feature request', async ({
  page,
}) => {
  await ready(page);
  let calls = 0;
  await page.route('**/api/v1/snapshots/1', (r) => {
    calls++;
    return r.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'missing' } } });
  });
  await page.goto('/snapshots/1');
  await expect(page.getByRole('alert')).toContainText('Inventory not found');
  await expect(page.getByRole('link', { name: 'Browse root' })).toHaveCount(0);
  await page.goto('/snapshots/1?return_to=https%3A%2F%2Fevil.test');
  await expect(page.getByRole('alert')).toContainText('Invalid search return destination');
  expect(calls).toBe(1);
});

test('disk detail separates known subtotal from unknown total and pages capture-ordered history', async ({
  page,
}) => {
  await ready(page);
  const disk = diskFixture();
  disk.id = '1';
  disk.latest_snapshot = snapshot();
  disk.notes = '<script>literal notes</script>';
  disk.cataloged = {
    file_count: '9007199254740993',
    content_count: '1',
    known_bytes: '9007199254740993',
    unknown_size_file_count: '1',
    size_complete: false,
  };
  await page.route('**/api/v1/disks/1', (r) => r.fulfill({ json: disk }));
  await page.route('**/api/v1/disks/1/snapshots?**', (r) => {
    const old = new URL(r.request().url()).searchParams.has('cursor');
    const item = snapshot();
    if (old) {
      item.id = '2';
      item.captured_at = '1969-01-01T00:00:00Z';
      item.imported_at = '2026-10-11T00:00:00Z';
    }
    return r.fulfill({
      json: {
        disk_id: '1',
        revision: '9007199254740993',
        items: [item],
        next_cursor: old ? null : 'history-next',
      },
    });
  });
  await page.goto('/disks/1?limit=1&return_to=%2F%3Fq%3Dreport&observation=1');
  await page.getByText('Disk notes', { exact: true }).click();
  await expect(page.getByText('<script>literal notes</script>', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Cataloged inventory size' })).toContainText(
    '9,007,199,254,740,993 B',
  );
  await expect(page.getByRole('region', { name: 'Cataloged inventory size' })).toContainText(
    'Unknown',
  );
  await expect(page.getByRole('region', { name: 'Cataloged inventory size' })).toContainText(
    'Incomplete',
  );
  await expect(page.getByRole('link', { name: 'Return to search' })).toHaveAttribute(
    'href',
    '/?q=report',
  );
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  const history = page.getByRole('region', { name: 'Inventory history' });
  await expect(history.locator('tbody tr')).toHaveCount(2);
  await expect(history.locator('tbody tr').nth(0)).toContainText('Latest complete inventory');
  await expect(history.locator('tbody tr').nth(1)).toContainText('Historical inventory');
  await expect(history.locator('tbody tr').nth(1)).toContainText('1969-01-01T00:00:00Z');
  await expect(history).not.toContainText('Imported at');
  await expect(page.getByRole('region', { name: 'Latest complete inventory' })).not.toContainText(
    'Distinct contents',
  );
  await expect(page.locator('main')).not.toContainText(/free capacity|disk full/i);
});

test('no inventory differs from an empty complete inventory', async ({ page }) => {
  await ready(page);
  const disk = diskFixture();
  disk.id = '1';
  await page.route('**/api/v1/disks/1', (r) => r.fulfill({ json: disk }));
  await page.route('**/api/v1/disks/1/snapshots?**', (r) =>
    r.fulfill({
      json: {
        disk_id: '1',
        revision: '9007199254740993',
        items: [],
        next_cursor: null,
      },
    }),
  );
  await page.goto('/disks/1');
  await expect(
    page.getByText('Cataloged total: Unknown — no complete inventory.', { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Browse root' })).toHaveCount(0);
  await expect(
    page.getByText(/Stop the server before CLI catalog operations/).first(),
  ).toBeVisible();
  disk.latest_snapshot = snapshot();
  disk.latest_snapshot.file_count = '0';
  disk.latest_snapshot.content_count = '0';
  disk.cataloged = {
    file_count: '0',
    content_count: '0',
    known_bytes: '0',
    unknown_size_file_count: '0',
    size_complete: true,
  };
  await page.reload();
  await expect(page.getByRole('region', { name: 'Cataloged inventory size' })).toContainText(
    'Cataloged total',
  );
  await expect(page.getByRole('region', { name: 'Cataloged inventory size' })).toContainText('0 B');
  await expect(page.getByRole('link', { name: 'Browse root' })).toHaveCount(1);
});

test('snapshot dates, source-mtime provenance, counts and literal backend source are not browser links', async ({
  page,
}) => {
  await ready(page);
  const item = snapshot();
  item.capture_provenance = 'source_mtime';
  item.input_path = '/<img src=x>&?.cshd';
  item.file_count = '9007199254740993';
  await page.route('**/api/v1/snapshots/1', (r) => r.fulfill({ json: item }));
  await page.goto('/snapshots/1?return_to=%2F%3Fq%3Dreport&observation=1');
  await expect(page.getByText('/<img src=x>&?.cshd', { exact: true })).not.toBeVisible();
  await page.getByText('Import and capture provenance', { exact: true }).click();
  await expect(page.getByText('/<img src=x>&?.cshd', { exact: true })).toBeVisible();
  await expect(page.locator('main img')).toHaveCount(0);
  await expect(page.locator('main')).toContainText('Source inventory file modification time');
  await expect(page.locator('main')).toContainText('9,007,199,254,740,993');
  await expect(page.locator('main')).toContainText('Captured at');
  await expect(page.locator('main')).toContainText('Imported at');
  await expect(page.getByRole('link', { name: 'Browse root' })).toHaveAttribute(
    'href',
    '/snapshots/1/directory?path=',
  );
  await expect(page.getByRole('link', { name: 'Disk 1' })).toHaveAttribute('href', /return_to=/);
  await expect(page.getByRole('link', { name: '/<img src=x>&?.cshd' })).toHaveCount(0);
});

test('history failure preserves detail and stale traversal restarts explicitly', async ({
  page,
}) => {
  await ready(page);
  const disk = diskFixture();
  disk.id = '1';
  disk.latest_snapshot = snapshot();
  await page.route('**/api/v1/disks/1', (r) => r.fulfill({ json: disk }));
  let starts = 0;
  await page.route('**/api/v1/disks/1/snapshots?**', (r) => {
    if (new URL(r.request().url()).searchParams.has('cursor'))
      return r.fulfill({
        status: 409,
        json: { error: { code: 'stale_cursor', message: 'changed' } },
      });
    starts++;
    return r.fulfill({
      json: {
        disk_id: '1',
        revision: '9007199254740993',
        items: [snapshot()],
        next_cursor: 'next',
      },
    });
  });
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByText('Inventory changed; reload results', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Disk metadata' })).toContainText('archive');
  await page.getByRole('button', { name: 'Reload results' }).click();
  await expect(
    page.getByRole('region', { name: 'Inventory history' }).locator('tbody tr'),
  ).toHaveCount(1);
  expect(starts).toBe(2);
});

test('history disconnect marks retained metadata unverified and reconnect clears both owners', async ({
  page,
}) => {
  await ready(page);
  const disk = diskFixture();
  disk.id = '1';
  disk.latest_snapshot = snapshot();
  await page.route('**/api/v1/disks/1', (r) => r.fulfill({ json: disk }));
  let fail = false;
  await page.route('**/api/v1/disks/1/snapshots?**', (r) =>
    r.fulfill(
      fail
        ? { status: 503, json: { error: { code: 'catalog_unavailable', message: 'offline' } } }
        : {
            json: {
              disk_id: '1',
              revision: '9007199254740993',
              items: [snapshot()],
              next_cursor: 'next',
            },
          },
    ),
  );
  await page.goto('/disks/1');
  await expect(page.getByRole('region', { name: 'Disk metadata' })).toContainText('archive');
  fail = true;
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.getByText('Previously loaded disk; not freshly verified.')).toBeVisible();
  await expect(page.getByText('Previously loaded results; not freshly verified.')).toBeVisible();
  await expect(page.getByRole('region', { name: 'Inventory history' })).not.toContainText(
    'Latest complete inventory',
  );
  await page.getByRole('button', { name: 'Retry connection', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Disk metadata' })).not.toContainText('archive');
  await expect(
    page.getByRole('region', { name: 'Inventory history' }).locator('tbody tr'),
  ).toHaveCount(0);
});

test('history loads independently when disk metadata fails, without inferred latest badges', async ({
  page,
}) => {
  await ready(page);
  let reads = 0;
  await page.route('**/api/v1/disks/1', (r) =>
    r.fulfill({ status: 500, json: { error: { code: 'internal', message: 'metadata failed' } } }),
  );
  await page.route('**/api/v1/disks/1/snapshots?**', (r) => {
    reads++;
    return r.fulfill({
      json: { disk_id: '1', revision: '9007199254740993', items: [snapshot()], next_cursor: null },
    });
  });
  await page.goto('/disks/1');
  const history = page.getByRole('region', { name: 'Inventory history' });
  await expect(history.locator('tbody tr')).toHaveCount(1);
  await expect(history.locator('.badge')).toHaveCount(0);
  await page
    .getByRole('region', { name: 'Disk metadata' })
    .getByRole('button', { name: 'Retry', exact: true })
    .click();
  await expect(page.getByRole('alert')).toContainText('Unexpected backend response: 500');
  expect(reads).toBe(1);
});

test('compact browsing wraps long literals at approved widths with secondary notes and provenance', async ({
  page,
}) => {
  await ready(page);
  const disk = diskFixture();
  disk.id = '1';
  disk.label = 'Archive / literal <>& Unicode 短 '.repeat(8);
  disk.notes = 'Long literal notes / <>& 短 '.repeat(30);
  disk.latest_snapshot = snapshot();
  disk.cataloged = {
    file_count: '3',
    content_count: '2',
    known_bytes: '0',
    unknown_size_file_count: '1',
    size_complete: false,
  };
  const empty = { ...diskFixture(), id: '2', label: 'Uncataloged', capacity: '0' };
  const old = {
    ...snapshot(),
    id: '2',
    captured_at: '1969-01-01T00:00:00Z',
    imported_at: '2026-10-11T00:00:00Z',
  };
  await page.route('**/api/v1/disks?**', (r) =>
    r.fulfill({ json: { revision: '9007199254740993', items: [disk, empty], next_cursor: null } }),
  );
  await page.route('**/api/v1/disks/1', (r) => r.fulfill({ json: disk }));
  await page.route('**/api/v1/disks/1/snapshots?**', (r) =>
    r.fulfill({
      json: {
        disk_id: '1',
        revision: '9007199254740993',
        items: [snapshot(), old],
        next_cursor: null,
      },
    }),
  );
  const provenance = { ...snapshot(), input_path: '/literal/<>&短/'.repeat(30) };
  await page.route('**/api/v1/snapshots/1', (r) => r.fulfill({ json: provenance }));
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    for (const [state, path] of [
      ['list', '/disks'],
      ['detail', '/disks/1'],
      ['provenance', '/snapshots/1'],
    ]) {
      await page.goto(path!);
      if (state === 'list') {
        await expect(page.locator('tbody tr')).toHaveCount(2);
        await expect(page.getByRole('link', { name: 'Browse root' })).toHaveCount(1);
        await expect(page.getByText(/Imports are CLI-only/)).toBeVisible();
      } else if (state === 'detail') {
        await expect(page.locator('tbody tr')).toHaveCount(2);
        await expect(page.getByText(disk.notes, { exact: true })).not.toBeVisible();
        await page.getByText('Disk notes', { exact: true }).click();
        await expect(page.getByText(disk.notes, { exact: true })).toBeVisible();
      } else {
        await page.getByText('Import and capture provenance', { exact: true }).click();
        await expect(page.getByText(provenance.input_path, { exact: true })).toBeVisible();
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
      const overlapping = await page.locator('tbody tr').evaluateAll((rows) =>
        rows.some((row) => {
          const cells = [...row.children].map((cell) => cell.getBoundingClientRect());
          return cells.some((a, i) =>
            cells
              .slice(i + 1)
              .some(
                (b) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom,
              ),
          );
        }),
      );
      expect(overlapping).toBe(false);
      await page.screenshot({
        path: `/tmp/opencode/u08-screenshots/${state}-${width}.png`,
        fullPage: true,
      });
    }
  }
});
