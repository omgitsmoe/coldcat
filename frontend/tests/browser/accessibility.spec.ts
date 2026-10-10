import { expect, test, type Locator, type Page, type Route } from '@playwright/test';
import { searchFixture } from '../fixtures/api';
import { contentFixture, locationsFixture, observationFixture } from '../fixtures/details';

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

async function tabTo(page: Page, control: Locator) {
  for (let i = 0; i < 100; i++) {
    if (await control.evaluate((el) => el === document.activeElement)) {
      expect(await control.evaluate((el) => getComputedStyle(el).outlineStyle)).not.toBe('none');
      return;
    }
    await page.keyboard.press('Tab');
  }
  throw new Error('Core action unreachable by Tab');
}

test('native keyboard workflow, skip link, focus and forced-color/reduced-motion controls', async ({
  page,
}) => {
  await ready(page);
  await page.emulateMedia({ forcedColors: 'active', reducedMotion: 'reduce' });
  await page.route('**/api/v1/search?**', (r) => r.fulfill({ json: searchFixture() }));
  await page.route('**/api/v1/contents/1**', (r) =>
    r.fulfill({
      json: new URL(r.request().url()).pathname.endsWith('/observations')
        ? locationsFixture()
        : contentFixture(),
    }),
  );
  await page.route('**/api/v1/observations/*', (r) => r.fulfill({ json: observationFixture() }));
  await page.goto('/');
  await expect(page.getByRole('searchbox')).toBeFocused();
  expect(await page.evaluate(() => matchMedia('(forced-colors: active)').matches)).toBe(true);
  expect(
    await page.evaluate(() =>
      [...document.querySelectorAll('*')].every((el) => {
        const style = getComputedStyle(el);
        return style.animationDuration === '0s' && style.transitionDuration === '0s';
      }),
    ),
  ).toBe(true);
  await page.keyboard.type('report');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await tabTo(page, page.getByRole('link', { name: 'Skip to main content' }));
  await page.keyboard.press('Enter');
  await expect(page.getByRole('main')).toBeFocused();
  await tabTo(page, page.locator('tbody a').first());
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: /^Content \d+$/ })).toBeVisible();
  await tabTo(page, page.getByRole('link', { name: /^Observation / }).first());
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: /^Observation \d+$/ })).toBeVisible();
  await tabTo(page, page.getByRole('link', { name: 'Containing directory' }));
  await expect(page.getByRole('link', { name: 'Containing directory' })).toHaveAttribute(
    'href',
    /\/snapshots\/1\/directory\?path=/,
  );
});

test('delayed requests preserve typing and navigation; loading, empty and failure are live', async ({
  page,
}) => {
  await ready(page);
  const pending: Route[] = [];
  await page.route('**/api/v1/search?**', (r) => {
    pending.push(r);
  });
  await page.route('**/api/v1/disks?**', (r) =>
    r.fulfill({ json: { revision: '9007199254740993', items: [], next_cursor: null } }),
  );
  await page.goto('/');
  await expect(page.getByRole('searchbox')).toBeFocused();
  await page.keyboard.type('first');
  await expect.poll(() => pending.length).toBe(1);
  await expect(page.getByRole('status').filter({ hasText: 'Searching…' })).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'Still working' })).toBeVisible();
  await page.keyboard.type('-latest');
  await expect(page.getByRole('searchbox')).toHaveValue('first-latest');
  await expect.poll(() => pending.length).toBe(2);
  const empty = searchFixture();
  empty.items = [];
  await pending[1]!.fulfill({ json: empty });
  await expect(
    page.getByRole('status').filter({ hasText: 'No matching observations' }),
  ).toBeVisible();
  await page.getByRole('searchbox').fill('failure');
  await expect.poll(() => pending.length).toBe(3);
  await pending[2]!.fulfill({
    status: 400,
    json: { error: { code: 'invalid_request', message: 'Correct the query' } },
  });
  await expect(page.getByRole('alert')).toContainText('Correct the query');
  await page.getByRole('searchbox').fill('waiting');
  await expect.poll(() => pending.length).toBe(4);
  await tabTo(
    page,
    page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Disks' }),
  );
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Disks', exact: true })).toBeVisible();
  await pending[0]!.fulfill({ json: searchFixture() });
  await pending[3]!.fulfill({ json: searchFixture() });
  await expect(page.getByRole('searchbox')).toHaveCount(0);
});
