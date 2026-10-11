import { MAX_INT64 } from './decimal';

export const CAPACITY_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'KiB', 'MiB', 'GiB', 'TiB'] as const;
export type CapacityUnit = (typeof CAPACITY_UNITS)[number];

const multipliers: Record<CapacityUnit, bigint> = {
  B: 1n,
  KB: 1000n,
  MB: 1000n ** 2n,
  GB: 1000n ** 3n,
  TB: 1000n ** 4n,
  KiB: 1024n,
  MiB: 1024n ** 2n,
  GiB: 1024n ** 3n,
  TiB: 1024n ** 4n,
};

export function capacityBytes(amount: string, unit: CapacityUnit): string {
  if (/^[0-9]+(?:\.[0-9]+)?$/.exec(amount)?.[0] !== amount)
    throw new Error(
      'Capacity must be a non-negative decimal amount without exponents or separators.',
    );
  if (!Object.hasOwn(multipliers, unit)) throw new Error('Unsupported capacity unit.');

  const [whole, tail = ''] = amount.split('.');
  const fraction = tail.replace(/0+$/, '');
  const denominator = 10n ** BigInt(fraction.length);
  const numerator = BigInt(whole! + fraction) * multipliers[unit];
  if (numerator % denominator !== 0n)
    throw new Error('Capacity must resolve to whole bytes; no rounding.');

  const bytes = numerator / denominator;
  if (bytes > MAX_INT64)
    throw new Error('Capacity exceeds the signed 64-bit maximum of 9223372036854775807 bytes.');
  return bytes.toString();
}
