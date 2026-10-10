import { resolve } from 'node:path';
import { test, expect } from '@playwright/test';
import { startCatalog, type Catalog } from '../real/harness';
import { fixture } from '../real/fixture';

let catalog: Catalog;
test.beforeAll(async () => {
  catalog = await startCatalog(undefined, resolve('build'));
});
test.afterAll(async () => {
  await catalog?.close();
});

test('Go alone hosts built assets, real API and literal deep-link reloads', async ({ page }) => {
  expect(catalog.origin).toBe(catalog.backendOrigin);
  const errors: string[] = [];
  const assets: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('response', (response) => {
    expect(new URL(response.url()).origin).toBe(catalog.backendOrigin);
    if (new URL(response.url()).pathname.startsWith('/_app/')) {
      assets.push(response.url());
      if (![200, 304].includes(response.status()))
        errors.push(`Asset ${response.status()} ${response.url()}`);
    }
  });
  await page.goto(catalog.origin);
  await expect(page.getByRole('searchbox')).toBeVisible();
  await page.getByRole('searchbox').fill('report');
  await expect(page.getByRole('link', { name: 'report.txt', exact: true }).first()).toBeVisible();
  await page.getByRole('link', { name: 'report.txt', exact: true }).first().click();
  await expect(page.getByRole('region', { name: 'Content identity' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('region', { name: 'Content identity' })).toBeVisible();
  await page.goto(new URL(`/disks/${catalog.diskID}`, catalog.origin).href);
  await expect(page.getByRole('region', { name: 'Latest complete inventory' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('region', { name: 'Latest complete inventory' })).toBeVisible();
  const snapshot = catalog.snapshots[0];
  if (!snapshot) throw new Error('Imported snapshot missing');
  const deep = new URL(`/snapshots/${snapshot.id}/directory`, catalog.origin);
  deep.searchParams.set('path', '');
  await page.goto(deep.href);
  await expect(page.getByRole('region', { name: 'Directory summary' })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('region', { name: 'Directory summary' })).toBeVisible();
  deep.searchParams.set('path', fixture.directory);
  await page.goto(deep.href);
  await expect(page.getByRole('table', { name: 'Directory entries' })).toContainText('report.txt');
  await page.reload();
  await expect(page.getByRole('table', { name: 'Directory entries' })).toContainText('report.txt');
  for (const path of ['/_app/missing.js', '/unknown', '/api/v1/missing', '/api/v1/disks/999999']) {
    const response = await page.request.get(new URL(path, catalog.origin).href);
    expect(response.status()).toBe(404);
    expect(response.headers()['content-type']).not.toContain('text/html');
  }
  const invalid = await page.request.get(new URL('/api/v1/search?q=x', catalog.origin).href);
  expect(invalid.status()).toBe(400);
  expect(invalid.headers()['content-type']).toBe('application/json');
  expect(assets.length).toBeGreaterThan(0);
  expect(errors).toEqual([]);
});
