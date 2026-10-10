import { describe, expect, it } from 'vitest';
import {
  decimal,
  formatBytes,
  formatCount,
  percentage,
  otherCount,
  capacityInput,
} from '../../src/lib/format/decimal';
import { presentDate } from '../../src/lib/format/date';
import { breadcrumbs, containingDirectory, directoryURL } from '../../src/lib/format/path';
import { validateGlobRules } from '../../src/lib/api/query';

describe('exact decimal helpers', () => {
  it('formats huge values, known zero and unknown distinctly', () => {
    expect(decimal('9007199254740993')).toBe(9007199254740993n);
    expect(formatCount('9007199254740993', 'en-US')).toBe('9,007,199,254,740,993');
    expect(formatBytes('9007199254740993', 'en-US')).toBe('9,007,199,254,740,993 B');
    expect(formatBytes('0', 'en-US')).toBe('0 B');
    expect(formatBytes(null, 'en-US')).toBe('Unknown');
    expect(() => decimal(9007199254740992 as unknown as string)).toThrow();
    expect(() => capacityInput(1 as unknown as string)).toThrow();
  });
  it.each(['-1', '01', '1.2', ' 1', '1\n', '1\r', '', '9223372036854775808'])(
    'rejects malformed/out-of-range wire decimal %s',
    (value) => {
      expect(() => decimal(value)).toThrow();
    },
  );
  it('validates exact capacity input without trimming or losing precision', () => {
    expect(capacityInput('0001')).toBe('1');
    expect(capacityInput('9223372036854775807')).toBe('9223372036854775807');
    for (const value of ['', ' 1', '1\n', '-1', '1e3', '9223372036854775808'])
      expect(() => capacityInput(value)).toThrow();
  });
  it('uses integer arithmetic for percentages and zero-safe content other counts', () => {
    expect(percentage('9007199254740993', '18014398509481986')).toBe('50.0%');
    expect(percentage('1', '3')).toBe('33.3%');
    expect(percentage('0', '0')).toBeNull();
    expect(() => percentage('2', '1')).toThrow();
    expect(otherCount('0')).toBe('0');
    expect(otherCount('3')).toBe('2');
  });
});

describe('distinct date meanings', () => {
  it('labels capture/import/source mtime and retains exact UTC nanoseconds', () => {
    const utc = '1970-01-01T00:00:10.000000123Z';
    expect(presentDate(utc, 'captured_at', 'en-US', 'UTC')).toMatchObject({
      label: 'Captured at',
      exactUTC: utc,
    });
    expect(presentDate(utc, 'imported_at', 'en-US', 'UTC').label).toBe('Imported at');
    expect(presentDate(null, 'mtime', 'en-US', 'UTC')).toEqual({
      label: 'Source mtime',
      text: 'Unknown',
      exactUTC: null,
    });
    expect(() => presentDate('2026-02-30T00:00:00Z', 'mtime')).toThrow();
  });
});

describe('literal directory paths', () => {
  it('uses slash-only breadcrumbs without case/Unicode/backslash normalization', () => {
    expect(breadcrumbs('É/e\u0301/a\\b')).toEqual([
      { name: 'É', path: 'É' },
      { name: 'e\u0301', path: 'É/e\u0301' },
      { name: 'a\\b', path: 'É/e\u0301/a\\b' },
    ]);
    expect(breadcrumbs('')).toEqual([]);
    expect(containingDirectory('a\\b')).toBe('');
    expect(containingDirectory('foo/foobar/file')).toBe('foo/foobar');
    const url = new URL(directoryURL('9007199254740993', 'É/+?&#\\'), 'http://localhost');
    expect(url.pathname).toBe('/snapshots/9007199254740993/directory');
    expect(url.searchParams.get('path')).toBe('É/+?&#\\');
    expect(directoryURL('1', '')).toBe('/snapshots/1/directory?path=');
  });
  it.each(['/foo', 'foo/', 'foo//bar', '.', 'a/../b', 'a/./b', 'a\0b', '\ud800'])(
    'rejects invalid paths %s',
    (path) => {
      expect(() => breadcrumbs(path)).toThrow();
    },
  );
});

describe('glob limits without interpreting patterns', () => {
  it('preserves rules and enforces combined UTF-8 byte/count limits', () => {
    expect(() => validateGlobRules({ allow: [' **/a\\? '], block: ['{a,b}/**'] })).not.toThrow();
    for (const rules of [
      { allow: ['é'.repeat(513)] },
      { block: [''] },
      { allow: Array(51).fill('a'), block: Array(50).fill('b') },
      { allow: Array(17).fill('a'.repeat(1024)) },
    ])
      expect(() => validateGlobRules(rules)).toThrow();
    expect(() => validateGlobRules({ allow: ['é'.repeat(512)] })).not.toThrow();
  });
});
