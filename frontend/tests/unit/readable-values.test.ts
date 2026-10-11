import { describe, expect, it } from 'vitest';
import { readableBytes, type ByteUnits } from '../../src/lib/format/decimal';
import { presentDate } from '../../src/lib/format/date';

describe('readable exact-integer byte presentation', () => {
  it.each([
    ['0', 'binary', '0 B', false],
    ['1', 'binary', '1 B', false],
    ['1023', 'binary', '1,023 B', false],
    ['1024', 'binary', '1 KiB', false],
    ['1536', 'binary', '1.5 KiB', false],
    ['1025', 'binary', '1 KiB', true],
    ['1030', 'binary', '1.01 KiB', true],
    ['1048575', 'binary', '1 MiB', true],
    ['1048576', 'binary', '1 MiB', false],
    ['9007199254740992', 'binary', '8 PiB', false],
    ['9007199254740993', 'binary', '8 PiB', true],
    ['9223372036854775807', 'binary', '8 EiB', true],
    ['999', 'decimal', '999 B', false],
    ['1000', 'decimal', '1 KB', false],
    ['1005', 'decimal', '1.01 KB', true],
    ['1010', 'decimal', '1.01 KB', false],
    ['1001', 'decimal', '1 KB', true],
    ['999999', 'decimal', '1 MB', true],
    ['1500000000000', 'decimal', '1.5 TB', false],
    ['9007199254740993', 'decimal', '9.01 PB', true],
    ['9223372036854775807', 'decimal', '9.22 EB', true],
  ] as const)('formats %s in %s units', (value, units, text, approximate) => {
    const result = readableBytes(value, units, 'en-US');
    expect(result.text).toBe(text);
    expect(result.approximate).toBe(approximate);
    expect(result.exactBytes).toBe(`${new Intl.NumberFormat('en-US').format(BigInt(value))} B`);
    expect(result.scaled).toBe(!text.endsWith(' B'));
  });

  it('keeps unknown separate from a known zero', () => {
    expect(readableBytes(null)).toEqual({
      text: 'Unknown',
      approximate: false,
      exactBytes: null,
      scaled: false,
    });
    expect(readableBytes('0').exactBytes).toBe('0 B');
  });

  it.each(['-1', '01', '1.5', '1e3', '', '9223372036854775808'])('rejects %s', (value) => {
    expect(() => readableBytes(value)).toThrow();
  });

  for (const units of ['binary', 'decimal'] satisfies ByteUnits[]) {
    it(`selects every ${units} unit without rounding an exact power`, () => {
      const base = units === 'binary' ? 1024n : 1000n;
      const labels =
        units === 'binary'
          ? ['KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']
          : ['KB', 'MB', 'GB', 'TB', 'PB', 'EB'];
      let power = 1n;
      for (const label of labels) {
        power *= base;
        expect(readableBytes(power.toString(), units)).toMatchObject({
          text: `1 ${label}`,
          approximate: false,
          scaled: true,
        });
      }
    });
  }
});

describe('compact dates with explicit zones and full precision', () => {
  it('defaults to UTC, includes seconds, and keeps nanoseconds unchanged', () => {
    const value = '2026-10-11T23:59:58.123456789Z';
    const result = presentDate(value, 'captured_at', 'en-US');
    expect(result.text).toContain('23:59:58');
    expect(result.text).toContain('UTC');
    expect(result.exactUTC).toBe(value);
  });

  it('keeps the zone explicit when a caller requests local time across a date boundary', () => {
    const value = '2026-01-01T00:00:00.000000001Z';
    const result = presentDate(value, 'imported_at', 'en-US', 'America/Los_Angeles');
    expect(result.text).toContain('Dec 31, 2025');
    expect(result.text).toContain('16:00:00');
    expect(result.text).toContain('PST');
    expect(result.exactUTC).toBe(value);
  });

  it.each([
    '2026-02-30T00:00:00Z',
    '2026-10-11T00:00:00+00:00',
    '2026-10-11T00:00:00.1234567890Z',
    '2026-10-11',
    'invalid',
  ])('rejects %s', (value) => {
    expect(() => presentDate(value, 'mtime')).toThrow();
  });
});
