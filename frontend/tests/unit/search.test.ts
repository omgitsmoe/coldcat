import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  acquireSearch,
  retainSearch,
  searchIssue,
  SearchWorkspace,
} from '../../src/lib/features/search/workspace';
import { parseSearch } from '../../src/lib/state/routes';
import { Connection } from '../../src/lib/state/connection';
import { createClient } from '../../src/lib/api/client';
import { searchFixture } from '../fixtures/api';

const input = (q: string, extra = '') =>
  parseSearch(new URLSearchParams(`q=${encodeURIComponent(q)}${extra}`));
afterEach(() => vi.useRealTimers());

describe('search dispatch eligibility', () => {
  it('counts code points, preserves literal input and enforces UTF-8/NUL', () => {
    expect(searchIssue(input(''))?.kind).toBe('idle');
    expect(searchIssue(input('😀雪'))?.kind).toBe('short');
    expect(searchIssue(input('😀雪 '))).toBeUndefined();
    expect(searchIssue(input(' ', '&match=exact'))).toBeUndefined();
    expect(searchIssue({ ...input('abc'), q: 'é'.repeat(513) })?.kind).toBe('invalid');
    expect(searchIssue({ ...input('abc'), q: 'a\0b' })?.kind).toBe('invalid');
    expect(searchIssue({ ...input('abc'), q: '\ud800' })?.kind).toBe('invalid');
  });
  it('rejects invalid filter combinations without removing them', () => {
    for (const extra of [
      '&directory=foo',
      '&scope=history&other_replicas=0',
      '&other_replicas=0&min_other_replicas=0',
      '&min_other_replicas=2&max_other_replicas=1',
      '&disk_id=1&directory=' + 'é'.repeat(513),
    ])
      expect(searchIssue(input('abc', extra))?.kind).toBe('invalid');
    expect(searchIssue(input('abc', '&scope=history&snapshot_id=1&directory='))).toBeUndefined();
  });
});

describe('search scheduling and ordering', () => {
  it('coalesces at 200ms, flushes, immediately retires selection and rejects late success', async () => {
    vi.useFakeTimers();
    const pending: Array<{
      resolve: (response: Response) => void;
      url: string;
      signal?: AbortSignal | null;
    }> = [];
    const connection = new Connection(
      createClient(
        (url, init) =>
          new Promise<Response>((resolve) => {
            pending.push({ resolve, url: String(url), signal: init?.signal });
          }),
      ),
    );
    const workspace = new SearchWorkspace(connection);
    workspace.update(input('old'));
    await vi.advanceTimersByTimeAsync(199);
    expect(pending).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(pending).toHaveLength(1);
    workspace.selected = '1';
    workspace.update(input('new'));
    expect(workspace.selected).toBeUndefined();
    expect(pending[0]!.signal?.aborted).toBe(true);
    workspace.flush();
    expect(pending).toHaveLength(2);
    const response = (name: string) => {
      const fixture = searchFixture();
      fixture.items[0]!.basename = name;
      return new Response(JSON.stringify(fixture), {
        headers: { 'Content-Type': 'application/json' },
      });
    };
    pending[1]!.resolve(response('new'));
    await vi.advanceTimersByTimeAsync(0);
    pending[0]!.resolve(response('old'));
    await vi.advanceTimersByTimeAsync(500);
    expect(workspace.items[0]?.basename).toBe('new');
    expect(pending).toHaveLength(2);
    workspace.dispose();
    connection.dispose();
  });
  it('retains only one successful bounded session, restores selection/scroll and invalidates metadata', async () => {
    const connection = new Connection(
      createClient(
        async () =>
          new Response(JSON.stringify(searchFixture()), {
            headers: { 'Content-Type': 'application/json' },
          }),
      ),
    );
    const route = input('report');
    const workspace = new SearchWorkspace(connection);
    workspace.update(route, true);
    await vi.waitFor(() => expect(workspace.state.status).toBe('success'));
    workspace.select('9007199254740993');
    workspace.scroll = 400;
    retainSearch(workspace);
    const acquired = acquireSearch(connection, route);
    expect(acquired.restored).toBe(true);
    expect(acquired.workspace).toBe(workspace);
    expect(workspace.selected).toBe('9007199254740993');
    expect(workspace.scroll).toBe(400);
    retainSearch(workspace);
    connection.metadataChanged();
    expect(workspace.items).toEqual([]);
    expect(workspace.selected).toBeUndefined();
    const fresh = acquireSearch(connection, route);
    expect(fresh.restored).toBe(false);
    fresh.workspace.dispose();
    connection.dispose();
  });
});
