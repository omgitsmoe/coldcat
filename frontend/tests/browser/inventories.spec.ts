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
  await expect(page.getByRole('article')).toHaveCount(1);
  expect(calls).toBe(1);
  for (let n = 2; n <= 10; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.getByRole('article')).toHaveCount(n);
  }
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(page.getByRole('article')).toContainText('Disk row 11');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByRole('article')).toContainText('Disk row 12');
  await page.getByRole('button', { name: 'Previous page', exact: true }).click();
  await expect(page.getByRole('article')).toContainText('Disk row 11');
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
  await expect(history.getByRole('article')).toHaveCount(2);
  await expect(history.getByRole('article').nth(0)).toContainText('Latest complete inventory');
  await expect(history.getByRole('article').nth(1)).toContainText('Historical inventory');
  await expect(history.getByRole('article').nth(1)).toContainText('1969-01-01T00:00:00Z');
  await expect(history.getByRole('article').nth(1)).toContainText('2026-10-11T00:00:00Z');
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
    page.getByRole('region', { name: 'Inventory history' }).getByRole('article'),
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
    page.getByRole('region', { name: 'Inventory history' }).getByRole('article'),
  ).toHaveCount(0);
});
