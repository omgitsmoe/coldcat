import assert from 'node:assert/strict';
import { access } from 'node:fs/promises';
import { resolve } from 'node:path';
import { test } from 'node:test';
import { startCatalog, type Catalog } from '../real/harness.ts';

async function gone(state: Partial<Catalog>) {
  assert(state.root);
  await assert.rejects(access(state.root), { code: 'ENOENT' });
  if (state.pid) assert.throws(() => process.kill(state.pid!, 0), { code: 'ESRCH' });
  if (state.backendOrigin)
    await assert.rejects(
      fetch(`${state.backendOrigin}/healthz`, { signal: AbortSignal.timeout(1000) }),
    );
}

test('built hosting owns only Go and releases its port, process and catalog', async () => {
  const catalog = await startCatalog(undefined, resolve('build'));
  try {
    assert.equal(catalog.origin, catalog.backendOrigin);
    const response = await fetch(`${catalog.origin}/disks/${catalog.diskID}`);
    assert.equal(response.status, 200);
    assert.match(response.headers.get('content-type')!, /text\/html/);
  } finally {
    await catalog.close();
  }
  await catalog.close();
  await gone(catalog);
});

for (const failure of ['imported', 'backend', 'assets'] as const) {
  test(`built hosting failure after ${failure} releases acquired resources`, async () => {
    let state: Partial<Catalog> = {};
    await assert.rejects(
      startCatalog((stage, acquired) => {
        state = { ...state, ...acquired };
        if (stage === failure) throw new Error(`injected ${failure} failure`);
      }, resolve('build')),
      new RegExp(`injected ${failure} failure`),
    );
    await gone(state);
  });
}
