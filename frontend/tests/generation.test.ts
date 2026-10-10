import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = fileURLToPath(new URL('../scripts/generate-api.mjs', import.meta.url));
const original = await readFile(new URL('../../doc/openapi.json', import.meta.url), 'utf8');

test('generation is reproducible; drift check is read-only and detects source and output drift', async () => {
  const temporary = await mkdtemp('/tmp/opencode/coldcat-api-generation-');
  try {
    const source = join(temporary, 'openapi.json');
    const output = join(temporary, 'generated');
    await writeFile(source, original);
    const run = (...args: string[]) =>
      spawnSync(process.execPath, [script, '--source', source, '--output', output, ...args], {
        encoding: 'utf8',
      });
    let result = run();
    assert.equal(result.status, 0, result.stderr);
    const wire = join(output, 'wire.ts');
    const before = await readFile(wire, 'utf8');
    assert.equal(run().status, 0);
    assert.equal(await readFile(wire, 'utf8'), before);
    assert.equal(run('--check').status, 0);

    await writeFile(wire, `${before}\n`);
    result = run('--check');
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Generated API drift/);
    assert.equal(await readFile(wire, 'utf8'), `${before}\n`);
    await writeFile(wire, before);

    const changed = JSON.parse(original);
    changed.components.schemas.DiskDetail.properties.label.minLength = 2;
    await writeFile(source, JSON.stringify(changed));
    assert.notEqual(run('--check').status, 0);
    assert.equal(await readFile(wire, 'utf8'), before);
  } finally {
    await rm(temporary, { recursive: true });
  }
});

test('unsupported schema assertions fail generation instead of silently weakening types', async () => {
  const temporary = await mkdtemp('/tmp/opencode/coldcat-api-generation-');
  try {
    const source = join(temporary, 'openapi.json');
    const changed = JSON.parse(original);
    changed.components.schemas.DiskDetail.oneOf = [{ type: 'object' }];
    await writeFile(source, JSON.stringify(changed));
    const result = spawnSync(
      process.execPath,
      [script, '--source', source, '--output', join(temporary, 'generated')],
      { encoding: 'utf8' },
    );
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Unsupported schema keyword: oneOf/);
    delete changed.components.schemas.DiskDetail.oneOf;
    changed.components.schemas.DiskDetail.properties.id.minimum = 5;
    await writeFile(source, JSON.stringify(changed));
    const sibling = spawnSync(
      process.execPath,
      [script, '--source', source, '--output', join(temporary, 'generated')],
      { encoding: 'utf8' },
    );
    assert.notEqual(sibling.status, 0);
    assert.match(sibling.stderr, /Unsupported schema reference siblings/);
  } finally {
    await rm(temporary, { recursive: true });
  }
});
