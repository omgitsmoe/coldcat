import { describe, expect, it } from 'vitest';
import {
  routes,
  parseSearch,
  parseDirectory,
  parseDetailContext,
  parseContentRoute,
  parseInventoryRoute,
  contentURL,
  searchURL,
  directoryBrowseURL,
  comparisonURL,
  safeSearchReturn,
} from '../../src/lib/state/routes';

describe('literal route state', () => {
  it('keeps inventory paging and validated detail context independent', () => {
    const context = { observation: '9007199254740993', returnTo: '/?q=+literal%26' };
    for (const build of [routes.disk, routes.snapshot]) {
      const url = new URL(build('9007199254740993', context), 'http://local');
      expect(parseDetailContext(url.searchParams)).toEqual(context);
      url.searchParams.set('limit', '1');
      expect(parseInventoryRoute(url.searchParams)).toEqual({ context, limit: 1 });
    }
    for (const query of [
      'cursor=x',
      'limit=0',
      'limit=1&limit=2',
      'return_to=%2Fdisks',
      'scope=history',
    ]) {
      expect(() => parseInventoryRoute(new URLSearchParams(query))).toThrow();
    }
  });
  it('round trips defaults, literal whitespace, Unicode, empty roots and huge IDs', () => {
    const input = parseSearch(
      new URLSearchParams('q=+%E9%9B%AA%26%3F+&disk_id=9007199254740993&directory='),
    );
    expect(input.q).toBe(' 雪&? ');
    expect(input.match).toBe('substring');
    expect(parseSearch(new URL(searchURL(input), 'http://local').searchParams)).toEqual(input);
    expect(
      parseDirectory(
        new URL(directoryBrowseURL('1', { path: '', recursive: true }), 'http://local')
          .searchParams,
      ),
    ).toMatchObject({ path: '', recursive: true });
    expect(
      routes.content('9007199254740993', { returnTo: searchURL(input), observation: '2' }),
    ).toContain('return_to=');
    expect(
      parseDetailContext(new URLSearchParams('observation=2&return_to=%2F%3Fq%3Dabc')),
    ).toEqual({ observation: '2', returnTo: '/?q=abc' });
  });
  it.each([
    'q=a&q=b',
    'q=a&match=fuzzy',
    'q=a&limit=NaN',
    'q=a&cursor=bookmark',
    'q=a&unknown=x',
    'q=a&disk_id=0',
  ])('rejects malformed route state: %s', (query) => {
    expect(() => parseSearch(new URLSearchParams(query))).toThrow();
  });
  it('rejects external or disguised returns and preserves repeated escaped rules', () => {
    for (const value of [
      '//evil.test',
      '/\\evil.test',
      'https://evil.test/',
      '/disks',
      '/?cursor=x',
      '/?q=a#fragment',
    ]) {
      expect(safeSearchReturn(value)).toBeUndefined();
    }
    const url = new URL(
      comparisonURL('1', 'replicas', { path: '雪/a?&', allow: [' a\\* ', '[x]'], block: ['?'] }),
      'http://local',
    );
    expect(url.searchParams.getAll('allow')).toEqual([' a\\* ', '[x]']);
    expect(url.searchParams.get('path')).toBe('雪/a?&');
    expect(() => routes.disk('../x')).toThrow();
  });
  it('keeps content history and return context independent, and rejects cursors in builders', () => {
    const context = { observation: '2', returnTo: '/?q=abc' };
    const url = new URL(contentURL('1', { scope: 'history', limit: 50 }, context), 'http://local');
    expect(parseContentRoute(url.searchParams)).toEqual({
      context,
      locations: { scope: 'history', limit: 50 },
    });
    const bookmarked = { q: 'abc', cursor: 'opaque' };
    expect(() => searchURL(bookmarked)).toThrow('session state');
    const locations = { scope: 'current' as const, cursor: 'opaque' };
    expect(() => contentURL('1', locations)).toThrow('session state');
    expect(() => directoryBrowseURL('1', { path: '', ...locations })).toThrow('session state');
    expect(() => comparisonURL('1', 'coverage', { path: '', ...locations })).toThrow(
      'session state',
    );
  });
});
