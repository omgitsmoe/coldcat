import { expect, test } from '@playwright/test';
import { shellFixtures } from '../fixtures/shell';

test('section ownership includes nested detail routes and excludes unknown routes', async ({
  page,
}) => {
  await shellFixtures(page);
  const cases = [
    ['/', 'Search', 'page'],
    ['/contents', 'Contents', 'page'],
    ['/contents/1', 'Contents', 'location'],
    ['/observations/1', 'Contents', 'location'],
    ['/disks', 'Disks', 'page'],
    ['/disks/1', 'Disks', 'location'],
    ['/snapshots/1', 'Disks', 'location'],
    ['/snapshots/1/directory?path=literal%2F%E9%9B%AA&view=coverage', 'Disks', 'location'],
  ];
  for (const [path, section, current] of cases) {
    await page.goto(path!);
    await expect(page.locator('.shell-connection')).toHaveText('Backend ready');
    const navigation = page.getByRole('navigation', { name: 'Main navigation' });
    await expect(navigation.locator('[aria-current]')).toHaveCount(1);
    await expect(navigation.getByRole('link', { name: section })).toHaveAttribute(
      'aria-current',
      current!,
    );
  }
  await page.goto('/unknown/nested');
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible();
  await expect(page.locator('.shell-navigation [aria-current]')).toHaveCount(0);
});

test('native navigation retains connection ownership, skip link and global search shortcut', async ({
  page,
}) => {
  const calls = await shellFixtures(page);
  await page.goto('/unknown');
  await expect(page.locator('.shell-connection')).toHaveText('Backend ready');
  await page.keyboard.press('Tab');
  const skip = page.getByRole('link', { name: 'Skip to main content' });
  await expect(skip).toBeFocused();
  await expect(skip).toBeVisible();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('main')).toBeFocused();
  const navigation = page.getByRole('navigation', { name: 'Main navigation' });
  await navigation.getByRole('link', { name: 'Disks' }).click();
  await expect(navigation.getByRole('link', { name: 'Disks' })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await expect(page.getByText('Fixture resource not found', { exact: false })).toBeVisible();
  await page.getByRole('main').focus();
  await page.keyboard.press('Control+k');
  await expect(page.getByRole('searchbox')).toBeFocused();
  await expect(navigation.getByRole('link', { name: 'Search' })).toHaveAttribute(
    'aria-current',
    'page',
  );
  expect(calls).toEqual(['/healthz', '/api/v1/catalog', '/api/v1/disks']);
});

test('checking never implies readiness; outage retry uses the existing connection owner', async ({
  page,
}) => {
  await page.clock.install();
  await page.clock.pauseAt(new Date());
  const calls = await shellFixtures(page);
  let releaseHealth: (() => void) | undefined;
  const healthGate = new Promise<void>((resolve) => {
    releaseHealth = resolve;
  });
  await page.route('**/healthz', async (route) => {
    calls.push('/healthz');
    await healthGate;
    await route.fulfill({ json: { status: 'ready' } });
  });
  let available = false;
  await page.route('**/api/v1/catalog', (route) => {
    if (available) return route.fallback();
    calls.push('/api/v1/catalog');
    return route.fulfill({ status: 503, body: 'Unavailable' });
  });
  await page.goto('/');
  await expect(page.locator('.shell-connection')).toHaveText('Checking backend…');
  await expect(page.getByRole('button', { name: 'Retry connection' })).toHaveCount(0);
  expect(calls).toEqual(['/healthz']);
  releaseHealth!();
  await expect(page.locator('.shell-connection')).toContainText('Backend unavailable.');
  await expect(page.locator('.shell-connection')).toHaveClass(/unavailable/);
  available = true;
  await page.getByRole('button', { name: 'Retry connection' }).click();
  await expect(page.locator('.shell-connection')).toHaveText('Backend ready');
  await expect(page.getByRole('button', { name: 'Retry connection' })).toHaveCount(0);
  expect(calls).toEqual(['/healthz', '/api/v1/catalog', '/healthz', '/api/v1/catalog']);
});
