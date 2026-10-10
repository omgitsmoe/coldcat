import assert from 'node:assert/strict';
import { access } from 'node:fs/promises';
import { test } from 'node:test';
import { startCatalog, type Catalog } from './harness.ts';
import { resolve } from 'node:path';
import { inventories } from './fixture.ts';
import { createServer } from 'node:net';

async function gone(state: Partial<Catalog>) {
  assert(state.root);
  await assert.rejects(access(state.root), { code: 'ENOENT' });
  if (state.pid) assert.throws(() => process.kill(state.pid!, 0), { code: 'ESRCH' });
  for (const origin of new Set([state.origin, state.backendOrigin].filter(Boolean))) {
    await assert.rejects(fetch(new URL('/healthz', origin), { signal: AbortSignal.timeout(1000) }));
    const released = createServer();
    try {
      await new Promise<void>((done, reject) => {
        released.once('error', reject);
        released.listen(Number(new URL(origin!).port), '127.0.0.1', done);
      });
    } finally {
      await new Promise<void>((done, reject) =>
        released.close((error) => (error ? reject(error) : done())),
      );
    }
  }
}

test('successful harness owns a ready linked catalog and releases processes/ports/directory', async () => {
  const catalog = await startCatalog();
  try {
    const response = await fetch(new URL('/api/v1/catalog', catalog.origin));
    assert.equal(response.status, 200);
    const totals = await response.json();
    assert.equal(totals.disk_count, '3');
    assert.equal(totals.file_count, '12');
    assert.equal(catalog.snapshots.length, 3);
  } finally {
    await catalog.close();
  }
  await catalog.close();
  await gone(catalog);
});

for (const failure of ['imported', 'backend', 'proxy'] as const) {
  test(`failure after ${failure} cleans every acquired resource`, async () => {
    let state: Partial<Catalog> = {};
    await assert.rejects(
      startCatalog((stage, acquired) => {
        state = { ...state, ...acquired };
        if (stage === failure) throw new Error(`injected ${failure} failure`);
      }),
      new RegExp(`injected ${failure} failure`),
    );
    await gone(state);
  });
}

for (const assets of [undefined, resolve('build')]) {
  const mode = assets ? 'Go assets' : 'development proxy';
  test(`${mode}: stop/import/restart owns only one catalog process and cleans the replacement`, async () => {
    const catalog = await startCatalog(undefined, assets);
    const firstPID = catalog.pid;
    try {
      await assert.rejects(catalog.importInventory(inventories[0]!), /Stop the backend/);
      await assert.rejects(catalog.restartBackend(), /Stop the backend/);
      const stopping = catalog.stopBackend();
      await assert.rejects(catalog.importInventory(inventories[0]!), /already in progress/);
      await stopping;
      await catalog.stopBackend();
      assert.throws(() => process.kill(firstPID, 0), { code: 'ESRCH' });
      await catalog.importInventory({
        ...inventories[0]!,
        name: 'next',
        capturedAt: '2023-01-01T00:00:00Z',
      });
      await catalog.restartBackend();
      assert.notEqual(catalog.pid, firstPID);
      assert.equal((await fetch(`${catalog.origin}/healthz`)).status, 200);
    } finally {
      await catalog.close();
    }
    await gone(catalog);
  });

  test(`${mode}: failure after restart readiness cleans replacement and owned directory`, async () => {
    let launches = 0;
    const catalog = await startCatalog((stage) => {
      if (stage === 'backend' && ++launches === 2) throw new Error('injected restart failure');
    }, assets);
    const firstPID = catalog.pid;
    try {
      await catalog.stopBackend();
      await assert.rejects(catalog.restartBackend(), /injected restart failure/);
    } finally {
      await catalog.close();
    }
    assert.throws(() => process.kill(firstPID, 0), { code: 'ESRCH' });
    await gone(catalog);
  });

  test(`${mode}: failed offline import still releases stopped catalog and host`, async () => {
    const catalog = await startCatalog(undefined, assets);
    try {
      await catalog.stopBackend();
      await assert.rejects(
        catalog.importInventory({
          ...inventories[0]!,
          name: 'invalid',
          records: 'invalid record\n',
        }),
      );
    } finally {
      await catalog.close();
    }
    await gone(catalog);
  });
}
