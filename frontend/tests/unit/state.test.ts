import { describe, expect, it, vi } from 'vitest';
import { RequestSlot } from '../../src/lib/state/request';
import { CursorTraversal, traversalKey } from '../../src/lib/state/traversal';
import { ApiError } from '../../src/lib/api/errors';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

describe('request ownership', () => {
  it('aborts replacement, ignores late success and rejection, and disposes', async () => {
    const slot = new RequestSlot<string>();
    const old = deferred<string>();
    let signal!: AbortSignal;
    const pending = slot.run((s) => {
      signal = s;
      return old.promise;
    });
    await slot.run(async () => 'latest');
    expect(signal.aborted).toBe(true);
    old.resolve('old');
    await pending;
    expect(slot.state).toEqual({ status: 'success', value: 'latest' });
    const late = deferred<string>();
    const failure = slot.run(() => late.promise);
    slot.cancel();
    late.reject(new Error('late'));
    await failure;
    expect(slot.state.status).toBe('idle');
    slot.dispose();
    await expect(slot.run(async () => 'no')).rejects.toThrow('disposed');
  });
});

describe('cursor windows', () => {
  const identity = { endpoint: 'search', resource: '', query: '?q=literal&limit=50' };
  it('includes resource, endpoint, filters and limit in identity without decoding cursors', () => {
    expect(traversalKey(identity)).not.toBe(traversalKey({ ...identity, resource: '2' }));
    expect(traversalKey(identity)).not.toBe(
      traversalKey({ ...identity, query: '?q=literal&limit=1' }),
    );
  });
  it('reaches later pages with bounded retention and cached previous/next navigation', async () => {
    const load = vi.fn(async (cursor: string | undefined) => {
      const n = cursor === undefined ? 1 : Number(cursor);
      return { items: [n], revision: '1', next_cursor: n === 15 ? null : String(n + 1) };
    });
    const pages = new CursorTraversal(identity, load);
    await pages.restart();
    for (let n = 2; n <= 10; n++) await pages.more();
    expect(pages.items).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    expect(pages.canMore).toBe(false);
    for (let n = 11; n <= 15; n++) await pages.next();
    expect(pages.items).toEqual([15]);
    expect(pages.state.pages).toHaveLength(10);
    expect(pages.state.pages[0]!.number).toBe(6);
    pages.previous();
    expect(pages.items).toEqual([14]);
    await pages.next();
    expect(load).toHaveBeenCalledTimes(15);
    expect(pages.canNext).toBe(false);
  });
  it('keeps successful pages on page error; stale cursor or revision mismatch clears them', async () => {
    const load = vi
      .fn()
      .mockResolvedValueOnce({ items: [1], revision: '1', next_cursor: 'opaque' })
      .mockRejectedValueOnce(new ApiError('http', 'Unavailable', { status: 503 }))
      .mockResolvedValueOnce({ items: [2], revision: '2', next_cursor: null });
    const pages = new CursorTraversal(identity, load);
    await pages.restart();
    await pages.more();
    expect(pages.items).toEqual([1]);
    expect(pages.state.status).toBe('failure');
    await pages.more();
    expect(pages.state.status).toBe('stale');
    expect(pages.items).toEqual([]);
    load
      .mockResolvedValueOnce({ items: [3], revision: '2', next_cursor: 'new' })
      .mockRejectedValueOnce(
        new ApiError('http', 'changed', { status: 409, code: 'stale_cursor' }),
      );
    await pages.restart();
    await pages.more();
    expect(pages.state.status).toBe('stale');
    expect(pages.items).toEqual([]);
  });
  it('parameter retirement cancels continuation and rejects late pages', async () => {
    const late = deferred<{ items: number[]; revision: string; next_cursor: null }>();
    let signal!: AbortSignal;
    const pages = new CursorTraversal(identity, async (cursor, s) => {
      signal = s;
      return cursor ? late.promise : { items: [1], revision: '1', next_cursor: 'next' };
    });
    await pages.restart();
    const more = pages.more();
    pages.retire();
    expect(signal.aborted).toBe(true);
    late.resolve({ items: [2], revision: '1', next_cursor: null });
    await more;
    expect(pages.items).toEqual([]);
    expect(pages.state.status).toBe('idle');
  });
  it('suspends cursor reuse but keeps cached navigation visibly unverified', async () => {
    const load = vi.fn(async (cursor: string | undefined) => ({
      items: [cursor ?? 'first'],
      revision: '1',
      next_cursor: 'next',
    }));
    const pages = new CursorTraversal(identity, load, 3);
    await pages.restart();
    await pages.more();
    await pages.more();
    await pages.next();
    pages.suspend();
    pages.previous();
    expect(pages.state.active).toBe(3);
    expect(pages.state.status).toBe('failure');
    await pages.next();
    expect(pages.state.active).toBe(4);
    expect(pages.state.status).toBe('failure');
    await pages.next();
    expect(load).toHaveBeenCalledTimes(4);
    expect(pages.canNext).toBe(false);
    expect(pages.canMore).toBe(false);
    expect(pages.state.status).toBe('failure');
    await pages.restart();
    expect(load.mock.calls.at(-1)?.[0]).toBeUndefined();
    expect(pages.canNext).toBe(true);
  });
});
