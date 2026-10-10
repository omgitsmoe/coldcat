import { test, expect } from './test';

test('real disk create, conflict, metadata-only rename, precise capacity and null clearing', async ({
  page,
  catalog,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/disks');
  const catalogBefore = await page.request.get(new URL('/api/v1/catalog', catalog.origin).href);
  const revision = (await catalogBefore.json()).revision;
  await page.getByLabel('Label', { exact: true }).fill('F7 browser-created disk');
  await page.getByLabel('Capacity in bytes').fill('9007199254740993');
  await page.getByLabel('Serial', { exact: true }).fill('write serial');
  await page.getByLabel('Notes', { exact: true }).fill('write notes');
  const postResponse = page.waitForResponse(
    (r) => r.request().method() === 'POST' && r.url().endsWith('/api/v1/disks'),
  );
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  expect((await postResponse).status()).toBe(201);
  await expect(page.getByRole('status').filter({ hasText: 'Disk created' })).toBeVisible();
  await page.getByRole('link', { name: 'Open verified disk' }).click();
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  const disks = await (
    await page.request.get(new URL('/api/v1/disks', catalog.origin).href)
  ).json();
  const other = disks.items.find(
    (disk: { label: string }) => disk.label !== 'F7 browser-created disk',
  );
  await page.getByLabel('Label', { exact: true }).fill(other.label);
  const conflict = page.waitForResponse((r) => r.request().method() === 'PATCH');
  await page.getByRole('button', { name: 'Save changes' }).click();
  expect((await conflict).status()).toBe(409);
  await expect(page.getByRole('alert')).toContainText('Conflict');
  await expect(page.getByLabel('Label', { exact: true })).toHaveValue(other.label);
  await page.getByLabel('Label', { exact: true }).fill('F7 renamed disk');
  await page.getByLabel('Capacity in bytes').fill('9223372036854775807');
  await page.getByLabel('Serial', { exact: true }).fill('');
  await page.getByLabel('Notes', { exact: true }).fill('');
  const patchResponse = page.waitForResponse((r) => r.request().method() === 'PATCH');
  await page.getByRole('button', { name: 'Save changes' }).click();
  const response = await patchResponse;
  expect(response.status()).toBe(200);
  expect(response.request().postDataJSON()).toEqual({
    label: 'F7 renamed disk',
    capacity: '9223372036854775807',
    serial: null,
    notes: null,
  });
  await expect(page.getByRole('status').filter({ hasText: 'Disk updated' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Disk metadata' })).toContainText(
    '9,223,372,036,854,775,807 B',
  );
  const stored = await (await page.request.get(response.url())).json();
  expect(stored).toMatchObject({
    label: 'F7 renamed disk',
    capacity: '9223372036854775807',
    serial: null,
    notes: null,
  });
  const catalogAfter = await page.request.get(new URL('/api/v1/catalog', catalog.origin).href);
  expect((await catalogAfter.json()).revision).toBe(revision);
  expect(errors).toEqual([]);
});
