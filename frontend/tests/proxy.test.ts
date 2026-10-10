import { createServer as httpServer } from 'node:http';
import { once } from 'node:events';
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createServer } from 'vite';

test('development proxy forwards API/readiness and exposes connection refusal', async () => {
  const backend = httpServer((request, response) => {
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ path: request.url }));
  });
  backend.listen(0, '127.0.0.1');
  await once(backend, 'listening');
  const address = backend.address();
  assert(address && typeof address !== 'string');
  process.env.COLDCAT_BACKEND = `http://127.0.0.1:${address.port}`;
  const vite = await createServer({ server: { port: 0 } });
  try {
    await vite.listen();
    const origin = vite.resolvedUrls?.local[0];
    assert(origin);
    for (const path of ['healthz', 'api/v1/disks?limit=1']) {
      const response: Response = await fetch(new URL(path, origin));
      assert.equal(response.status, 200);
      assert.deepEqual(await response.json(), { path: `/${path}` });
    }
    await new Promise<void>((resolve, reject) =>
      backend.close((err) => (err ? reject(err) : resolve())),
    );
    const response = await fetch(new URL('healthz', origin));
    assert.equal(response.status, 502);
    assert.equal((await response.text()).includes('<html'), false);
  } finally {
    await vite.close();
    if (backend.listening) await new Promise((resolve) => backend.close(resolve));
    delete process.env.COLDCAT_BACKEND;
  }
});
