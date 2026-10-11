import { expect, test, type Page } from '@playwright/test';
import { diskFixture, searchFixture } from '../fixtures/api';
import { observationFixture } from '../fixtures/details';

async function setup(page: Page) {
  const disk = { ...diskFixture(), id: '1' };
  const writes: { method: string; body: Record<string, unknown> }[] = [];
  let reads = 0;
  let lists = 0;
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
  await page.route('**/api/v1/disks/1/snapshots?**', (r) =>
    r.fulfill({
      json: {
        revision: '9007199254740993',
        disk_id: '1',
        items: [],
        next_cursor: null,
      },
    }),
  );
  await page.route('**/api/v1/disks?**', (r) => {
    lists++;
    return r.fulfill({
      json: {
        revision: '9007199254740993',
        items: [disk],
        next_cursor: null,
      },
    });
  });
  await page.route('**/api/v1/disks/1', (r) => {
    if (r.request().method() === 'PATCH') {
      const body = r.request().postDataJSON();
      writes.push({ method: 'PATCH', body });
      Object.assign(disk, body);
    } else reads++;
    return r.fulfill({ json: disk });
  });
  return { disk, writes, reads: () => reads, lists: () => lists };
}

for (const [amount, unit, bytes] of [
  ['1.5', 'TB', '1500000000000'],
  ['1', 'TiB', '1099511627776'],
  ['0', 'B', '0'],
  ['9223372036854775807', 'B', '9223372036854775807'],
] as const) {
  test(`create sends exact ${amount} ${unit} bytes`, async ({ page }) => {
    const ctx = await setup(page);
    await page.route('**/api/v1/disks', (r) => {
      const body = r.request().postDataJSON();
      ctx.writes.push({ method: 'POST', body });
      Object.assign(ctx.disk, body);
      return r.fulfill({ status: 201, json: ctx.disk });
    });
    await page.goto('/disks');
    await page.getByRole('button', { name: 'Create disk', exact: true }).click();
    await page.getByLabel('Label', { exact: true }).fill('unit-aware disk');
    await page.getByLabel('Capacity amount').fill(amount);
    await page.getByLabel('Capacity unit').selectOption(unit);
    await expect(page.getByText(`Exact bytes: ${bytes}`, { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Create disk', exact: true }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Disk created' })).toBeVisible();
    expect(ctx.writes).toEqual([
      {
        method: 'POST',
        body: { label: 'unit-aware disk', capacity: bytes, serial: null, notes: null },
      },
    ]);
  });
}

test('unit selector immediately reinterprets amount and validation describes the capacity field', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.route('**/api/v1/disks', (r) => {
    ctx.writes.push({ method: r.request().method(), body: r.request().postDataJSON() });
    return r.fulfill({
      status: 400,
      json: { error: { code: 'invalid_request', message: 'Unexpected invalid-capacity write' } },
    });
  });
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  const amount = page.getByLabel('Capacity amount');
  const unit = page.getByLabel('Capacity unit');
  await page.getByLabel('Label', { exact: true }).fill('validation disk');
  await expect(unit.locator('option')).toHaveText([
    'B',
    'KB',
    'MB',
    'GB',
    'TB',
    'KiB',
    'MiB',
    'GiB',
    'TiB',
  ]);
  await amount.fill('1');
  await unit.selectOption('TB');
  await expect(page.getByText('Exact bytes: 1000000000000', { exact: true })).toBeVisible();
  await unit.selectOption('TiB');
  await expect(amount).toHaveValue('1');
  await expect(page.getByText('Exact bytes: 1099511627776', { exact: true })).toBeVisible();
  await expect(unit).toHaveAccessibleDescription(/Changing the unit reinterprets/);
  await unit.selectOption('B');
  for (const invalid of ['-1', '1e3', '1,000', '1_000', '0.5', '9223372036854775808']) {
    await amount.fill(invalid);
    await expect(amount).toHaveAttribute('aria-invalid', 'true');
    await expect(amount).toHaveAccessibleDescription(/Capacity (must|exceeds)/);
    await expect(page.getByText('Exact bytes: —', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Create disk', exact: true }).click();
    expect(ctx.writes).toEqual([]);
  }
  await amount.fill('1');
  await expect(amount).not.toHaveAttribute('aria-invalid');
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('existing maximum opens losslessly in bytes and equivalent unit edits send no PATCH', async ({
  page,
}) => {
  const ctx = await setup(page);
  ctx.disk.capacity = '9223372036854775807';
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await expect(page.getByLabel('Capacity amount')).toHaveValue('9223372036854775807');
  await expect(page.getByLabel('Capacity unit')).toHaveValue('B');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'No changes' })).toBeVisible();
  await page.getByLabel('Capacity amount').fill('9223372.036854775807');
  await page.getByLabel('Capacity unit').selectOption('TB');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'No changes' })).toBeVisible();
  expect(ctx.writes).toEqual([]);
});

test('capacity controls fit approved widths with exact preview', async ({ page }, testInfo) => {
  await setup(page);
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('Archive capacity');
  await page.getByLabel('Capacity amount').fill('1.5');
  await page.getByLabel('Capacity unit').selectOption('TB');
  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.getByText('Exact bytes: 1500000000000', { exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.getByRole('region', { name: 'Create disk', exact: true }).screenshot({
      path: testInfo.outputPath(`capacity-${width}.png`),
    });
  }
});

test('create validates exact decimal range and preserves literal text without numeric rounding', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.route('**/api/v1/disks', (r) => {
    ctx.writes.push({ method: r.request().method(), body: r.request().postDataJSON() });
    Object.assign(ctx.disk, r.request().postDataJSON());
    return r.fulfill({ status: 201, json: ctx.disk });
  });
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill(' <new> ');
  await page.getByLabel('Capacity amount').fill('9223372036854775808');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('signed 64-bit');
  expect(ctx.writes).toEqual([]);
  await page.getByLabel('Capacity amount').fill('09007199254740993');
  await page.getByLabel('Serial', { exact: true }).fill('  literal serial  ');
  await page.getByLabel('Notes', { exact: true }).fill('<script>literal</script>');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Disk created' })).toBeVisible();
  expect(ctx.writes).toEqual([
    {
      method: 'POST',
      body: {
        label: ' <new> ',
        capacity: '9007199254740993',
        serial: '  literal serial  ',
        notes: '<script>literal</script>',
      },
    },
  ]);
  expect(ctx.reads()).toBe(1);
  await expect(page.getByRole('link', { name: ' <new> ', exact: true })).toBeVisible();
});

test('edit sends changed fields only, explicit null clearing, and no empty PATCH', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'No changes' })).toBeVisible();
  expect(ctx.writes).toEqual([]);
  await page.getByLabel('Capacity amount').fill('9007199254740993');
  await page.getByLabel('Serial', { exact: true }).fill('');
  await page.getByLabel('Notes', { exact: true }).fill('');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Disk updated' })).toBeVisible();
  expect(ctx.writes).toEqual([
    {
      method: 'PATCH',
      body: {
        capacity: '9007199254740993',
        serial: null,
        notes: null,
      },
    },
  ]);
  expect(ctx.reads()).toBeGreaterThan(1);
  await expect(page.getByRole('region', { name: 'Disk metadata' })).toContainText(
    '9,007,199,254,740,993 B',
  );
});

test('conflict retains every input and allows deliberate correction without retry', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.route('**/api/v1/disks/1', (r) => {
    if (r.request().method() === 'GET') return r.fulfill({ json: ctx.disk });
    ctx.writes.push({ method: 'PATCH', body: r.request().postDataJSON() });
    return r.fulfill({
      status: 409,
      json: { error: { code: 'conflict', message: 'label exists' } },
    });
  });
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('duplicate');
  await page.getByLabel('Notes', { exact: true }).fill('draft kept');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('alert')).toContainText('Conflict');
  await expect(page.getByLabel('Label', { exact: true })).toHaveValue('duplicate');
  await expect(page.getByLabel('Notes', { exact: true })).toHaveValue('draft kept');
  await page.waitForTimeout(1200);
  expect(ctx.writes).toHaveLength(1);
});

test('uncertain PATCH is blocked until manual reconciliation, even after readiness recovery', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.route('**/api/v1/disks/1', (r) => {
    if (r.request().method() === 'GET') return r.fulfill({ json: ctx.disk });
    ctx.writes.push({ method: 'PATCH', body: r.request().postDataJSON() });
    Object.assign(ctx.disk, r.request().postDataJSON());
    return r.abort('failed');
  });
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('committed but lost response');
  await page.getByLabel('Capacity amount').fill('1.5');
  await page.getByLabel('Capacity unit').selectOption('TB');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('alert')).toContainText('may have succeeded');
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
  await page.waitForTimeout(1500);
  expect(ctx.writes).toHaveLength(1);
  await expect(page.getByLabel('Label', { exact: true })).toHaveValue(
    'committed but lost response',
  );
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await expect(page.getByLabel('Capacity amount')).toHaveValue('1.5');
  await expect(page.getByLabel('Capacity unit')).toHaveValue('TB');
  expect(ctx.writes[0]!.body.capacity).toBe('1500000000000');
  await expect(
    page.getByRole('status').filter({ hasText: 'Review refreshed metadata' }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'No changes' })).toBeVisible();
  expect(ctx.writes).toHaveLength(1);
});

test('uncertain creation checks paginated labels and reconciles a created disk without another POST', async ({
  page,
}) => {
  const ctx = await setup(page);
  let posts = 0;
  await page.route('**/api/v1/disks', (r) => {
    posts++;
    Object.assign(ctx.disk, r.request().postDataJSON());
    return r.abort('failed');
  });
  await page.route('**/api/v1/disks?**', (r) =>
    r.fulfill({
      json: {
        revision: '9007199254740993',
        items: new URL(r.request().url()).searchParams.has('cursor') ? [ctx.disk] : [],
        next_cursor: new URL(r.request().url()).searchParams.has('cursor') ? null : 'next',
      },
    }),
  );
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('new disk');
  await page.getByLabel('Capacity amount').fill('0');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('may have succeeded');
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Check next disk page' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create disk', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Check next disk page' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Existing disk found' })).toBeVisible();
  expect(posts).toBe(1);
});

test('list-first creation has deliberate discard and associated required-field errors', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.goto('/disks');
  await expect(page.getByLabel('Label', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: ctx.disk.label, exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Create disk', exact: true })).toBeFocused();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByLabel('Label', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByLabel('Label', { exact: true })).toHaveAccessibleDescription(
    'Enter a disk label.',
  );
  await expect(page.getByLabel('Capacity amount')).toHaveAttribute('aria-invalid', 'true');
  await page.getByLabel('Capacity unit').selectOption('TB');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await expect(page.getByLabel('Capacity unit')).toHaveValue('TB');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByLabel('Capacity unit')).toHaveValue('B');
  expect(ctx.writes).toEqual([]);
});

test('edit closes unchanged drafts, confirms changed drafts and completes saved edits', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByLabel('Label', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Notes', { exact: true }).fill('unsaved');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click();
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await expect(page.getByLabel('Notes', { exact: true })).toHaveValue(ctx.disk.notes ?? '');
  await page.getByLabel('Notes', { exact: true }).fill('saved');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Disk updated' })).toBeVisible();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Edit disk', exact: true })).toBeEnabled();
  expect(ctx.writes).toEqual([{ method: 'PATCH', body: { notes: 'saved' } }]);
});

test('cancel cannot interrupt a dispatched creation or bypass an uncertain outcome', async ({
  page,
}) => {
  await setup(page);
  let release!: () => void;
  const pending = new Promise<void>((done) => {
    release = done;
  });
  let posts = 0;
  await page.route('**/api/v1/disks', async (r) => {
    posts++;
    await pending;
    await r.abort('failed');
  });
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('sent draft');
  await page.getByLabel('Capacity amount').fill('0');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Saving…', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
  release();
  await expect(
    page.getByRole('region', { name: 'Check whether saved', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
  await expect(page.getByLabel('Label', { exact: true })).toHaveValue('sent draft');
  expect(posts).toBe(1);
});

test('label write invalidates retained search selection and refetches unchanged-revision labels', async ({
  page,
}) => {
  const ctx = await setup(page);
  let searches = 0;
  await page.route('**/api/v1/search?**', (r) => {
    searches++;
    const result = searchFixture();
    result.items[0]!.disk.label = ctx.disk.label;
    return r.fulfill({ json: result });
  });
  await page.route('**/api/v1/observations/9007199254740993', (r) =>
    r.fulfill({ json: observationFixture() }),
  );
  await page.goto('/?q=report');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('searchbox').press('ArrowDown');
  await page.getByRole('link', { name: 'Observation', exact: true }).click();
  await page.getByRole('link', { name: 'Disk <archive>', exact: true }).click();
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('fresh label');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Disk updated' })).toBeVisible();
  expect(ctx.writes).toEqual([{ method: 'PATCH', body: { label: 'fresh label' } }]);
  await page.getByRole('link', { name: 'Return to search' }).click();
  await expect(page.locator('tbody tr')).toContainText('fresh label');
  expect(searches).toBe(2);
  await expect(page.locator('[data-selected-content="true"]')).toHaveCount(0);
});

test('context invalidation keeps drafts but requires rebase before PATCH, retaining remote untouched fields', async ({
  page,
}) => {
  const ctx = await setup(page);
  let reloads = 0;
  await page.route('**/api/v1/disks/1', (r) => {
    if (r.request().method() === 'PATCH') {
      ctx.writes.push({ method: 'PATCH', body: r.request().postDataJSON() });
      Object.assign(ctx.disk, r.request().postDataJSON());
      return r.fulfill({ json: ctx.disk });
    }
    reloads++;
    if (reloads === 2) return r.abort('failed');
    if (reloads > 2) {
      ctx.disk.serial = 'externally changed';
      ctx.disk.capacity = '9007199254740993';
    }
    return r.fulfill({ json: ctx.disk });
  });
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Notes', { exact: true }).fill('retained local note');
  await page.getByLabel('Capacity amount').fill('9223372.036854775807');
  await page.getByLabel('Capacity unit').selectOption('TB');
  await page.getByRole('button', { name: 'Reload disk', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  await expect(page.getByLabel('Notes', { exact: true })).toHaveValue('retained local note');
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await expect(page.getByLabel('Serial', { exact: true })).toHaveValue('externally changed');
  await expect(page.getByLabel('Capacity amount')).toHaveValue('9007199254740993');
  await expect(page.getByLabel('Capacity unit')).toHaveValue('B');
  await expect(page.getByLabel('Notes', { exact: true })).toHaveValue('retained local note');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Disk updated' })).toBeVisible();
  expect(ctx.writes).toEqual([{ method: 'PATCH', body: { notes: 'retained local note' } }]);
});

test('failed post-write refetch blocks another write until verified metadata is loaded', async ({
  page,
}) => {
  const ctx = await setup(page);
  let failRead = false;
  await page.route('**/api/v1/disks/1', (r) => {
    if (r.request().method() === 'PATCH') {
      ctx.writes.push({ method: 'PATCH', body: r.request().postDataJSON() });
      Object.assign(ctx.disk, r.request().postDataJSON());
      failRead = true;
      return r.fulfill({ json: ctx.disk });
    }
    if (failRead) {
      failRead = false;
      return r.fulfill({
        status: 500,
        json: { error: { code: 'internal_error', message: 'read failed' } },
      });
    }
    return r.fulfill({ json: ctx.disk });
  });
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('saved label');
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('alert')).toContainText('Write succeeded but metadata is unverified');
  await expect(page.getByRole('button', { name: 'Save changes' })).toBeDisabled();
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'No changes' })).toBeVisible();
  expect(ctx.writes).toHaveLength(1);
});

test('uncertain unmatched creation requires complete label traversal and explicit resubmission approval', async ({
  page,
}) => {
  await setup(page);
  let posts = 0;
  let reconciledPages = 0;
  await page.route('**/api/v1/disks', (r) => {
    posts++;
    return r.abort('failed');
  });
  await page.route('**/api/v1/disks?**', (r) => {
    const last = new URL(r.request().url()).searchParams.has('cursor');
    reconciledPages++;
    return r.fulfill({
      json: {
        revision: '9007199254740993',
        items: [],
        next_cursor: last ? null : 'last',
      },
    });
  });
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('missing after uncertain POST');
  await page.getByLabel('Capacity amount').fill('0');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('may have succeeded');
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'I reviewed the disks; allow another creation' }),
  ).toHaveCount(0);
  const checked = reconciledPages;
  await page.waitForTimeout(200);
  expect(reconciledPages).toBe(checked);
  await page.getByRole('button', { name: 'Check next disk page' }).click();
  await expect(
    page.getByRole('status').filter({ hasText: 'A disk might have been renamed' }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create disk', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'I reviewed the disks; allow another creation' }).click();
  await expect(page.getByRole('button', { name: 'Create disk', exact: true })).toBeEnabled();
  expect(posts).toBe(1);
});

test('failed creation reconciliation restart cannot reuse an earlier no-match approval', async ({
  page,
}) => {
  const ctx = await setup(page);
  let posted = false;
  let failRead = false;
  let posts = 0;
  await page.route('**/healthz', (r) =>
    posted ? r.abort('failed') : r.fulfill({ json: { status: 'ready' } }),
  );
  await page.route('**/api/v1/disks', (r) => {
    posts++;
    posted = true;
    return r.abort('failed');
  });
  await page.route('**/api/v1/disks?**', (r) =>
    r.fulfill({
      json: {
        revision: '9007199254740993',
        items: posted && failRead ? [ctx.disk] : [],
        next_cursor: null,
      },
    }),
  );
  await page.route('**/api/v1/disks/1', (r) =>
    r.fulfill({
      status: 500,
      json: { error: { code: 'internal_error', message: 'reconciliation read failed' } },
    }),
  );
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill(ctx.disk.label);
  await expect(page.getByRole('status').filter({ hasText: 'Backend ready' })).toBeVisible();
  await page.getByLabel('Capacity amount').fill('0');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('may have succeeded');
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'I reviewed the disks; allow another creation' }),
  ).toBeVisible();
  failRead = true;
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('reconciliation read failed');
  await expect(
    page.getByRole('button', { name: 'I reviewed the disks; allow another creation' }),
  ).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Create disk', exact: true })).toBeDisabled();
  expect(posts).toBe(1);
});

test('disk lifecycle layouts fit approved widths', async ({ page }, testInfo) => {
  const ctx = await setup(page);
  await page.route('**/api/v1/disks', (r) => r.abort('failed'));
  await page.goto('/disks');
  const capture = async (state: string) => {
    for (const width of [1440, 1024, 768]) {
      await page.setViewportSize({ width, height: 900 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
      await page.locator('main').screenshot({ path: testInfo.outputPath(`${state}-${width}.png`) });
    }
  };
  await expect(page.getByRole('link', { name: ctx.disk.label, exact: true })).toBeVisible();
  await capture('list');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('Archive draft');
  await page.getByLabel('Capacity amount').fill('1.5');
  await page.getByLabel('Capacity unit').selectOption('TB');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await capture('discard');
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click();
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('may have succeeded');
  await capture('recovery');
  await page.goto('/disks/1');
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await capture('edit');
});

test('verified creation closes without discard, but later invalidation must be checked first', async ({
  page,
}) => {
  const ctx = await setup(page);
  await page.route('**/api/v1/disks', (r) => {
    ctx.writes.push({ method: 'POST', body: r.request().postDataJSON() });
    Object.assign(ctx.disk, r.request().postDataJSON());
    return r.fulfill({ status: 201, json: ctx.disk });
  });
  await page.goto('/disks');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('Verified creation');
  await page.getByLabel('Capacity amount').fill('0');
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await expect(page.getByRole('link', { name: 'Open verified disk' })).toBeVisible();
  await page.route('**/api/v1/disks?**', (r) => r.abort('failed'));
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Close', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Check whether saved', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Close', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Discard changes', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Create disk', exact: true })).toBeEnabled();
  expect(ctx.writes).toHaveLength(1);
});
