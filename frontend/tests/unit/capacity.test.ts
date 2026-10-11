import { describe, expect, it } from 'vitest';
import { capacityBytes, CAPACITY_UNITS } from '../../src/lib/format/capacity';

describe('exact capacity amounts', () => {
  it.each([
    ['B', '1'],
    ['KB', '1000'],
    ['MB', '1000000'],
    ['GB', '1000000000'],
    ['TB', '1000000000000'],
    ['KiB', '1024'],
    ['MiB', '1048576'],
    ['GiB', '1073741824'],
    ['TiB', '1099511627776'],
  ] as const)('uses the exact %s multiplier', (unit, bytes) => {
    expect(capacityBytes('1', unit)).toBe(bytes);
    expect(capacityBytes('0.0000', unit)).toBe('0');
  });

  it('preserves specified examples, leading zeros and arbitrary byte precision', () => {
    expect(capacityBytes('1.5', 'TB')).toBe('1500000000000');
    expect(capacityBytes('1', 'TiB')).toBe('1099511627776');
    expect(capacityBytes('09007199254740993.000', 'B')).toBe('9007199254740993');
    expect(capacityBytes('0.001', 'KB')).toBe('1');
    expect(capacityBytes('0.0000000000009094947017729282379150390625', 'TiB')).toBe('1');
    expect(capacityBytes('1.' + '0'.repeat(100), 'B')).toBe('1');
  });

  it('accepts the signed 64-bit maximum exactly in decimal and binary units', () => {
    expect(capacityBytes('9223372036854775807', 'B')).toBe('9223372036854775807');
    expect(capacityBytes('9223372.036854775807', 'TB')).toBe('9223372036854775807');
    expect(capacityBytes('9007199254740991.9990234375', 'KiB')).toBe('9223372036854775807');
  });

  it.each(CAPACITY_UNITS)('rejects overflow in %s', (unit) => {
    expect(() => capacityBytes('9223372036854775808', unit)).toThrow('signed 64-bit');
  });

  it.each([
    '',
    '-1',
    '-0',
    '+1',
    '1e3',
    '1E3',
    '1,5',
    '1,000',
    '1_000',
    '1 000',
    ' 1',
    '1 ',
    '1\n',
    '.5',
    '1.',
    'NaN',
    'Infinity',
    '１',
    '1TB',
  ])('rejects %j', (amount) => {
    expect(() => capacityBytes(amount, 'TB')).toThrow('non-negative decimal');
  });

  it.each([
    ['0.5', 'B'],
    ['0.0001', 'KB'],
    ['0.1', 'KiB'],
    ['0.0000000000001', 'TB'],
    ['0.' + '0'.repeat(40) + '1', 'TiB'],
  ] as const)('rejects fractional bytes from %s %s without rounding', (amount, unit) => {
    expect(() => capacityBytes(amount, unit)).toThrow('whole bytes');
  });
});
