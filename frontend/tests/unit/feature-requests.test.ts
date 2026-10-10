import { describe, expect, it, vi } from 'vitest';
import { Connection } from '../../src/lib/state/connection';
import { createClient } from '../../src/lib/api/client';
import {
  searchPages,
  locationPages,
  diskPages,
  snapshotPages,
  directoryPages,
  replicaPages,
  coveragePages,
  contentDetail,
  observationDetail,
  diskDetail,
  snapshotDetail,
  directoryDetail,
} from '../../src/lib/state/feature-requests';
import { searchFixture } from '../fixtures/api';
import { ApiError } from '../../src/lib/api/errors';

describe('concrete feature contracts', () => {
  it('captures coverage rules, cancels late results and invalidates current destination context', async () => {
    const client = createClient();
    let release!: (value: { revision: string; items: never[]; next_cursor: null }) => void;
    const pending = new Promise<{ revision: string; items: never[]; next_cursor: null }>(
      (resolve) => {
        release = resolve;
      },
    );
    client.directoryCoverage = vi.fn().mockReturnValue(pending);
    const connection = new Connection(client);
    const query = { path: '雪/\\x', allow: [' **/a\\?.txt '], block: ['**/*.log'] };
    const bound = coveragePages(connection, '13', query);
    expect(client.directoryCoverage).not.toHaveBeenCalled();
    query.allow[0] = 'mutated';
    const running = bound.traversal.restart();
    expect(client.directoryCoverage).toHaveBeenCalledWith(
      '13',
      {
        path: '雪/\\x',
        allow: [' **/a\\?.txt '],
        block: ['**/*.log'],
        limit: 50,
        cursor: undefined,
      },
      expect.any(AbortSignal),
    );
    const signal = vi.mocked(client.directoryCoverage).mock.calls[0]![2]!;
    connection.metadataChanged();
    expect(signal.aborted).toBe(true);
    release({ revision: '1', items: [], next_cursor: null });
    await running;
    expect(bound.traversal.state.status).toBe('idle');
    expect(bound.traversal.state.pages).toHaveLength(0);
    bound.dispose();
    connection.dispose();
  });
  it('freezes literal replica rules and cancels obsolete comparisons without eager calls', async () => {
    const client = createClient();
    let release!: (value: { revision: string; items: never[]; next_cursor: null }) => void;
    const pending = new Promise<{ revision: string; items: never[]; next_cursor: null }>(
      (resolve) => {
        release = resolve;
      },
    );
    client.directoryReplicas = vi.fn().mockReturnValue(pending);
    const connection = new Connection(client);
    const query = { path: '雪/\\x', allow: [' **/a\\?.txt ', '**/*.jpg'], block: ['**/*.log'] };
    const bound = replicaPages(connection, '13', query);
    expect(client.directoryReplicas).not.toHaveBeenCalled();
    query.allow[0] = 'mutated';
    const running = bound.traversal.restart();
    expect(client.directoryReplicas).toHaveBeenCalledWith(
      '13',
      {
        path: '雪/\\x',
        allow: [' **/a\\?.txt ', '**/*.jpg'],
        block: ['**/*.log'],
        limit: 50,
        cursor: undefined,
      },
      expect.any(AbortSignal),
    );
    const signal = vi.mocked(client.directoryReplicas).mock.calls[0]![2]!;
    bound.traversal.retire();
    expect(signal.aborted).toBe(true);
    release({ revision: '1', items: [], next_cursor: null });
    await running;
    expect(bound.traversal.state.status).toBe('idle');
    expect(bound.traversal.state.pages).toHaveLength(0);
    bound.dispose();
    connection.dispose();
  });
  it('freezes search inputs, has equivalent default identity, and creates no speculative requests', async () => {
    const client = createClient();
    client.search = vi.fn().mockResolvedValue(searchFixture());
    const connection = new Connection(client);
    const query = { q: 'literal 雪&?' };
    const bound = searchPages(connection, query);
    const equivalent = searchPages(connection, { ...query, limit: 50, scope: 'current' });
    expect(bound.traversal.key).toBe(equivalent.traversal.key);
    expect(client.search).not.toHaveBeenCalled();
    query.q = 'changed';
    await bound.traversal.restart();
    expect(client.search).toHaveBeenCalledWith(
      expect.objectContaining({ q: 'literal 雪&?', limit: 50 }),
      expect.any(AbortSignal),
    );
    bound.dispose();
    equivalent.dispose();
    connection.dispose();
  });
  it('binds F4–F6 page resources and query defaults to their exact wrappers', async () => {
    const client = createClient();
    for (const key of [
      'contentObservations',
      'disks',
      'diskSnapshots',
      'directoryEntries',
    ] as const) {
      client[key] = vi.fn().mockResolvedValue({ revision: '1', items: [], next_cursor: null });
    }
    const connection = new Connection(client);
    const locations = locationPages(connection, '11', { scope: 'history' });
    const disks = diskPages(connection);
    const snapshots = snapshotPages(connection, '12');
    const directory = directoryPages(connection, '13', { path: '雪/[a]\\b' });
    for (const bound of [locations, disks, snapshots, directory]) await bound.traversal.restart();
    expect(client.contentObservations).toHaveBeenCalledWith(
      '11',
      { scope: 'history', limit: 50, cursor: undefined },
      expect.any(AbortSignal),
    );
    expect(client.disks).toHaveBeenCalledWith(
      { limit: 50, cursor: undefined },
      expect.any(AbortSignal),
    );
    expect(client.diskSnapshots).toHaveBeenCalledWith(
      '12',
      { limit: 50, cursor: undefined },
      expect.any(AbortSignal),
    );
    expect(client.directoryEntries).toHaveBeenCalledWith(
      '13',
      {
        path: '雪/[a]\\b',
        recursive: false,
        replica_metric: 'disks',
        limit: 50,
        cursor: undefined,
      },
      expect.any(AbortSignal),
    );
    connection.metadataChanged();
    for (const bound of [locations, disks, snapshots, directory]) {
      expect(bound.traversal.state.status).toBe('idle');
      bound.dispose();
    }
    connection.dispose();
  });
  it('retains page failure on disconnect, then retires data on reconnect at the same revision', async () => {
    const client = createClient();
    client.health = vi.fn().mockResolvedValue({ status: 'ready' });
    client.catalog = vi.fn().mockResolvedValue({
      revision: '1',
      scope: 'current',
      disk_count: '1',
      file_count: '1',
      content_count: '1',
    });
    client.search = vi
      .fn()
      .mockResolvedValueOnce({ ...searchFixture(), next_cursor: 'opaque' })
      .mockRejectedValueOnce(new ApiError('http', 'offline', { status: 503 }));
    const connection = new Connection(client);
    await connection.check();
    const bound = searchPages(connection, { q: 'abc' });
    await bound.traversal.restart();
    await bound.traversal.more();
    expect(bound.traversal.items).toHaveLength(1);
    expect(bound.traversal.state.status).toBe('failure');
    expect(connection.state.status).toBe('unavailable');
    await bound.traversal.more();
    await bound.traversal.next();
    expect(client.search).toHaveBeenCalledTimes(2);
    await connection.retry();
    expect(bound.traversal.items).toEqual([]);
    expect(bound.traversal.state.status).toBe('idle');
    bound.dispose();
    connection.dispose();
  });
  it('owns independent detail sections and invalidates metadata without revision changes', async () => {
    const client = createClient();
    client.content = vi.fn().mockResolvedValue({ id: '1' });
    client.contentObservations = vi
      .fn()
      .mockRejectedValue(new ApiError('http', 'failure', { status: 500 }));
    client.observation = vi.fn().mockResolvedValue({ id: '1' });
    client.disk = vi.fn().mockResolvedValue({ id: '1' });
    client.snapshot = vi.fn().mockResolvedValue({ id: '1' });
    client.directory = vi.fn().mockResolvedValue({ id: '1' });
    const connection = new Connection(client);
    const content = contentDetail(connection, '1');
    const locations = locationPages(connection, '1');
    const details = [
      content,
      observationDetail(connection, '2'),
      diskDetail(connection, '3'),
      snapshotDetail(connection, '4'),
      directoryDetail(connection, '4', ''),
    ];
    await Promise.all([
      locations.traversal.restart(),
      ...details.map((section) => section.reload()),
    ]);
    expect(content.request.state.status).toBe('success');
    expect(locations.traversal.state.status).toBe('failure');
    connection.metadataChanged();
    for (const section of details) {
      expect(section.request.state.status).toBe('idle');
      section.dispose();
    }
    locations.dispose();
    connection.dispose();
  });
});
