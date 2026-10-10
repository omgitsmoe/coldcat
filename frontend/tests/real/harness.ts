import { execFile, spawn, type ChildProcess } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { createServer, type ViteDevServer } from 'vite';
import { backendTarget, proxyRules } from '../../dev-proxy.ts';
import { fixture, inventories } from './fixture.ts';
import type { components } from '../../src/lib/api/generated/wire.ts';

const run = promisify(execFile);
const repository = resolve(import.meta.dirname, '../../..');

export type Stage = 'imported' | 'backend' | 'proxy' | 'assets';
export type Catalog = {
  root: string;
  origin: string;
  backendOrigin: string;
  pid: number;
  diskID: string;
  snapshots: components['schemas']['Snapshot'][];
  close: () => Promise<void>;
};

async function terminate(child: ChildProcess) {
  if (!child.pid || child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise<void>((done) => child.once('exit', () => done()));
  child.kill('SIGTERM');
  const result = await Promise.race([
    exited.then(() => true),
    delay(12_000, undefined, { ref: false }).then(() => false),
  ]);
  if (!result) {
    child.kill('SIGKILL');
    await exited;
    throw new Error('Backend needed SIGKILL during cleanup');
  }
}

export async function startCatalog(
  onStage?: (stage: Stage, state: Partial<Catalog>) => void,
  assetsDirectory?: string,
): Promise<Catalog> {
  const root = await mkdtemp('/tmp/opencode/coldcat-browser-');
  let child: ChildProcess | undefined;
  let vite: ViteDevServer | undefined;
  let closing: Promise<void> | undefined;
  const lifetime = new AbortController();
  const commands = new Set<Promise<unknown>>();
  async function command(binary: string, args: string[], timeout: number) {
    const pending = run(binary, args, {
      cwd: repository,
      timeout,
      signal: lifetime.signal,
    });
    commands.add(pending);
    try {
      return await pending;
    } finally {
      commands.delete(pending);
    }
  }
  const close = () => {
    closing ??= (async () => {
      lifetime.abort();
      await Promise.allSettled([...commands]);
      try {
        await vite?.close();
      } finally {
        try {
          if (child) await terminate(child);
        } finally {
          await rm(root, { recursive: true, force: true });
          process.off('SIGINT', interrupt);
          process.off('SIGTERM', interrupt);
        }
      }
    })();
    return closing;
  };
  const interrupt = () => void close().finally(() => process.exit(130));
  process.once('SIGINT', interrupt);
  process.once('SIGTERM', interrupt);
  try {
    const binary = join(root, 'coldcat');
    await command('go', ['build', '-o', binary, './cmd/coldcat'], 120_000);
    const cli = (...args: string[]) =>
      command(binary, ['--db', join(root, 'catalog.sqlite'), ...args], 30_000);
    for (const label of [fixture.label, 'Backup β', 'Third γ']) {
      await cli(
        'disk',
        'create',
        '--label',
        label,
        '--capacity',
        fixture.capacity,
        '--serial',
        fixture.serial,
        '--notes',
        fixture.notes,
      );
    }
    for (const inventory of inventories) {
      const path = join(root, `${inventory.name}.cshd`);
      await writeFile(path, '# version 1\n' + inventory.records);
      await cli('import', '--label', inventory.label, '--captured-at', inventory.capturedAt, path);
    }
    const disks = JSON.parse(
      (await cli('disk', 'list', '--json')).stdout,
    ) as components['schemas']['DiskSummary'][];
    const disk = disks.find((disk) => disk.label === fixture.label);
    if (!disk) throw new Error('Imported fixture disk missing');
    const snapshots = JSON.parse(
      (await cli('snapshot', 'list', '--disk-id', disk.id, '--json')).stdout,
    ) as components['schemas']['Snapshot'][];
    onStage?.('imported', { root, diskID: disk.id, snapshots });
    child = spawn(
      binary,
      [
        '--db',
        join(root, 'catalog.sqlite'),
        'serve',
        '--listen',
        '127.0.0.1:0',
        ...(assetsDirectory === undefined ? [] : ['--assets', assetsDirectory]),
      ],
      { stdio: ['ignore', 'pipe', 'pipe'] },
    );
    let logs = '';
    let launchError: Error | undefined;
    child.on('error', (error) => {
      launchError = error;
    });
    child.stdout?.on('data', (chunk) => {
      logs = (logs + chunk).slice(-16_384);
    });
    child.stderr?.on('data', (chunk) => {
      logs = (logs + chunk).slice(-16_384);
    });
    const deadline = Date.now() + 30_000;
    let backendOrigin: string | undefined;
    while (Date.now() < deadline) {
      if (launchError) throw launchError;
      if (child.exitCode !== null || child.signalCode !== null)
        throw new Error(`Backend exited before readiness: ${logs}`);
      backendOrigin = logs.match(/serving (http:\/\/127\.0\.0\.1:\d+)/)?.[1];
      if (backendOrigin) {
        const health = await fetch(`${backendOrigin}/healthz`, {
          signal: AbortSignal.timeout(1000),
        });
        if (health.ok && (await health.json()).status === 'ready') break;
      }
      await delay(25);
    }
    if (!backendOrigin || Date.now() >= deadline)
      throw new Error(`Backend readiness deadline exceeded: ${logs}`);
    if (!child.pid) throw new Error('Backend PID unavailable');
    onStage?.('backend', { root, backendOrigin, pid: child.pid });
    let origin = backendOrigin;
    if (assetsDirectory === undefined) {
      vite = await createServer({
        root: resolve(repository, 'frontend'),
        server: {
          host: '127.0.0.1',
          port: 0,
          strictPort: true,
          proxy: proxyRules(backendTarget(backendOrigin)),
        },
      });
      await vite.listen();
      const proxyOrigin = vite.resolvedUrls?.local[0];
      if (!proxyOrigin) throw new Error('Frontend origin unavailable');
      origin = proxyOrigin;
    }
    for (const path of ['healthz', 'api/v1/catalog']) {
      const response = await fetch(new URL(path, origin), { signal: AbortSignal.timeout(5000) });
      if (!response.ok) throw new Error(`Host readiness failed: ${path} ${response.status}`);
      await response.json();
    }
    const catalog = {
      root,
      origin,
      backendOrigin,
      pid: child.pid,
      diskID: disk.id,
      snapshots,
      close,
    };
    onStage?.(assetsDirectory === undefined ? 'proxy' : 'assets', catalog);
    return catalog;
  } catch (error) {
    await close();
    throw error;
  }
}
