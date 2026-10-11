import { expect, test, type Page } from '@playwright/test';
import { diskFixture } from '../fixtures/api';

async function ready(page: Page) {
  await page.route('**/healthz', (route) => route.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (route) =>
    route.fulfill({
      json: {
        revision: '3',
        scope: 'current',
        disk_count: '99',
        file_count: '99',
        content_count: '99',
      },
    }),
  );
}

test('retained paging failure retries the same cursor; reconnect requires an explicit first-page reload', async ({
  page,
}) => {
  await ready(page);
  const cursors: (string | null)[] = [];
  let failure: number | undefined;
  await page.route('**/api/v1/disks?**', (route) => {
    const cursor = new URL(route.request().url()).searchParams.get('cursor');
    cursors.push(cursor);
    if (failure)
      return route.fulfill({
        status: failure,
        json: { error: { code: 'internal', message: 'failed' } },
      });
    const n = Number(cursor ?? 1);
    return route.fulfill({
      json: {
        revision: '3',
        items: [{ ...diskFixture(), id: String(n), label: `Retained disk ${n}` }],
        next_cursor: String(n + 1),
      },
    });
  });
  await page.goto('/disks?limit=1');
  for (let n = 2; n <= 10; n++) {
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.locator('tbody tr')).toHaveCount(n);
  }
  const controls = page.getByRole('navigation', { name: 'Result pages' });
  await expect(controls).toContainText('10 loaded');
  await expect(controls).toContainText('Next shows one page at a time');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(controls).toContainText('Page 11. 1 loaded on this page.');
  failure = 500;
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('main [role="alert"]')).toContainText('not freshly verified');
  await expect(page.locator('tbody tr')).toContainText('Retained disk 11');
  await expect(page.getByRole('button', { name: 'Reload results', exact: true })).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: 'Reload from first page', exact: true }),
  ).toHaveCount(1);
  failure = undefined;
  await page.locator('main').getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(controls).toContainText('Page 12. 1 loaded on this page.');
  expect(cursors.slice(-2)).toEqual(['12', '12']);
  failure = 503;
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('main [role="alert"]')).toContainText('Backend unavailable');
  const reads = cursors.length;
  await page.getByRole('button', { name: 'Previous page', exact: true }).click();
  await expect(controls).toContainText('Page 11.');
  await expect(page.locator('main [role="alert"]')).toContainText('not freshly verified');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(controls).toContainText('Page 12.');
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeDisabled();
  expect(cursors).toHaveLength(reads);
  failure = undefined;
  await page.getByRole('button', { name: 'Retry connection', exact: true }).click();
  await expect(page.getByText('Data invalidated; reload results.', { exact: true })).toBeVisible();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  expect(cursors).toHaveLength(reads);
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.locator('tbody tr')).toContainText('Retained disk 1');
  expect(cursors.at(-1)).toBeNull();
});

test('retained failure can deliberately restart without reusing the continuation cursor', async ({
  page,
}) => {
  await ready(page);
  const cursors: (string | null)[] = [];
  await page.route('**/api/v1/disks?**', (route) => {
    const cursor = new URL(route.request().url()).searchParams.get('cursor');
    cursors.push(cursor);
    if (cursor)
      return route.fulfill({
        status: 500,
        json: { error: { code: 'internal', message: 'failed' } },
      });
    return route.fulfill({
      json: { revision: '3', items: [diskFixture()], next_cursor: 'opaque' },
    });
  });
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.locator('main [role="alert"]')).toContainText('not freshly verified');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('button', { name: 'Reload from first page', exact: true }).click();
  await expect(page.locator('main [role="alert"]')).toHaveCount(0);
  await expect(page.locator('tbody tr')).toHaveCount(1);
  expect(cursors).toEqual([null, 'opaque', null]);
});

test('revision mismatch retires rows and offers exactly one scoped reload, not an empty result', async ({
  page,
}) => {
  await ready(page);
  const cursors: (string | null)[] = [];
  await page.route('**/api/v1/disks?**', (route) => {
    const cursor = new URL(route.request().url()).searchParams.get('cursor');
    cursors.push(cursor);
    return route.fulfill({
      json: {
        revision: cursor ? '4' : '3',
        items: [diskFixture()],
        next_cursor: 'next',
      },
    });
  });
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.locator('main [role="alert"]')).toContainText('Inventory changed');
  await expect(page.getByRole('button', { name: 'Reload results', exact: true })).toHaveCount(1);
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await expect(page.getByText('No disks cataloged', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  expect(cursors).toEqual([null, 'next', null]);
});

test('loading actions keep their dimensions and loaded counts wrap at approved widths', async ({
  page,
}) => {
  await ready(page);
  let release!: () => void;
  await page.route('**/api/v1/disks?**', async (route) => {
    const number = Number(new URL(route.request().url()).searchParams.get('cursor') ?? 1);
    if (number === 2 || number === 11)
      await new Promise<void>((resolve) => {
        release = resolve;
      });
    await route.fulfill({
      json: {
        revision: '3',
        items: [{ ...diskFixture(), id: String(number) }],
        next_cursor: number === 11 ? null : String(number + 1),
      },
    });
  });
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto('/disks');
    const button = page.getByRole('button', { name: 'Load more', exact: true });
    const before = await button.boundingBox();
    await button.click();
    await expect(button).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Reload results', exact: true })).toBeDisabled();
    await expect(page.getByRole('navigation', { name: 'Result pages' })).toHaveAttribute(
      'aria-busy',
      'true',
    );
    const during = await button.boundingBox();
    expect(during!.width).toBe(before!.width);
    expect(during!.height).toBe(before!.height);
    expect(during!.height).toBeGreaterThanOrEqual(32);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.screenshot({
      path: `/tmp/opencode/u13-screenshots/loading-${width}.png`,
      fullPage: true,
    });
    release();
    await expect(page.getByRole('navigation', { name: 'Result pages' })).toContainText('2 loaded');
    for (let n = 3; n <= 10; n++) {
      await button.click();
      await expect(page.locator('tbody tr')).toHaveCount(n);
    }
    const next = page.getByRole('button', { name: 'Next page', exact: true });
    const nextBefore = await next.boundingBox();
    await next.click();
    await expect(next).toBeDisabled();
    const nextDuring = await next.boundingBox();
    expect(nextDuring!.width).toBe(nextBefore!.width);
    expect(nextDuring!.height).toBe(nextBefore!.height);
    await page.screenshot({
      path: `/tmp/opencode/u13-screenshots/transition-${width}.png`,
      fullPage: true,
    });
    release();
    await expect(page.getByRole('navigation', { name: 'Result pages' })).toContainText(
      'Page 11. 1 loaded on this page.',
    );
    await expect(next).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Previous page', exact: true })).toBeEnabled();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.screenshot({
      path: `/tmp/opencode/u13-screenshots/retained-${width}.png`,
      fullPage: true,
    });
  }
});
