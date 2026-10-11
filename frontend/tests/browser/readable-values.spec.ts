import { expect, test, type Page } from '@playwright/test';
import { diskFixture, searchFixture } from '../fixtures/api';
import { observationFixture } from '../fixtures/details';

async function ready(page: Page) {
  await page.route('**/healthz', (route) => route.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (route) =>
    route.fulfill({
      json: {
        revision: '9007199254740993',
        scope: 'current',
        disk_count: '1',
        file_count: '3',
        content_count: '3',
      },
    }),
  );
}

test('search sizes distinguish unknown, zero and rounded maximum with keyboard exact bytes', async ({
  page,
}) => {
  await ready(page);
  const fixture = searchFixture();
  fixture.items = [null, '0', '9223372036854775807'].map((size, index) => {
    const item = structuredClone(searchFixture().items[0]!);
    item.observation.id = String(index + 1);
    item.basename = `report-${index}`;
    item.content.size = size;
    return item;
  });
  let calls = 0;
  await page.route('**/api/v1/search?**', (route) => {
    calls++;
    return route.fulfill({ json: fixture });
  });
  await page.goto('/?q=report');
  const rows = page.locator('tbody tr');
  await expect(rows).toHaveCount(3);
  await expect(rows.nth(0).locator('td').nth(2).locator('.size-value')).toHaveText('Unknown');
  await expect(rows.nth(1).locator('td').nth(2).locator('.size-value')).toHaveText('0 B');
  const size = rows.nth(2).locator('.size-value');
  await expect(size.locator('summary')).toHaveText('≈ 8 EiB');
  await expect(size.locator('summary')).toHaveAccessibleName('Approximately 8 EiB; exact bytes');
  await expect(size.locator('code')).not.toBeVisible();
  await size.locator('summary').focus();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await expect(size.locator('summary')).toBeFocused();
  expect(
    await size.locator('summary').evaluate((node) => getComputedStyle(node).outlineStyle),
  ).toBe('solid');
  await page.keyboard.press('Enter');
  await expect(size.locator('code')).toHaveText('9,223,372,036,854,775,807 B');
  await expect(size.locator('code')).toBeVisible();
  await page.keyboard.press('Space');
  await expect(size.locator('code')).not.toBeVisible();
  expect(calls).toBe(1);
});

test('dates retain meaning, UTC, nanoseconds and epoch zero without separate disclosure rows', async ({
  page,
}, testInfo) => {
  await ready(page);
  const fixture = observationFixture();
  fixture.content.size = '9007199254740993';
  fixture.observation.mtime = '1970-01-01T00:00:00.000000001Z';
  fixture.snapshot.captured_at = '2026-10-11T23:59:58.123456789Z';
  fixture.snapshot.imported_at = '2026-10-12T00:00:00Z';
  await page.route('**/api/v1/observations/9007199254740993', (route) =>
    route.fulfill({ json: fixture }),
  );
  await page.goto('/observations/9007199254740993');
  const dates = page.locator('.date-value');
  await expect(dates).toHaveCount(3);
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.locator('main').screenshot({ path: testInfo.outputPath(`compact-${width}.png`) });
  }
  for (const [index, label, exact] of [
    [0, 'Source mtime', fixture.observation.mtime],
    [1, 'Captured at', fixture.snapshot.captured_at],
    [2, 'Imported at', fixture.snapshot.imported_at],
  ] as const) {
    const date = dates.nth(index);
    await expect(date.locator('summary')).toContainText(label);
    await expect(date.locator('summary')).toContainText('UTC');
    await expect(date.locator('time')).toHaveAttribute('datetime', exact);
    await expect(date.locator('code')).not.toBeVisible();
    await date.locator('summary').focus();
    await page.keyboard.press('Enter');
    await expect(date.locator('code')).toHaveText(exact);
    await expect(date.locator('code')).toBeVisible();
  }
  await expect(dates.nth(0).locator('summary')).toContainText('00:00:00');
  await expect(dates.nth(1).locator('summary')).toContainText('23:59:58');
  await page.locator('.size-value summary').click();
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.locator('main').screenshot({ path: testInfo.outputPath(`precision-${width}.png`) });
  }
});

test('declared capacity is decimal while incomplete catalog subtotal stays binary and total unknown', async ({
  page,
}, testInfo) => {
  await ready(page);
  const disk = diskFixture();
  disk.id = '1';
  const snapshot = searchFixture().items[0]!.snapshot;
  snapshot.file_count = '3';
  snapshot.content_count = '3';
  disk.latest_snapshot = snapshot;
  disk.cataloged = {
    known_bytes: '9007199254740993',
    size_complete: false,
    file_count: '3',
    content_count: '3',
    unknown_size_file_count: '1',
  };
  await page.route('**/api/v1/disks/1', (route) => route.fulfill({ json: disk }));
  await page.route('**/api/v1/disks/1/snapshots?**', (route) =>
    route.fulfill({
      json: {
        disk_id: '1',
        revision: '9007199254740993',
        items: [snapshot],
        next_cursor: null,
      },
    }),
  );
  await page.goto('/disks/1');
  const capacity = page
    .locator('dt')
    .filter({ hasText: /^Declared capacity$/ })
    .locator('+ dd');
  await expect(capacity.locator('summary')).toHaveText('≈ 9.22 EB');
  await capacity.locator('summary').click();
  await expect(capacity.locator('code')).toHaveText('9,223,372,036,854,775,807 B');
  const cataloged = page.getByRole('region', { name: 'Cataloged inventory size' });
  await expect(cataloged.locator('summary')).toHaveText('≈ 8 PiB');
  await expect(
    cataloged
      .locator('dt')
      .filter({ hasText: /^Cataloged total$/ })
      .locator('+ dd'),
  ).toHaveText('Unknown');
  await expect(cataloged).toContainText('Incomplete');
  await expect(cataloged).toContainText('Known cataloged byte subtotal');
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.locator('main').screenshot({ path: testInfo.outputPath(`subtotal-${width}.png`) });
  }
  disk.capacity = '0';
  disk.cataloged.known_bytes = '0';
  await page.reload();
  await expect(capacity).toHaveText('0 B');
  await expect(
    cataloged
      .locator('dt')
      .filter({ hasText: /^Known cataloged byte subtotal$/ })
      .locator('+ dd'),
  ).toHaveText('0 B');
  await expect(
    cataloged
      .locator('dt')
      .filter({ hasText: /^Cataloged total$/ })
      .locator('+ dd'),
  ).toHaveText('Unknown');
});

test('exact scaled values have no approximation marker but still expose exact bytes', async ({
  page,
}) => {
  await ready(page);
  const fixture = observationFixture();
  fixture.content.size = '1536';
  await page.route('**/api/v1/observations/9007199254740993', (route) =>
    route.fulfill({ json: fixture }),
  );
  await page.goto('/observations/9007199254740993');
  await expect(page.locator('.size-value summary')).toHaveText('1.5 KiB');
  await expect(page.locator('.size-value summary')).toHaveAccessibleName('1.5 KiB; exact bytes');
  await page.locator('.size-value summary').click();
  await expect(page.locator('.size-value code')).toHaveText('1,536 B');
  await expect(page.getByText('Source mtime: Unknown', { exact: true })).toBeVisible();
});
