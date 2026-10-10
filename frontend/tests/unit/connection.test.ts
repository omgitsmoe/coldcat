import { afterEach, describe, expect, it, vi } from 'vitest';
import { Connection } from '../../src/lib/state/connection';
import { createClient } from '../../src/lib/api/client';
import { ApiError } from '../../src/lib/api/errors';

afterEach(() => vi.useRealTimers());
const catalog = {
  revision: '1',
  scope: 'current',
  disk_count: '0',
  file_count: '0',
  content_count: '0',
};
const response = (data: unknown) =>
  new Response(JSON.stringify(data), { headers: { 'Content-Type': 'application/json' } });

describe('connection context', () => {
  it('rejects late connection responses and aborts teardown work', async () => {
    const client = createClient();
    let finish!: (value: typeof catalog) => void;
    const late = new Promise<typeof catalog>((resolve) => {
      finish = resolve;
    });
    let signal!: AbortSignal;
    client.health = vi.fn().mockResolvedValue({ status: 'ready' });
    client.catalog = vi
      .fn()
      .mockImplementationOnce((s: AbortSignal) => {
        signal = s;
        return late;
      })
      .mockResolvedValue({ ...catalog, revision: '99' });
    const connection = new Connection(client);
    const first = connection.check();
    await Promise.resolve();
    await connection.check();
    expect(signal.aborted).toBe(true);
    finish(catalog);
    await first;
    expect(connection.state).toMatchObject({ status: 'ready', catalog: { revision: '99' } });
    let finishHealth!: (value: { status: 'ready' }) => void;
    client.health = vi.fn().mockImplementation((s: AbortSignal) => {
      signal = s;
      return new Promise((resolve) => {
        finishHealth = resolve;
      });
    });
    const pending = connection.check();
    connection.dispose();
    expect(signal.aborted).toBe(true);
    finishHealth({ status: 'ready' });
    await pending;
    expect(connection.state.status).toBe('checking');
  });
  it('refreshes catalog and invalidates on revision, metadata writes and same-revision reconnect', async () => {
    const fetcher = vi.fn(async (url) =>
      response(url === '/healthz' ? { status: 'ready' } : catalog),
    );
    const connection = new Connection(createClient(fetcher));
    const invalidate = vi.fn();
    connection.onInvalidate(invalidate);
    await connection.check();
    expect(connection.state.status).toBe('ready');
    expect(fetcher).toHaveBeenCalledTimes(2);
    await connection.check();
    expect(invalidate).not.toHaveBeenCalled();
    connection.metadataChanged();
    expect(invalidate).toHaveBeenCalledExactlyOnceWith('metadata');
    catalog.revision = '2';
    await connection.check();
    expect(invalidate).toHaveBeenCalledWith('revision');
    connection.report(new ApiError('transport', 'offline'));
    expect(connection.state.status).toBe('unavailable');
    await connection.check();
    expect(invalidate).toHaveBeenCalledWith('reconnect');
    connection.dispose();
    catalog.revision = '1';
  });
  it('uses one bounded recheck schedule, preserves errors, and stops on dispose', async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn().mockRejectedValue(new TypeError('offline'));
    const connection = new Connection(createClient(fetcher));
    await connection.check();
    await vi.runAllTimersAsync();
    expect(fetcher).toHaveBeenCalledTimes(4);
    expect(connection.state.status).toBe('unavailable');
    await connection.retry();
    expect(fetcher).toHaveBeenCalledTimes(5);
    connection.dispose();
    await vi.runAllTimersAsync();
    expect(fetcher).toHaveBeenCalledTimes(5);
  });
  it('does not classify parameter/not-found/contract failures as disconnects', async () => {
    const connection = new Connection(
      createClient(async (url) => response(url === '/healthz' ? { status: 'ready' } : catalog)),
    );
    await connection.check();
    for (const error of [
      new ApiError('http', 'bad', { status: 400 }),
      new ApiError('http', 'missing', { status: 404 }),
      new ApiError('contract', 'bad payload'),
    ])
      connection.report(error);
    expect(connection.state.status).toBe('ready');
    connection.dispose();
  });
});
