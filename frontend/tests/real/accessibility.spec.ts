import AxeBuilder from '@axe-core/playwright';
import type { Page, TestInfo } from '@playwright/test';
import { test, expect } from './test';

async function audit(page: Page, info: TestInfo, name: string) {
  await expect(page.getByRole('banner')).toHaveCount(1);
  await expect(page.getByRole('main')).toHaveCount(1);
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  await info.attach(`${name}-axe`, {
    body: JSON.stringify({ violations: results.violations, incomplete: results.incomplete }),
    contentType: 'application/json',
  });
  expect(results.violations, name).toEqual([]);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    `${name}: no document-wide horizontal scrolling`,
  ).toBe(true);
}

for (const width of [1280, 320]) {
  test(`integrated landmarks, labels, contrast and layout at ${width}px`, async ({
    page,
    catalog,
  }, info) => {
    test.setTimeout(120_000);
    await page.setViewportSize({ width, height: 900 });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/?q=report');
    await expect(page.locator('tbody tr')).toHaveCount(4);
    await audit(page, info, 'search');
    await page.getByRole('link', { name: 'report.txt', exact: true }).first().click();
    await expect(page.locator('tbody tr')).toHaveCount(5);
    await audit(page, info, 'content');
    await page
      .getByRole('link', { name: /^Observation / })
      .first()
      .click();
    await expect(page.getByRole('link', { name: /^Disk / })).toBeVisible();
    await audit(page, info, 'observation');
    const screens = [
      ['contents', '/contents'],
      ['disks-create', '/disks'],
      ['disk', `/disks/${catalog.diskID}`],
      ['snapshot', `/snapshots/${catalog.snapshots[0]!.id}`],
      ['directory', `/snapshots/${catalog.snapshots[0]!.id}/directory?path=foo`],
    ];
    for (const [name, path] of screens) {
      await page.goto(path!);
      await expect(page.getByRole('status').filter({ hasText: 'Backend ready' })).toBeVisible();
      await expect(page.getByRole('status').filter({ hasText: /Loading/ })).toHaveCount(0);
      await audit(page, info, name!);
      if (name === 'disk') {
        await page.getByRole('button', { name: 'Edit disk' }).click();
        await audit(page, info, 'disk-edit');
      }
    }
    for (const name of ['Exact tree copies', 'Content on other disks']) {
      await page.getByRole('button', { name, exact: true }).click();
      await page.getByRole('button', { name: 'Add allow rule' }).click();
      await page.getByLabel('Allow pattern 1').fill('**');
      await audit(page, info, name);
      await page.getByRole('button', { name: 'Apply and compare' }).click();
      await expect(page.getByText('Applied rules', { exact: true })).toBeVisible();
      await audit(page, info, `${name}-results`);
    }
    await info.attach(`directory-${width}`, {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });
  });
}
