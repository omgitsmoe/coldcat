import { describe, expect, it } from 'vitest';
import {
  advancedCount,
  boundsMode,
  boundsQuery,
  searchSummary,
} from '../../src/lib/features/search/filters';
import { parseSearch } from '../../src/lib/state/routes';

describe('explicit replica matching', () => {
  it.each([
    ['', '', '', 'any'],
    ['0', '', '', 'exact'],
    ['', '0', '', 'range'],
    ['', '', '9223372036854775807', 'range'],
    ['0', '1', '', 'conflict'],
    ['0', '', '2', 'conflict'],
  ] as const)('restores %s / %s / %s as %s', (exact, minimum, maximum, expected) => {
    expect(boundsMode(exact, minimum, maximum)).toBe(expected);
  });
  it('projects only the deliberately selected mode without mutating retained draft values', () => {
    const draft = { exact: '0', minimum: '9007199254740993', maximum: '9223372036854775807' };
    expect(boundsQuery({ ...draft, mode: 'any' })).toEqual({});
    expect(boundsQuery({ ...draft, mode: 'exact' })).toEqual({ other_replicas: '0' });
    expect(boundsQuery({ ...draft, mode: 'range' })).toEqual({
      min_other_replicas: '9007199254740993',
      max_other_replicas: '9223372036854775807',
    });
    expect(draft).toEqual({
      exact: '0',
      minimum: '9007199254740993',
      maximum: '9223372036854775807',
    });
  });
  it('rejects empty active modes and conflicts rather than turning them into Any', () => {
    for (const mode of ['exact', 'range', 'conflict'] as const)
      expect(() => boundsQuery({ mode, exact: '', minimum: '', maximum: '' })).toThrow();
    expect(boundsQuery({ mode: 'range', exact: '', minimum: '', maximum: '0' })).toEqual({
      max_other_replicas: '0',
    });
  });
});

describe('advanced exposure and applied summaries', () => {
  it.each([
    ['wildcard', 'personal*foo', 'Wildcard'],
    ['expression', '(personal OR work) AND foo', 'AND / OR'],
  ] as const)('retains %s syntax and identifies its mode', (match, q, label) => {
    const route = parseSearch(new URLSearchParams({ match, q, field: 'path' }));
    expect(route.q).toBe(q);
    expect(route.match).toBe(match);
    expect(searchSummary(route)).toBe(`Path · ${label} · Current · Query: ${q}`);
  });
  it('counts root and zero bounds as active, not each range endpoint', () => {
    const route = parseSearch(
      new URLSearchParams(
        'q=abc&disk_id=1&snapshot_id=2&directory=&replica_metric=locations&min_other_replicas=0&max_other_replicas=2',
      ),
    );
    expect(advancedCount(route)).toBe(5);
    expect(searchSummary(route)).toBe(
      'Name · Contains · Current · Query: abc · Disk 1 · Snapshot 2 · Directory: root · Other locations: ≥ 0 · Other locations: ≤ 2',
    );
  });
  it('retains full literal query/path and identifies exact/history restrictions', () => {
    const route = {
      q: ' 雪😀/? ',
      field: 'path',
      match: 'exact',
      scope: 'history',
      directory: '雪 /?#%',
      disk_id: '1',
    } as const;
    expect(searchSummary(route)).toBe(
      'Path · Exact · History · Query:  雪😀/?  · Disk 1 · Directory: 雪 /?#%',
    );
    expect(advancedCount(route)).toBe(2);
  });
  it('exposes a nondefault metric even without bounds or with membership', () => {
    expect(searchSummary({ q: 'abc', replica_metric: 'locations' })).toContain(
      'Other-copy metric: locations',
    );
    expect(searchSummary({ q: 'abc', replica_metric: 'locations', disk_id: '1' })).toContain(
      'Other-copy metric: locations',
    );
    expect(advancedCount({ q: 'abc' })).toBe(0);
  });
});
