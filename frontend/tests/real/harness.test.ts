import assert from 'node:assert/strict';
import { access } from 'node:fs/promises';
import { test } from 'node:test';
import { startCatalog, type Catalog } from './harness.ts';

async function gone(state: Partial<Catalog>) {
  assert(state.root);
  await assert.rejects(access(state.root), { code: 'ENOENT' });
  if (state.pid) assert.throws(() => process.kill(state.pid!, 0), { code: 'ESRCH' });
  for (const origin of [state.origin, state.backendOrigin].filter(Boolean)) {
    await assert.rejects(fetch(new URL('/healthz', origin), { signal: AbortSignal.timeout(1000) }));
  }
}

test('successful harness owns a ready linked catalog and releases processes/ports/directory', async () => {
  const catalog = await startCatalog();
  try {
    const response = await fetch(new URL('/api/v1/catalog', catalog.origin));
    assert.equal(response.status, 200);
    const totals = await response.json();
    assert.equal(totals.disk_count, '3');
    assert.equal(totals.file_count, '10');
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
