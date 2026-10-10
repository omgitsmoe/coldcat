import { test as base, expect, type Page } from '@playwright/test';
import { resolve } from 'node:path';
import { startCatalog, type Catalog } from './harness';
import { fixture, inventories } from './fixture';

const test = base.extend<{ catalog: Catalog }>({
  catalog: async ({ browserName }, use, info) => {
    expect(browserName).toBe('chromium');
    const catalog = await startCatalog(
      undefined,
      info.project.name === 'deployed' ? resolve('build') : undefined,
    );
    try {
      await use(catalog);
    } finally {
      await catalog.close();
    }
  },
});

const kinds = [
  'search',
  'contents',
  'entries',
  'replicas',
  'coverage',
  'locations',
  'disks',
  'snapshots',
] as const;
type Kind = (typeof kinds)[number];

async function cleared(page: Page, kind: Kind) {
  await expect(page.getByRole('button', { name: 'Load more', exact: true })).toHaveCount(0);
  if (kind === 'replicas' || kind === 'coverage') {
    const region = page.getByRole('region', {
      name: kind === 'replicas' ? 'Exact tree copies' : 'Content on other disks',
      exact: true,
    });
    await expect(region.getByRole('link')).toHaveCount(0);
    await expect(region).not.toContainText('Selected source files:');
  } else await expect(page.locator('tbody tr')).toHaveCount(0);
}

async function open(page: Page, catalog: Catalog, kind: Kind) {
  const old = catalog.snapshots.find((snapshot) => snapshot.captured_at === fixture.oldCapture)!;
  let path: string;
  let endpoint: string;
  if (kind === 'search') {
    path = '/?q=report&limit=1';
    endpoint = '/api/v1/search';
  } else if (kind === 'contents') {
    path = '/contents?limit=1';
    endpoint = '/api/v1/contents';
  } else if (kind === 'disks') {
    path = '/disks?limit=1';
    endpoint = '/api/v1/disks';
  } else if (kind === 'snapshots') {
    path = `/disks/${catalog.diskID}?limit=1`;
    endpoint = `/api/v1/disks/${catalog.diskID}/snapshots`;
  } else if (kind === 'locations') {
    const result = await (
      await page.request.get(`${catalog.origin}/api/v1/search?q=report`)
    ).json();
    const id = result.items.find(
      (item: { content: { hash: { hex: string } } }) =>
        item.content.hash.hex === fixture.sharedHash,
    ).content.id;
    path = `/contents/${id}?limit=1`;
    endpoint = `/api/v1/contents/${id}/observations`;
  } else {
    const directory =
      kind === 'coverage'
        ? fixture.coverageDirectory
        : kind === 'replicas'
          ? fixture.directory
          : 'foo';
    path = `/snapshots/${old.id}/directory?${new URLSearchParams({ path: directory, limit: '1' })}`;
    endpoint = `/api/v1/snapshots/${old.id}/directory/${kind}`;
  }
  await page.goto(new URL(path, catalog.origin).href);
  if (kind === 'replicas' || kind === 'coverage') {
    await page
      .getByRole('button', {
        name: kind === 'replicas' ? 'Exact tree copies' : 'Content on other disks',
        exact: true,
      })
      .click();
    await page.getByLabel('Comparison page size').fill('1');
    await page.getByRole('button', { name: 'Apply and compare' }).click();
  }
  await expect(page.getByRole('button', { name: 'Load more', exact: true })).toBeVisible();
  await expect(page.locator('header')).toContainText('Backend ready');
  return endpoint;
}

for (const kind of kinds) {
  test(`${kind}: offline import rejects old cursor, explicit restart never mixes revisions`, async ({
    page,
    catalog,
  }) => {
    const endpoint = await open(page, catalog, kind);
    const before = await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json();
    await catalog.stopBackend();
    await catalog.importInventory({
      ...inventories[0]!,
      name: 'alpha-revision',
      capturedAt: '2023-01-01T00:00:00Z',
      records: inventories[0]!.records.split('\n')[0] + '\n',
    });
    await catalog.restartBackend();
    const after = await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json();
    expect(after.revision).not.toBe(before.revision);
    const stale = page.waitForResponse(
      (response) => new URL(response.url()).pathname === endpoint && response.status() === 409,
    );
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    expect((await (await stale).json()).error.code).toBe('stale_cursor');
    await expect(
      page.getByText('Inventory changed; reload results', { exact: true }),
    ).toBeVisible();
    await cleared(page, kind);
    const restarted = page.waitForResponse(
      (response) => new URL(response.url()).pathname === endpoint && response.status() === 200,
    );
    await page.getByRole('button', { name: 'Reload results', exact: true }).click();
    const response = await restarted;
    expect(new URL(response.url()).searchParams.has('cursor')).toBe(false);
    expect((await response.json()).revision).toBe(after.revision);
    await expect(page.getByText('Inventory changed; reload results', { exact: true })).toHaveCount(
      0,
    );
  });

  test(`${kind}: same-revision reconnection clears retained data and does not reuse cursors`, async ({
    page,
    catalog,
  }) => {
    const endpoint = await open(page, catalog, kind);
    const before = await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json();
    await catalog.stopBackend();
    await page.getByRole('button', { name: 'Load more', exact: true }).click();
    await expect(page.locator('header')).toContainText('Backend unavailable');
    await catalog.restartBackend();
    const after = await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json();
    expect(after.revision).toBe(before.revision);
    const calls: string[] = [];
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === endpoint) calls.push(request.url());
    });
    const retry = page.getByRole('button', { name: 'Retry connection', exact: true });
    if (await retry.isVisible()) await retry.click();
    await expect(page.locator('header')).toContainText('Backend ready');
    await cleared(page, kind);
    expect(calls).toEqual([]);
    const restarted = page.waitForResponse(
      (response) => new URL(response.url()).pathname === endpoint && response.status() === 200,
    );
    const name =
      kind === 'replicas' || kind === 'coverage'
        ? 'Apply and compare'
        : kind === 'entries'
          ? 'Reload entries'
          : kind === 'locations'
            ? 'Reload locations'
            : 'Reload results';
    await page.getByRole('button', { name, exact: true }).click();
    expect(new URL((await restarted).url()).searchParams.has('cursor')).toBe(false);
  });
}

test('committed PATCH response loss, real outage, reconciliation and search label invalidation', async ({
  page,
  catalog,
}) => {
  await open(page, catalog, 'search');
  await page.getByRole('searchbox').press('ArrowDown');
  await page.getByRole('link', { name: 'Disks', exact: true }).click();
  await page.getByRole('link', { name: fixture.label, exact: true }).click();
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('Reconciled archive');
  const revision = (await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json())
    .revision;
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Fetch.enable', {
    patterns: [{ urlPattern: '*/api/v1/disks/*', requestStage: 'Response' }],
  });
  let writes = 0;
  page.on('request', (request) => {
    if (request.method() === 'PATCH') writes++;
  });
  const dropped = new Promise<void>((done, reject) => {
    cdp.on('Fetch.requestPaused', (event) => {
      void (async () => {
        if (event.request.method === 'PATCH') {
          expect(event.responseStatusCode).toBe(200);
          await catalog.stopBackend();
          await cdp.send('Fetch.failRequest', {
            requestId: event.requestId,
            errorReason: 'ConnectionClosed',
          });
          await cdp.send('Fetch.disable');
          done();
        } else await cdp.send('Fetch.continueRequest', { requestId: event.requestId });
      })().catch(reject);
    });
  });
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await dropped;
  await expect(page.getByRole('alert')).toContainText('write may have succeeded');
  await expect(page.getByLabel('Label', { exact: true })).toHaveValue('Reconciled archive');
  await expect(page.getByRole('button', { name: 'Save changes', exact: true })).toBeDisabled();
  await catalog.restartBackend();
  await page.getByRole('button', { name: 'Retry connection', exact: true }).click();
  await expect(page.locator('header')).toContainText('Backend ready');
  await page.getByRole('button', { name: 'Reconcile disk', exact: true }).click();
  await expect(
    page.getByRole('status').filter({ hasText: 'Review refreshed metadata' }),
  ).toBeVisible();
  expect(writes).toBe(1);
  expect((await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json()).revision).toBe(
    revision,
  );
  await page.getByRole('link', { name: 'Search', exact: true }).click();
  await page.getByRole('searchbox').fill('report');
  await expect(page.locator('tbody')).toContainText('Reconciled archive');
  await expect(page.locator('tbody')).not.toContainText(fixture.label);
  await expect(page.locator('[data-selected-content="true"]')).toHaveCount(0);
  await cdp.detach();
});

test('successful metadata-only write invalidates the retained same-query search session', async ({
  page,
  catalog,
}) => {
  await open(page, catalog, 'search');
  await page.getByRole('searchbox').press('ArrowDown');
  await expect(page.locator('[data-selected-content="true"]')).toHaveCount(1);
  const originalURL = page.url();
  const revision = (await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json())
    .revision;
  await page.getByRole('link', { name: 'Disks', exact: true }).click();
  await page.getByRole('link', { name: fixture.label, exact: true }).click();
  await page.getByRole('button', { name: 'Edit disk', exact: true }).click();
  await page.getByLabel('Label', { exact: true }).fill('Metadata-only archive');
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Disk updated' })).toBeVisible();
  const search = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/v1/search' && response.status() === 200,
  );
  await page.goBack();
  await page.goBack();
  await expect(page).toHaveURL(originalURL);
  expect(new URL((await search).url()).searchParams.has('cursor')).toBe(false);
  await expect(page.locator('tbody')).toContainText('Metadata-only archive');
  await expect(page.locator('tbody')).not.toContainText(fixture.label);
  await expect(page.locator('[data-selected-content="true"]')).toHaveCount(0);
  expect((await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json()).revision).toBe(
    revision,
  );
  await expect(page.locator('header')).toContainText('Backend ready');
});

test('committed POST response loss reconciles the existing disk without another creation', async ({
  page,
  catalog,
}) => {
  await page.goto(`${catalog.origin}/disks`);
  await page.getByLabel('Label', { exact: true }).fill('Uncertain creation');
  await page.getByLabel('Capacity in bytes').fill('9007199254740993');
  await page.getByLabel('Notes', { exact: true }).fill('Retained outage draft');
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Fetch.enable', {
    patterns: [{ urlPattern: '*/api/v1/disks', requestStage: 'Response' }],
  });
  let writes = 0;
  page.on('request', (request) => {
    if (request.method() === 'POST') writes++;
  });
  const dropped = new Promise<void>((done, reject) => {
    cdp.on('Fetch.requestPaused', (event) => {
      void (async () => {
        if (event.request.method === 'POST') {
          expect(event.responseStatusCode).toBe(201);
          await catalog.stopBackend();
          await cdp.send('Fetch.failRequest', {
            requestId: event.requestId,
            errorReason: 'ConnectionClosed',
          });
          await cdp.send('Fetch.disable');
          done();
        } else await cdp.send('Fetch.continueRequest', { requestId: event.requestId });
      })().catch(reject);
    });
  });
  await page.getByRole('button', { name: 'Create disk', exact: true }).click();
  await dropped;
  await expect(page.getByRole('alert')).toContainText('write may have succeeded');
  await expect(page.getByLabel('Notes', { exact: true })).toHaveValue('Retained outage draft');
  await expect(page.getByRole('button', { name: 'Create disk', exact: true })).toBeDisabled();
  await catalog.restartBackend();
  await page.getByRole('button', { name: 'Retry connection', exact: true }).click();
  await expect(page.locator('header')).toContainText('Backend ready');
  await page.getByRole('button', { name: 'Reconcile creation', exact: true }).click();
  await expect(
    page.getByText('Existing disk found. Review its metadata; no creation was retried.'),
  ).toBeVisible();
  expect(writes).toBe(1);
  await expect(page.getByRole('link', { name: 'Open verified disk' })).toBeVisible();
  const disks = await (await page.request.get(`${catalog.origin}/api/v1/disks`)).json();
  expect(
    disks.items.filter((disk: { label: string }) => disk.label === 'Uncertain creation'),
  ).toHaveLength(1);
  await cdp.detach();
});

test('changed-revision reconnection clears search restoration and refreshes catalog context', async ({
  page,
  catalog,
}) => {
  await open(page, catalog, 'search');
  await page.getByRole('searchbox').press('ArrowDown');
  const before = await (await page.request.get(`${catalog.origin}/api/v1/catalog`)).json();
  await catalog.stopBackend();
  await page.getByRole('button', { name: 'Load more', exact: true }).click();
  await expect(page.locator('header')).toContainText('Backend unavailable');
  await catalog.importInventory({
    ...inventories[0]!,
    name: 'changed',
    capturedAt: '2023-01-01T00:00:00Z',
    records: '',
  });
  await catalog.restartBackend();
  const checked = page.waitForResponse(
    (response) => response.url().endsWith('/api/v1/catalog') && response.status() === 200,
  );
  await page.getByRole('button', { name: 'Retry connection', exact: true }).click();
  expect((await (await checked).json()).revision).not.toBe(before.revision);
  await expect(page.locator('header')).toContainText('Backend ready');
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await expect(page.locator('[data-selected-content="true"]')).toHaveCount(0);
  await page.getByRole('button', { name: 'Reload results', exact: true }).click();
  await expect(page.locator('tbody')).not.toContainText(fixture.label);
  await expect(page.locator('tbody tr')).toHaveCount(1);
});
