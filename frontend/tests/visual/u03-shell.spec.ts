import { mkdir, writeFile } from 'node:fs/promises';
import { expect, test } from '@playwright/test';
import { shellFixtures } from '../fixtures/shell';

test('workspace cap and form width remain independent of viewport width', async ({ page }) => {
  await shellFixtures(page);
  await page.setViewportSize({ width: 1920, height: 900 });
  await page.goto('/');
  await expect(page.locator('.shell-connection')).toHaveText('Backend ready');
  const dimensions = await page.evaluate(() => {
    const main = document.querySelector<HTMLElement>('.shell-main')!;
    const bar = document.querySelector<HTMLElement>('.shell-bar')!;
    const form = document.createElement('div');
    form.className = 'form-body';
    main.append(form);
    const formWidth = form.getBoundingClientRect().width;
    form.remove();
    return {
      mainWidth: main.clientWidth,
      barWidth: bar.clientWidth,
      mainLeft: main.getBoundingClientRect().left,
      barLeft: bar.getBoundingClientRect().left,
      formWidth,
    };
  });
  expect(dimensions).toEqual({
    mainWidth: 1648,
    barWidth: 1648,
    mainLeft: 136,
    barLeft: 136,
    formWidth: 720,
  });
});

test('compact shell wraps connection states at approved desktop widths', async ({
  browser,
}, info) => {
  const output = info.outputPath('screenshots');
  await mkdir(output, { recursive: true });
  const measurements = [];
  for (const width of [1440, 1024, 768]) {
    for (const state of ['ready', 'checking', 'unavailable']) {
      const context = await browser.newContext({
        baseURL: 'http://127.0.0.1:4173',
        viewport: { width, height: 900 },
        locale: 'en-US',
        timezoneId: 'UTC',
        deviceScaleFactor: 1,
      });
      const page = await context.newPage();
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await page.clock.install();
      await page.clock.pauseAt(new Date());
      const calls = await shellFixtures(page);
      if (state !== 'ready') {
        await page.route('**/healthz', (route) => {
          calls.push('/healthz');
          if (state === 'unavailable') return route.fulfill({ status: 503, body: 'Unavailable' });
        });
      }
      await page.goto('/');
      await expect(page.locator('.shell-connection')).toContainText(
        state === 'ready'
          ? 'Backend ready'
          : state === 'checking'
            ? 'Checking backend…'
            : 'Backend unavailable.',
      );
      const dimensions = await page.evaluate(() => {
        const bar = document.querySelector<HTMLElement>('.shell-bar')!;
        const main = document.querySelector<HTMLElement>('.shell-main')!;
        const form = document.createElement('div');
        form.className = 'form-body';
        main.append(form);
        const formWidth = form.getBoundingClientRect().width;
        form.remove();
        const header = document.querySelector('header')!.getBoundingClientRect();
        const items = [...bar.querySelectorAll('a, p, button')].map((element) => {
          const rect = element.getBoundingClientRect();
          return {
            text: element.textContent,
            left: rect.left,
            right: rect.right,
            top: rect.top,
            bottom: rect.bottom,
          };
        });
        return {
          documentWidth: document.documentElement.scrollWidth,
          headerHeight: header.height,
          mainGutter: getComputedStyle(main).paddingLeft,
          barGutter: getComputedStyle(bar).paddingLeft,
          formWidth,
          items,
        };
      });
      expect(dimensions.documentWidth).toBe(width);
      expect(dimensions.mainGutter).toBe(width === 768 ? '16px' : '24px');
      expect(dimensions.barGutter).toBe(dimensions.mainGutter);
      expect(dimensions.formWidth).toBe(720);
      for (const item of dimensions.items) {
        expect(item.left).toBeGreaterThanOrEqual(width === 768 ? 16 : 24);
        expect(item.right).toBeLessThanOrEqual(width - (width === 768 ? 16 : 24));
        expect(item.top).toBeGreaterThanOrEqual(0);
        expect(item.bottom).toBeLessThanOrEqual(dimensions.headerHeight);
      }
      if (state !== 'unavailable') expect(dimensions.headerHeight).toBe(57);
      if (state === 'unavailable' && width === 768) {
        expect(dimensions.headerHeight).toBeGreaterThan(57);
        await expect(page.getByRole('button', { name: 'Retry connection' })).toBeVisible();
      }
      expect(calls).toEqual(state === 'ready' ? ['/healthz', '/api/v1/catalog'] : ['/healthz']);
      expect(errors).toEqual([]);
      const screenshot = `${state}-${width}.png`;
      await page.screenshot({
        path: `${output}/${screenshot}`,
        clip: { x: 0, y: 0, width, height: 240 },
      });
      measurements.push({ state, width, screenshot, ...dimensions });
      await context.close();
    }
  }
  await writeFile(
    `${output}/manifest.json`,
    JSON.stringify({ browser: browser.version(), measurements }, null, 2) + '\n',
  );
});
