import { describe, expect, it } from 'vitest';
import { contentsURL, parseContents } from '../../src/lib/state/routes';
import { otherCount } from '../../src/lib/format/decimal';

describe('distinct-content route contract', () => {
  it('defaults to current other disks and preserves literal membership and huge bounds', () => {
    expect(parseContents(new URLSearchParams())).toEqual({
      scope: 'current',
      replica_metric: 'disks',
      limit: 50,
    });
    const query = parseContents(
      new URLSearchParams({
        disk_id: '9007199254740993',
        directory: 'É/*?\\literal',
        min_other_replicas: '9007199254740993',
        replica_metric: 'locations',
      }),
    );
    expect(parseContents(new URL(contentsURL(query), 'http://local').searchParams)).toEqual(query);
    expect(parseContents(new URLSearchParams('disk_id=1&directory=')).directory).toBe('');
  });

  it.each([
    'directory=foo',
    'scope=history&other_replicas=0',
    'other_replicas=0&min_other_replicas=1',
    'min_other_replicas=2&max_other_replicas=1',
    'disk_id=0',
    'directory=../foo&disk_id=1',
    'scope=current&scope=history',
    'cursor=opaque',
    'limit=201',
    'other_replicas=01',
    'export=true',
  ])('rejects invalid combinations without dropping inputs: %s', (query) => {
    expect(() => parseContents(new URLSearchParams(query))).toThrow();
  });

  it('uses zero-safe content-level subtraction, not observation subtraction', () => {
    expect(otherCount('0')).toBe('0');
    expect(otherCount('1')).toBe('0');
    expect(otherCount('9007199254740993')).toBe('9007199254740992');
  });
});
