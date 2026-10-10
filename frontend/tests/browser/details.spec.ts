import { expect, test, type Page, type Route } from '@playwright/test';
import { contentFixture, locationsFixture, observationFixture } from '../fixtures/details';
import { searchFixture } from '../fixtures/api';

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

test('two independent detail requests, selected context, same-disk copies and exact decimals', async ({
  page,
}) => {
  await ready(page);
  const calls: URL[] = [];
  const content = contentFixture();
  content.size = '9223372036854775807';
  content.observation_count = '9007199254740993';
  await page.route('**/api/v1/contents/1**', (r) => {
    const url = new URL(r.request().url());
    calls.push(url);
    return r.fulfill({
      json: url.pathname.endsWith('/observations') ? locationsFixture() : content,
    });
  });
  await page.goto('/contents/1?return_to=%2F%3Fq%3Dreport&observation=9007199254740993');
  await expect(page.getByRole('region', { name: 'Content identity' })).toContainText(
    '9,223,372,036,854,775,807 B',
  );
  await expect(page.getByRole('region', { name: 'Content identity' })).toContainText(
    '9,007,199,254,740,993',
  );
  await expect(
    page.getByText('Selected observation 9007199254740993', { exact: true }),
  ).toBeVisible();
  await expect(page.locator('tr[aria-current="true"]')).toContainText('É/<report>&?.txt');
  await expect(page.getByRole('region', { name: 'Content identity' })).toContainText(
    /Current disks\s*1/,
  );
  await expect(page.getByRole('region', { name: 'Content identity' })).toContainText(
    /Current locations\s*2/,
  );
  await expect(page.getByRole('link', { name: 'Return to search' })).toHaveAttribute(
    'href',
    '/?q=report',
  );
  await expect(page.locator('tbody tr')).toHaveCount(2);
  expect(calls).toHaveLength(2);
  expect(calls.find((u) => u.pathname.endsWith('/observations'))!.searchParams.get('scope')).toBe(
    'current',
  );
  expect(calls.find((u) => !u.pathname.endsWith('/observations'))!.searchParams.get('scope')).toBe(
    'history',
  );
});

test('summary and location failures retain the other section, retries remain independent', async ({
  page,
}) => {
  await ready(page);
  let summaryFails = true;
  let locationsFail = false;
  const calls: string[] = [];
  await page.route('**/api/v1/contents/1**', (r) => {
    const locations = new URL(r.request().url()).pathname.endsWith('/observations');
    calls.push(locations ? 'locations' : 'summary');
    return r.fulfill(
      (locations ? locationsFail : summaryFails)
        ? { status: 500, json: { error: { code: 'internal_error', message: 'failed' } } }
        : { json: locations ? locationsFixture() : contentFixture() },
    );
  });
  await page.goto('/contents/1');
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(
    page.getByRole('region', { name: 'Content identity' }).getByRole('alert'),
  ).toBeVisible();
  summaryFails = false;
  await page
    .getByRole('region', { name: 'Content identity' })
    .getByRole('button', { name: 'Retry' })
    .click();
  await expect(page.getByText('ab'.repeat(32), { exact: true })).toBeVisible();
  expect(calls.filter((c) => c === 'locations')).toHaveLength(1);
  locationsFail = true;
  await page.getByRole('button', { name: 'Reload locations' }).click();
  await expect(page.getByRole('region', { name: 'Locations' }).getByRole('alert')).toBeVisible();
  await expect(page.getByText('ab'.repeat(32), { exact: true })).toBeVisible();
  expect(calls.filter((c) => c === 'summary')).toHaveLength(2);
});

test('historical-only content, independent history pagination and stale-cursor restart', async ({
  page,
}) => {
  await ready(page);
  const content = contentFixture();
  content.current_disk_count = '0';
  content.current_location_count = '0';
  content.size = null;
  let pageFails = true;
  const scopes: string[] = [];
  await page.route('**/api/v1/contents/1**', (r) => {
    const url = new URL(r.request().url());
    if (!url.pathname.endsWith('/observations')) return r.fulfill({ json: content });
    const scope = url.searchParams.get('scope')!;
    scopes.push(scope);
    const fixture = locationsFixture();
    fixture.scope = scope as 'current' | 'history';
    if (scope === 'current') fixture.items = [];
    else {
      fixture.items = [fixture.items[0]!];
      fixture.items[0]!.is_current = false;
      if (!url.searchParams.has('cursor')) fixture.next_cursor = 'history-next';
      else if (pageFails)
        return r.fulfill({
          status: 400,
          json: { error: { code: 'invalid_request', message: 'page failed' } },
        });
      else
        return r.fulfill({
          status: 409,
          json: { error: { code: 'stale_cursor', message: 'changed' } },
        });
    }
    return r.fulfill({ json: fixture });
  });
  await page.goto('/contents/1?observation=9007199254740993');
  await expect(
    page.getByText('No current locations in complete inventories.', { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('region', { name: 'Content identity' })).toContainText('Unknown');
  await page.getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.getByText('Historical', { exact: true })).toBeVisible();
  await expect(page.getByText(/repeated observations are not extra current copies/i)).toBeVisible();
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  pageFails = false;
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByText('Inventory changed; reload results')).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  expect(scopes).toEqual(['current', 'history', 'history', 'history', 'history']);
  await expect(page).toHaveURL(/observation=9007199254740993/);
});

test('observation detail uses supplied other counts, exact date meanings and literal context links', async ({
  page,
}) => {
  await ready(page);
  const fixture = observationFixture();
  fixture.observation.path = '<root>&?.txt';
  fixture.other_location_count = '9007199254740993';
  await page.route('**/api/v1/observations/9007199254740993', (r) => r.fulfill({ json: fixture }));
  await page.goto(
    '/observations/9007199254740993?return_to=%2F%3Fq%3Dreport&observation=9007199254740993',
  );
  await expect(page.getByText('<root>&?.txt', { exact: true })).toBeVisible();
  await expect(page.getByText('Source mtime: Unknown', { exact: true })).toBeVisible();
  await expect(page.locator('time').first()).toHaveAttribute(
    'datetime',
    fixture.snapshot.captured_at,
  );
  for (const summary of await page.getByText('Exact UTC', { exact: true }).all())
    await summary.click();
  await expect(page.getByText('1970-01-01T00:00:10.000000123Z', { exact: true })).toBeVisible();
  await expect(page.getByText('2026-10-10T12:00:00Z', { exact: true })).toBeVisible();
  await expect(page.locator('main')).toContainText(
    /Other current locations\s*9,007,199,254,740,993/,
  );
  await expect(page.locator('main')).toContainText(/Other current disks\s*0/);
  await expect(page.getByRole('link', { name: 'Containing directory' })).toHaveAttribute(
    'href',
    '/snapshots/1/directory?path=',
  );
  await expect(page.getByRole('link', { name: 'Inventory 1' })).toHaveAttribute(
    'href',
    '/snapshots/1?return_to=%2F%3Fq%3Dreport&observation=9007199254740993',
  );
  await expect(page.getByRole('link', { name: 'Disk <archive>' })).toHaveAttribute(
    'href',
    '/disks/1?return_to=%2F%3Fq%3Dreport&observation=9007199254740993',
  );
  await expect(page.getByRole('link', { name: 'Content 1' })).toHaveAttribute(
    'href',
    /observation=9007199254740993/,
  );
});

test('clipboard failures are visible and hash lookup validates explicit algorithm/digest', async ({
  page,
}) => {
  await ready(page);
  await page.addInitScript(() =>
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: () => Promise.reject(new Error('denied')),
      },
    }),
  );
  await page.route('**/api/v1/contents/1**', (r) =>
    r.fulfill({
      json: new URL(r.request().url()).pathname.endsWith('/observations')
        ? locationsFixture()
        : contentFixture(),
    }),
  );
  await page.goto('/contents/1');
  await page.getByRole('button', { name: 'Copy digest' }).click();
  await expect(page.getByText('Copy failed. Select and copy the text manually.')).toBeVisible();
  const lookups: URL[] = [];
  let found = false;
  await page.route('**/api/v1/contents/lookup?**', (r) => {
    lookups.push(new URL(r.request().url()));
    return r.fulfill(
      found
        ? { json: contentFixture() }
        : { status: 404, json: { error: { code: 'not_found', message: 'unknown hash' } } },
    );
  });
  await page.getByRole('main').getByRole('link', { name: 'Hash lookup' }).click();
  await page.getByLabel('Algorithm', { exact: true }).selectOption('md5');
  await page.getByLabel('Digest (hex)', { exact: true }).fill('zz');
  await page.getByRole('button', { name: 'Look up content' }).click();
  await expect(page.getByRole('alert')).toContainText(/hexadecimal/i);
  expect(lookups).toHaveLength(0);
  await page.getByLabel('Digest (hex)', { exact: true }).fill('AB'.repeat(16));
  await page.getByRole('button', { name: 'Look up content' }).click();
  await expect(page.getByText('No content found for this algorithm and digest.')).toBeVisible();
  expect(lookups[0]!.searchParams.get('hash_type')).toBe('md5');
  expect(lookups[0]!.searchParams.get('hash')).toBe('AB'.repeat(16));
  found = true;
  await page.getByRole('button', { name: 'Look up content' }).click();
  await expect(page.getByRole('link', { name: 'Open content 1' })).toBeVisible();
  await page.getByRole('link', { name: 'Open content 1' }).click();
  await expect(page).toHaveURL('/contents/1?scope=current');
});

test('search detail return keeps the in-app session, selection and scroll; reload refetches', async ({
  page,
}) => {
  await ready(page);
  let searches = 0;
  await page.route('**/api/v1/search?**', (r) => {
    searches++;
    const fixture = searchFixture();
    fixture.items = Array.from({ length: 50 }, (_, i) => ({
      ...structuredClone(fixture.items[0]!),
      basename: `report-${i}`,
      observation: { ...fixture.items[0]!.observation, id: String(i + 1) },
    }));
    return r.fulfill({ json: fixture });
  });
  await page.route('**/api/v1/contents/1**', (r) =>
    r.fulfill({
      json: new URL(r.request().url()).pathname.endsWith('/observations')
        ? locationsFixture()
        : contentFixture(),
    }),
  );
  await page.goto('/?q=report&field=path');
  await page.getByRole('button', { name: 'Select report-40', exact: true }).click();
  await expect(page.locator('[data-selected-content="true"]')).toHaveText('report-40');
  await page.getByRole('link', { name: 'report-40', exact: true }).scrollIntoViewIfNeeded();
  const scroll = await page.evaluate(() => window.scrollY);
  await page.getByRole('link', { name: 'report-40', exact: true }).click();
  await expect(page.getByText('Selected observation 41', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Return to search' }).click();
  await expect(page.locator('[data-selected-content="true"]')).toHaveText('report-40');
  await expect
    .poll(async () => Math.abs((await page.evaluate(() => window.scrollY)) - scroll))
    .toBeLessThan(5);
  expect(searches).toBe(1);
  await page.locator('[data-selected-content="true"]').click();
  await expect(page.getByRole('heading', { name: 'Content 1', exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.locator('[data-selected-content="true"]')).toHaveText('report-40');
  expect(searches).toBe(1);
  await page.reload();
  await expect(page.getByRole('link', { name: 'report-40', exact: true })).toBeAttached();
  expect(searches).toBe(2);
  await expect(page.locator('[data-selected-content="true"]')).toHaveCount(0);
});

test('scope replacement rejects obsolete location responses and invalid return links', async ({
  page,
}) => {
  await ready(page);
  let pending: Route | undefined;
  await page.route('**/api/v1/contents/1**', (r) => {
    const url = new URL(r.request().url());
    if (!url.pathname.endsWith('/observations')) return r.fulfill({ json: contentFixture() });
    if (url.searchParams.get('scope') === 'current') {
      pending = r;
      return;
    }
    const fixture = locationsFixture();
    fixture.scope = 'history';
    fixture.items = [];
    return r.fulfill({ json: fixture });
  });
  await page.goto('/contents/1');
  await expect.poll(() => !!pending).toBe(true);
  await page.getByRole('link', { name: 'History', exact: true }).click();
  await expect(page.getByText('No historical observations in complete inventories.')).toBeVisible();
  await pending!.fulfill({ json: locationsFixture() });
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await page.goto('/contents/1?return_to=https%3A%2F%2Fevil.example');
  await expect(page.getByRole('alert')).toContainText('Invalid search return destination');
  await expect(page.getByRole('link', { name: 'Return to search' })).toHaveCount(0);
});

test('history pages include repeated disk/path observations without extra detail requests', async ({
  page,
}) => {
  await ready(page);
  const calls: URL[] = [];
  const extraCalls: string[] = [];
  page.on('request', (r) => {
    if (/\/api\/v1\/(observations|snapshots|disks)/.test(r.url())) extraCalls.push(r.url());
  });
  await page.route('**/api/v1/contents/1**', (r) => {
    const url = new URL(r.request().url());
    calls.push(url);
    if (!url.pathname.endsWith('/observations')) return r.fulfill({ json: contentFixture() });
    const fixture = locationsFixture();
    fixture.scope = 'history';
    fixture.items = [fixture.items[0]!];
    fixture.items[0]!.is_current = false;
    if (url.searchParams.has('cursor')) {
      fixture.items[0]!.observation.id = '3';
      fixture.items[0]!.snapshot.id = '2';
    } else fixture.next_cursor = 'literal/+==';
    return r.fulfill({ json: fixture });
  });
  await page.goto('/contents/1?scope=history&limit=1');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await expect(page.getByText('É/<report>&?.txt', { exact: true })).toHaveCount(2);
  await expect(page.getByText('Historical', { exact: true })).toHaveCount(2);
  await expect(page.getByText('All results loaded.')).toBeVisible();
  expect(calls).toHaveLength(3);
  expect(calls.at(-1)!.searchParams.get('cursor')).toBe('literal/+==');
  expect(calls.at(-1)!.searchParams.get('limit')).toBe('1');
  expect(extraCalls).toEqual([]);
});

test('location traversal reaches later pages at the bound and restores retained pages', async ({
  page,
}) => {
  await ready(page);
  let calls = 0;
  await page.route('**/api/v1/contents/1**', (r) => {
    const url = new URL(r.request().url());
    if (!url.pathname.endsWith('/observations')) return r.fulfill({ json: contentFixture() });
    calls++;
    const n = Number(url.searchParams.get('cursor') ?? '1');
    const fixture = locationsFixture();
    fixture.items = [fixture.items[0]!];
    fixture.items[0]!.observation.id = String(n);
    fixture.items[0]!.observation.path = `page-${n}`;
    fixture.next_cursor = n === 12 ? null : String(n + 1);
    return r.fulfill({ json: fixture });
  });
  await page.goto('/contents/1');
  for (let i = 1; i < 10; i++) await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByText('10 loaded', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(page.getByText('page-11', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(page.getByText('page-12', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Next page' })).toBeDisabled();
  await page.getByRole('button', { name: 'Previous page' }).click();
  await expect(page.getByText('page-11', { exact: true })).toBeVisible();
  expect(calls).toBe(12);
});

test('unavailable locations retain successful identity as unverified until reconnect clears it', async ({
  page,
}) => {
  await ready(page);
  let fail = false;
  await page.route('**/api/v1/contents/1**', (r) => {
    if (!new URL(r.request().url()).pathname.endsWith('/observations'))
      return r.fulfill({ json: contentFixture() });
    return r.fulfill(
      fail
        ? { status: 503, json: { error: { code: 'catalog_unavailable', message: 'offline' } } }
        : { json: locationsFixture() },
    );
  });
  await page.goto('/contents/1');
  await expect(page.getByText('ab'.repeat(32), { exact: true })).toBeVisible();
  fail = true;
  await page.getByRole('button', { name: 'Reload locations' }).click();
  await expect(page.getByRole('region', { name: 'Locations' }).getByRole('alert')).toContainText(
    'Backend unavailable',
  );
  await expect(page.getByText('ab'.repeat(32), { exact: true })).toBeVisible();
  await expect(page.getByText('Previously loaded identity; not freshly verified.')).toBeVisible();
  await page.getByRole('button', { name: 'Retry connection', exact: true }).click();
  await expect(page.getByText('ab'.repeat(32), { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Reload content' })).toBeVisible();
});

test('missing observations and malformed content are resource failures, not empty success', async ({
  page,
}) => {
  await ready(page);
  await page.route('**/api/v1/observations/1', (r) =>
    r.fulfill({
      status: 404,
      json: { error: { code: 'not_found', message: 'missing observation' } },
    }),
  );
  await page.goto('/observations/1');
  await expect(page.getByRole('alert')).toContainText('Observation not found');
  await page.route('**/api/v1/contents/1**', (r) =>
    r.fulfill({
      json: new URL(r.request().url()).pathname.endsWith('/observations') ? locationsFixture() : {},
    }),
  );
  await page.goto('/contents/1');
  await expect(
    page.getByRole('region', { name: 'Content identity' }).getByRole('alert'),
  ).toContainText('Unexpected backend response');
  await expect(page.locator('tbody tr')).toHaveCount(2);
});

test('lookup errors retain inputs and obsolete responses cannot show a different identity', async ({
  page,
}) => {
  await ready(page);
  let pending: Route | undefined;
  await page.route('**/api/v1/contents/lookup?**', (r) => {
    if (new URL(r.request().url()).searchParams.get('hash') === 'ab') {
      pending = r;
      return;
    }
    return r.fulfill({
      status: 500,
      json: { error: { code: 'internal_error', message: 'failed' } },
    });
  });
  await page.goto('/contents');
  await page.getByLabel('Digest (hex)', { exact: true }).fill('ab');
  await page.getByRole('button', { name: 'Look up content' }).click();
  await expect.poll(() => !!pending).toBe(true);
  await page.getByLabel('Algorithm', { exact: true }).selectOption('md4');
  await page.getByLabel('Digest (hex)', { exact: true }).fill('cd');
  await page.getByRole('button', { name: 'Look up content' }).click();
  await expect(page.getByRole('alert')).toContainText('Request failed');
  await pending!.fulfill({ json: contentFixture() });
  await expect(page.getByRole('link', { name: 'Open content 1' })).toHaveCount(0);
  await expect(page.getByLabel('Algorithm', { exact: true })).toHaveValue('md4');
  await expect(page.getByLabel('Digest (hex)', { exact: true })).toHaveValue('cd');
});

test('malformed lookup 404 is not a confirmed unknown identity', async ({ page }) => {
  await ready(page);
  await page.route('**/api/v1/contents/lookup?**', (r) => r.fulfill({ status: 404, json: {} }));
  await page.goto('/contents');
  await page.getByLabel('Digest (hex)', { exact: true }).fill('ab');
  await page.getByRole('button', { name: 'Look up content' }).click();
  await expect(page.getByRole('alert')).toContainText('Unexpected backend response');
  await expect(page.getByText('No content found for this algorithm and digest.')).toHaveCount(0);
});

test('disconnected continuation retry discards suspended cursors and restarts page one', async ({
  page,
}) => {
  await ready(page);
  const calls: (string | null)[] = [];
  await page.route('**/api/v1/contents/1**', (r) => {
    const url = new URL(r.request().url());
    if (!url.pathname.endsWith('/observations')) return r.fulfill({ json: contentFixture() });
    const cursor = url.searchParams.get('cursor');
    calls.push(cursor);
    if (cursor)
      return r.fulfill({
        status: 503,
        json: { error: { code: 'catalog_unavailable', message: 'offline' } },
      });
    const fixture = locationsFixture();
    fixture.next_cursor = 'next';
    return r.fulfill({ json: fixture });
  });
  await page.goto('/contents/1');
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByRole('region', { name: 'Locations' }).getByRole('alert')).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  await page
    .getByRole('region', { name: 'Locations' })
    .getByRole('button', { name: 'Retry', exact: true })
    .click();
  await expect.poll(() => calls).toEqual([null, 'next', null]);
  await expect(page.locator('tbody tr')).toHaveCount(2);
});

test('keyboard detail to observation to return and successful clipboard preserve literal values', async ({
  page,
}) => {
  await ready(page);
  await page.addInitScript(() =>
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: (value: string) => {
          sessionStorage.setItem('test-copied', value);
          return Promise.resolve();
        },
      },
    }),
  );
  await page.route('**/api/v1/search?**', (r) => r.fulfill({ json: searchFixture() }));
  await page.route('**/api/v1/contents/1**', (r) =>
    r.fulfill({
      json: new URL(r.request().url()).pathname.endsWith('/observations')
        ? locationsFixture()
        : contentFixture(),
    }),
  );
  await page.route('**/api/v1/observations/9007199254740993*', (r) =>
    r.fulfill({ json: observationFixture() }),
  );
  await page.goto('/?q=report');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('searchbox').press('ArrowDown');
  await page.getByRole('searchbox').press('Enter');
  const selected = page.getByRole('link', { name: 'Selected observation 9007199254740993' });
  await selected.focus();
  await page.keyboard.press('Enter');
  await expect(
    page.getByRole('heading', { name: 'Observation 9007199254740993', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Copy path' }).click();
  await expect(page.getByText('Copied.', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => sessionStorage.getItem('test-copied'))).toBe('É/<report>&?.txt');
  await page.getByRole('link', { name: 'Return to search' }).focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-selected-content="true"]')).toHaveText('<report>&?.txt');
});
