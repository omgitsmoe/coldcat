import { expect, test, type Page, type Route } from '@playwright/test';
import { searchFixture } from '../fixtures/api';
import { contentFixture, locationsFixture } from '../fixtures/details';
import { mkdir, writeFile } from 'node:fs/promises';

async function ready(page: Page) {
  await page.route('**/api/v1/contents/1**', (r) =>
    r.fulfill({
      json: new URL(r.request().url()).pathname.endsWith('/observations')
        ? locationsFixture()
        : contentFixture(),
    }),
  );
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
function result(name: string, cursor: string | null = null) {
  const fixture = searchFixture();
  fixture.items[0]!.basename = name;
  fixture.items[0]!.observation.path = `雪/${name}`;
  fixture.next_cursor = cursor;
  return fixture;
}

test('literal Unicode validation, Enter flush and latest delayed request wins', async ({
  page,
}) => {
  await ready(page);
  const pending: Route[] = [];
  await page.route('**/api/v1/search?**', (r) => {
    pending.push(r);
  });
  await page.goto('/');
  const input = page.getByRole('searchbox');
  await expect(input).toBeFocused();
  await input.fill('😀雪');
  await expect(page.getByText('Type at least 3 characters, or choose Exact.')).toBeVisible();
  await page.waitForTimeout(250);
  expect(pending).toHaveLength(0);
  await input.fill('old');
  await expect.poll(() => pending.length).toBe(1);
  await expect(page.getByText('Searching…', { exact: false })).toBeVisible();
  await expect(page.getByText(/Still working/)).toBeVisible();
  await input.fill(' 雪😀/? ');
  await input.press('Enter');
  await expect.poll(() => pending.length).toBe(2);
  expect(new URL(pending[1]!.request().url()).searchParams.get('q')).toBe(' 雪😀/? ');
  await pending[1]!.fulfill({ json: result('latest') });
  await expect(page.getByRole('link', { name: 'latest', exact: true })).toBeVisible();
  await pending[0]!.fulfill({ json: result('obsolete') });
  await expect(page.getByRole('link', { name: 'obsolete', exact: true })).toHaveCount(0);
  await expect(input).toHaveValue(' 雪😀/? ');
  expect(pending).toHaveLength(2);
});

test('obsolete continuation and filter changes cannot append old results', async ({ page }) => {
  await ready(page);
  let continuation: Route | undefined;
  const calls: string[] = [];
  await page.route('**/api/v1/search?**', (r) => {
    const url = new URL(r.request().url());
    calls.push(url.search);
    if (url.searchParams.has('cursor')) {
      continuation = r;
      return;
    }
    return r.fulfill({ json: result(url.searchParams.get('q')!, 'next') });
  });
  await page.goto('/?q=first');
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect.poll(() => continuation !== undefined).toBe(true);
  await page.getByRole('searchbox').fill('second');
  await expect(page.getByRole('link', { name: 'first', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'second', exact: true })).toBeVisible();
  await continuation!.fulfill({ json: result('obsolete-page') });
  await expect(page.getByRole('link', { name: 'obsolete-page', exact: true })).toHaveCount(0);
  await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('history');
  await expect(page.getByRole('link', { name: 'second', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect.poll(() => calls.length).toBe(4);
  expect(new URLSearchParams(calls.at(-1)).get('scope')).toBe('history');
});

test('desktop type-to-search respects preference, forms, selection, dialogs and IME', async ({
  page,
}) => {
  await ready(page);
  await page.goto('/');
  const input = page.getByRole('searchbox');
  await page.locator('h1').click();
  await page.keyboard.type('a');
  await expect(input).toHaveValue('a');
  await page.getByText('Keyboard help', { exact: true }).click();
  await page.getByLabel('Type to search on desktop').uncheck();
  await page.locator('h1').click();
  await page.keyboard.type('b');
  await expect(input).toHaveValue('a');
  await page.getByLabel('Type to search on desktop').check();
  await page.getByText('Advanced filters (0 active)', { exact: true }).click();
  await page.getByLabel('Disk ID', { exact: true }).fill('1');
  await page.keyboard.type('2');
  await expect(input).toHaveValue('a');
  await page.locator('h1').click();
  await page.locator('h1').dispatchEvent('keydown', { key: '雪', isComposing: true });
  await page.locator('h1').dispatchEvent('keydown', { key: 'Dead' });
  await expect(input).toHaveValue('a');
  await input.dispatchEvent('compositionstart');
  await input.fill('雪');
  await input.dispatchEvent('input', { isComposing: true });
  await input.dispatchEvent('compositionend');
  await expect(input).toHaveValue('雪');
  await page.evaluate(() => {
    const selection = window.getSelection()!;
    const range = document.createRange();
    range.selectNodeContents(document.querySelector('h1')!);
    selection.removeAllRanges();
    selection.addRange(range);
  });
  await page.locator('h1').dispatchEvent('keydown', { key: 'x' });
  await expect(input).toHaveValue('雪');
  await page.evaluate(() => {
    window.getSelection()!.removeAllRanges();
    const d = document.createElement('dialog');
    document.body.append(d);
    d.showModal();
  });
  await page.locator('dialog').dispatchEvent('keydown', { key: 'x' });
  await expect(input).toHaveValue('雪');
});

test('short exact, explicit invalid filters, empty/history results and ordinary links', async ({
  page,
}) => {
  await ready(page);
  const calls: URL[] = [];
  await page.route('**/api/v1/search?**', (r) => {
    calls.push(new URL(r.request().url()));
    const fixture = result('<same>&?.txt');
    fixture.scope = 'history';
    fixture.items[0]!.is_current = false;
    fixture.items[0]!.content.scope = 'history';
    fixture.items[0]!.content.size = null;
    fixture.items.push(structuredClone(fixture.items[0]!));
    fixture.items[1]!.observation.id = '2';
    fixture.items[1]!.content.id = '2';
    fixture.items[1]!.content.hash.hex = 'cd'.repeat(32);
    fixture.items[1]!.observation.content_id = '2';
    fixture.items[1]!.content.size = '0';
    return r.fulfill({ json: fixture });
  });
  await page.goto('/');
  await page.getByRole('searchbox').fill('a');
  await page.getByRole('combobox', { name: 'Match', exact: true }).selectOption('exact');
  await page.getByText('Advanced filters (0 active)', { exact: true }).click();
  await page
    .getByRole('combobox', { name: 'Other-copy matching', exact: true })
    .selectOption('exact');
  await page.getByLabel('Exact other copies', { exact: true }).fill('0');
  await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('history');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByText(/Other-copy bounds require current scope;/)).toBeVisible();
  expect(calls).toHaveLength(0);
  await expect(page.getByLabel('Exact other copies', { exact: true })).toHaveValue('0');
  await page
    .getByRole('combobox', { name: 'Other-copy matching', exact: true })
    .selectOption('any');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(2);
  expect(calls[0]!.searchParams.get('q')).toBe('a');
  expect(calls[0]!.searchParams.get('match')).toBe('exact');
  await expect(page.getByText('Unknown', { exact: true })).toBeVisible();
  await expect(page.getByText('0 B', { exact: true })).toBeVisible();
  await expect(page.getByText(/Historical · Inventory/)).toHaveCount(2);
  const link = page.getByRole('link', { name: '<same>&?.txt', exact: true }).first();
  const destination = new URL((await link.getAttribute('href'))!, 'http://localhost');
  expect(destination.pathname).toBe('/contents/1');
  expect(destination.searchParams.get('scope')).toBe('current');
  expect(destination.searchParams.get('observation')).toBe('9007199254740993');
  expect(
    new URL(destination.searchParams.get('return_to')!, 'http://localhost').searchParams.get(
      'scope',
    ),
  ).toBe('history');
  await expect(
    page.getByRole('link', { name: 'Observation', exact: true }).first(),
  ).toHaveAttribute('href', /\/observations\/9007199254740993\?/);
  await expect(page.getByRole('link', { name: 'Containing directory' }).first()).toHaveAttribute(
    'href',
    '/snapshots/1/directory?path=%E9%9B%AA',
  );
  expect(calls).toHaveLength(1);
});

test('page failures retain rows for retry; stale cursor explicitly restarts page one', async ({
  page,
}) => {
  await ready(page);
  let attempts = 0;
  await page.route('**/api/v1/search?**', (r) => {
    const cursor = new URL(r.request().url()).searchParams.get('cursor');
    if (!cursor) return r.fulfill({ json: result('first', 'next') });
    attempts++;
    return r.fulfill({
      status: attempts === 1 ? 400 : 409,
      json: {
        error: {
          code: attempts === 1 ? 'invalid_request' : 'stale_cursor',
          message: 'page failed',
        },
      },
    });
  });
  await page.goto('/?q=report');
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByRole('link', { name: 'first', exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toContainText('page failed');
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByText('Inventory changed; reload results')).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.getByRole('link', { name: 'first', exact: true })).toBeVisible();
  expect(attempts).toBe(2);
});

test('typing replaces history; discrete filters, selected links, Back and reload restore safely', async ({
  page,
}) => {
  await ready(page);
  let calls = 0;
  await page.route('**/api/v1/search?**', (r) => {
    calls++;
    return r.fulfill({ json: result('report') });
  });
  await page.goto('/');
  const input = page.getByRole('searchbox');
  const length = await page.evaluate(() => history.length);
  await input.fill('rep');
  await input.fill('report');
  await expect(page.getByRole('link', { name: 'report', exact: true })).toBeVisible();
  expect(await page.evaluate(() => history.length)).toBe(length);
  await page.getByRole('combobox', { name: 'Field', exact: true }).selectOption('path');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('link', { name: 'report', exact: true })).toBeVisible();
  expect(await page.evaluate(() => history.length)).toBe(length + 1);
  await page.goBack();
  await expect(page.getByRole('combobox', { name: 'Field', exact: true })).toHaveValue('name');
  await expect(page.getByRole('link', { name: 'report', exact: true })).toBeVisible();
  await input.focus();
  await input.press('ArrowDown');
  await expect(page.getByRole('button', { name: 'Open selected content' })).toBeVisible();
  await input.press('Escape');
  await expect(page.getByRole('button', { name: 'Open selected content' })).toHaveCount(0);
  await expect(input).toHaveValue('report');
  await input.press('ArrowDown');
  await input.press('Enter');
  await expect(page).toHaveURL(/\/contents\/1\?/);
  const before = calls;
  await page.goBack();
  await expect(page.getByRole('searchbox')).toHaveValue('report');
  await expect(page.getByRole('link', { name: 'report', exact: true })).toBeVisible();
  expect(calls).toBe(before);
  await page.reload();
  await expect(page.getByRole('link', { name: 'report', exact: true })).toBeVisible();
  expect(calls).toBe(before + 1);
  await expect(page.getByRole('button', { name: 'Open selected content' })).toHaveCount(0);
});

test('invalid bookmarked parameters are correctable and explicit shortcuts work on detail routes', async ({
  page,
}) => {
  await ready(page);
  await page.route('**/api/v1/search?**', (r) => r.fulfill({ json: result('corrected') }));
  await page.goto('/?q=abc&cursor=obsolete');
  await expect(page.getByRole('alert')).toContainText('Unexpected route parameter');
  await page.getByRole('searchbox').fill('corrected');
  await expect(page.getByRole('link', { name: 'corrected', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'corrected', exact: true }).click();
  await page.locator('h1').click();
  await page.keyboard.type('x');
  await expect(page).toHaveURL(/\/contents\/1\?/);
  await page.keyboard.press('Control+k');
  await expect(page.getByRole('searchbox')).toBeFocused();
});

test('50-row pages reach later results at the ten-page bound without detail calls', async ({
  page,
}) => {
  await ready(page);
  const calls: string[] = [];
  const detailCalls: string[] = [];
  page.on('request', (request) => {
    if (/\/api\/v1\/(contents|observations|snapshots|disks)/.test(request.url()))
      detailCalls.push(request.url());
  });
  await page.route('**/api/v1/search?**', (r) => {
    calls.push(r.request().url());
    const n = Number(new URL(r.request().url()).searchParams.get('cursor') ?? '1');
    const fixture = result(`page-${n}`, n === 12 ? null : String(n + 1));
    fixture.items = Array.from({ length: 50 }, (_, i) => ({
      ...structuredClone(fixture.items[0]!),
      observation: { ...fixture.items[0]!.observation, id: String((n - 1) * 50 + i + 1) },
      basename: `page-${n}-row-${i + 1}`,
    }));
    return r.fulfill({ json: fixture });
  });
  await page.goto('/?q=report');
  for (let i = 1; i < 10; i++) await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByText('500 loaded', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(page.getByRole('link', { name: 'page-11-row-50', exact: true })).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(50);
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(page.getByRole('link', { name: 'page-12-row-50', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Previous page' }).click();
  await expect(page.getByRole('link', { name: 'page-11-row-1', exact: true })).toBeVisible();
  expect(calls).toHaveLength(12);
  expect(detailCalls).toEqual([]);
  expect(new URL(calls[0]!).searchParams.get('limit')).toBe('50');
});

test('IME scheduling, disabled preference persistence and mobile entry do not steal input', async ({
  page,
}) => {
  await ready(page);
  let calls = 0;
  await page.route('**/api/v1/search?**', (r) => {
    calls++;
    return r.fulfill({ json: result('composed') });
  });
  await page.goto('/');
  const input = page.getByRole('searchbox');
  await input.dispatchEvent('compositionstart');
  await input.fill('雪猫犬');
  await input.press('Enter');
  await page.waitForTimeout(250);
  expect(calls).toBe(0);
  await input.dispatchEvent('compositionend');
  await expect(page.getByRole('link', { name: 'composed', exact: true })).toBeVisible();
  expect(calls).toBe(1);
  await page.getByText('Keyboard help', { exact: true }).click();
  await page.getByLabel('Type to search on desktop').uncheck();
  await page.reload();
  await page.getByText('Keyboard help', { exact: true }).click();
  await expect(page.getByLabel('Type to search on desktop')).not.toBeChecked();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await expect(input).not.toBeFocused();
  await page.locator('h1').click();
  await page.keyboard.type('a');
  await expect(input).toHaveValue('');
  await page.keyboard.press('/');
  await expect(input).toBeFocused();
});

test('empty success, unavailable and malformed responses never masquerade as each other', async ({
  page,
}) => {
  await ready(page);
  let response: 'empty' | 'unavailable' | 'malformed' = 'empty';
  await page.route('**/api/v1/search?**', (r) => {
    if (response === 'unavailable')
      return r.fulfill({
        status: 503,
        json: { error: { code: 'unavailable', message: 'offline' } },
      });
    if (response === 'malformed') return r.fulfill({ json: {} });
    const fixture = result('empty');
    fixture.items = [];
    return r.fulfill({ json: fixture });
  });
  await page.goto('/?q=report');
  await expect(page.getByText(/No matching observations/)).toBeVisible();
  response = 'unavailable';
  await page.getByRole('button', { name: 'Reload from first page' }).click();
  await expect(page.getByRole('alert')).toContainText(/unavailable/i);
  await expect(page.getByText(/No matching observations/)).toHaveCount(0);
  response = 'malformed';
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(/unexpected|invalid/i);
  await expect(page.getByText(/No matching observations/)).toHaveCount(0);
});

test('bookmarked membership and bounds are exposed; drafts retire rows and preserve last applied summary', async ({
  page,
}) => {
  await ready(page);
  const calls: URL[] = [];
  await page.route('**/api/v1/search?**', (r) => {
    calls.push(new URL(r.request().url()));
    return r.fulfill({ json: result('bookmarked') });
  });
  await page.goto(
    '/?q=abc&disk_id=1&snapshot_id=1&directory=&replica_metric=locations&min_other_replicas=0&max_other_replicas=2',
  );
  await expect(page.getByRole('link', { name: 'bookmarked', exact: true })).toBeVisible();
  await expect(page.getByLabel('Disk ID', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Other-copy matching')).toHaveValue('range');
  await expect(page.getByLabel('Minimum other copies')).toHaveValue('0');
  await expect(page.locator('.applied-summary')).toContainText(
    'Directory: root · Other locations: ≥ 0 · Other locations: ≤ 2',
  );
  await page.getByText('Advanced filters (5 active)', { exact: true }).click();
  await expect(page.getByLabel('Disk ID', { exact: true })).not.toBeVisible();
  await expect(page.locator('.applied-summary')).toContainText('Disk 1 · Snapshot 1');
  expect(calls).toHaveLength(1);
  await page.getByText('Advanced filters (5 active)', { exact: true }).click();
  await page.getByLabel('Disk ID', { exact: true }).fill('2');
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await expect(page.getByText(/Unapplied changes — previous results retired/)).toBeVisible();
  await expect(page.locator('.applied-summary')).toContainText('Last applied:');
  await expect(page.locator('.applied-summary')).toContainText('Disk 1');
  expect(calls).toHaveLength(1);
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('link', { name: 'bookmarked', exact: true })).toBeVisible();
  await expect(page.locator('.applied-summary')).toContainText('Disk 2');
  expect(calls).toHaveLength(2);
  expect(calls[1]!.searchParams.get('disk_id')).toBe('2');
});

test('history incompatibility is immediately invalid, retains bounds and dispatches only deliberate mode choices', async ({
  page,
}) => {
  await ready(page);
  const calls: URL[] = [];
  await page.route('**/api/v1/search?**', (r) => {
    calls.push(new URL(r.request().url()));
    return r.fulfill({ json: result('valid') });
  });
  await page.goto('/?q=abc&other_replicas=0');
  await expect(page.getByRole('link', { name: 'valid', exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('history');
  await expect(page.getByRole('alert')).toContainText('Other-copy bounds require current scope');
  await expect(page.getByLabel('Exact other copies')).toHaveValue('0');
  await expect(page.getByLabel('Exact other copies')).toHaveAttribute('aria-invalid', 'true');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  expect(calls).toHaveLength(1);
  await page.getByLabel('Other-copy matching').selectOption('any');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('link', { name: 'valid', exact: true })).toBeVisible();
  expect(calls).toHaveLength(2);
  expect(calls[1]!.searchParams.get('scope')).toBe('history');
  expect(calls[1]!.searchParams.has('other_replicas')).toBe(false);
  await page.getByLabel('Other-copy matching').selectOption('exact');
  await expect(page.getByLabel('Exact other copies')).toHaveValue('0');
  await expect(page.getByRole('alert')).toContainText('Other-copy bounds require current scope');
  await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('current');
  await page.getByLabel('Other-copy matching').selectOption('range');
  await expect(page.getByRole('alert')).toContainText('Enter at least one range bound');
  await page.getByLabel('Minimum other copies').fill('2');
  await page.getByLabel('Maximum other copies').fill('1');
  await expect(page.getByRole('alert')).toContainText(
    'Maximum other-copy count must be at least the minimum',
  );
  await page.getByRole('button', { name: 'Apply filters' }).click();
  expect(calls).toHaveLength(2);
  await page.getByLabel('Maximum other copies').fill('3');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('link', { name: 'valid', exact: true })).toBeVisible();
  expect(calls).toHaveLength(3);
  expect(calls[2]!.searchParams.get('min_other_replicas')).toBe('2');
  expect(calls[2]!.searchParams.get('max_other_replicas')).toBe('3');
  expect(calls[2]!.searchParams.has('other_replicas')).toBe(false);
});

test('conflicting and invalid bookmarks expose literal controls without silently dispatching repaired bounds', async ({
  page,
}) => {
  await ready(page);
  let calls = 0;
  await page.route('**/api/v1/search?**', (r) => {
    calls++;
    return r.fulfill({ json: result('repaired') });
  });
  await page.goto('/?q=abc&scope=history&other_replicas=0&min_other_replicas=1');
  await expect(page.getByLabel('Other-copy matching')).toHaveValue('conflict');
  await expect(page.getByLabel('Exact other copies')).toHaveValue('0');
  await expect(page.getByLabel('Minimum other copies')).toHaveValue('1');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  expect(calls).toBe(0);
  await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('current');
  await page.getByLabel('Other-copy matching').selectOption('range');
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await expect(page.getByRole('link', { name: 'repaired', exact: true })).toBeVisible();
  expect(calls).toBe(1);
  await expect(page).toHaveURL(/min_other_replicas=1/);
  expect(new URL(page.url()).searchParams.has('other_replicas')).toBe(false);
  await page.goto('/?q=abc&scope=history&disk_id=bad&other_replicas=0');
  await expect(page.getByLabel('Disk ID', { exact: true })).toHaveValue('bad');
  await expect(page.getByRole('combobox', { name: 'Scope', exact: true })).toHaveValue('history');
  await expect(page.getByLabel('Exact other copies')).toHaveValue('0');
  expect(calls).toBe(1);
});

test('compact path-first results retain literal identities, numeric alignment and accessible selection at approved widths', async ({
  page,
}) => {
  await ready(page);
  const literal =
    'archive/雪 e\u0301/literal ?#% <report>/long-directory-without-shortening/'.repeat(3) +
    'very-long-descriptive-basename.txt';
  const basename = 'very-long-descriptive-basename.txt';
  const fixture = result(basename);
  fixture.items[0]!.observation.path = literal;
  fixture.items[0]!.content.size = '9223372036854775807';
  await page.route('**/api/v1/search?**', (r) => r.fulfill({ json: fixture }));
  const output = '/tmp/opencode/u07-screenshots';
  await mkdir(output, { recursive: true });
  const measurements = [];
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/?q=report');
    await expect(page.getByRole('link', { name: basename, exact: true })).toBeVisible();
    const selection = page.getByRole('button', {
      name: `Select ${basename} — ${literal} — observation 9007199254740993`,
      exact: true,
    });
    await expect(selection).toHaveText('Select');
    await expect(page.locator('.literal-path')).toHaveText(literal);
    await expect(page.locator('.literal-path')).toHaveCSS('white-space', 'pre-wrap');
    await expect(page.locator('.literal-path')).toHaveCSS('overflow-wrap', 'anywhere');
    await expect(page.locator('.size-cell')).toHaveCSS(
      'text-align',
      width > 900 ? 'right' : 'left',
    );
    await expect(page.locator('.copies-cell')).toHaveCSS('font-variant-numeric', 'tabular-nums');
    await expect(page.getByRole('link', { name: 'Observation', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Containing directory' })).toBeVisible();
    if (width < 900) {
      const caption = await page.locator('.search-results caption').boundingBox();
      expect(caption!.width).toBe(width - 32);
      await expect(page.locator('.size-cell .record-label')).toBeVisible();
      await expect(page.locator('.inventory-cell .record-label')).toBeVisible();
      await expect(page.locator('.copies-cell .record-label')).toBeVisible();
    }
    const dimensions = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
      tableY:
        document.querySelector('.search-results')!.getBoundingClientRect().top + window.scrollY,
      firstRowY: document.querySelector('tbody tr')!.getBoundingClientRect().top + window.scrollY,
    }));
    expect(dimensions.documentWidth).toBe(width);
    expect(dimensions.tableY).toBeLessThan(width < 900 ? 460 : 400);
    expect(dimensions.firstRowY).toBeLessThan(width < 900 ? 500 : 480);
    await page.screenshot({ path: `${output}/results-${width}.png`, fullPage: true });
    await selection.click();
    await expect(selection).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('[data-selected-content="true"]')).toHaveText(basename);
    await page.getByRole('button', { name: 'Cancel selection' }).click();
    await expect(selection).toHaveAttribute('aria-pressed', 'false');
    await page.getByText('Advanced filters (0 active)', { exact: true }).click();
    await page.getByLabel('Other-copy matching').selectOption('exact');
    await page.getByLabel('Exact other copies').fill('0');
    const membershipCheckbox = await page.getByLabel('Filter directory membership').boundingBox();
    expect(membershipCheckbox!.width).toBeLessThan(24);
    await page.getByRole('combobox', { name: 'Scope', exact: true }).selectOption('history');
    await expect(page.getByRole('alert')).toContainText('Other-copy bounds require current scope');
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.screenshot({ path: `${output}/invalid-history-${width}.png`, fullPage: true });
    measurements.push({ width, ...dimensions });
  }
  await writeFile(`${output}/measurements.json`, JSON.stringify(measurements, null, 2));
});
