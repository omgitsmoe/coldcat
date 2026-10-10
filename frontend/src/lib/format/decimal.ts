export const MAX_INT64 = 9223372036854775807n;

export function decimal(value: string): bigint {
  if (typeof value !== 'string' || /^(0|[1-9][0-9]*)$/.exec(value)?.[0] !== value)
    throw new Error('Expected canonical decimal text');
  const integer = BigInt(value);
  if (integer > MAX_INT64) throw new Error('Decimal exceeds signed 64-bit range');
  return integer;
}

export function capacityInput(value: string): string {
  if (typeof value !== 'string' || /^[0-9]+$/.exec(value)?.[0] !== value)
    throw new Error('Capacity must be exact decimal bytes');
  const canonical = BigInt(value).toString();
  decimal(canonical);
  return canonical;
}

export function formatCount(value: string, locale?: string): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(decimal(value));
}

export function formatBytes(value: string | null, locale?: string): string {
  return value === null ? 'Unknown' : `${formatCount(value, locale)} B`;
}

export function otherCount(currentCount: string): string {
  const count = decimal(currentCount);
  return (count === 0n ? 0n : count - 1n).toString();
}

export function percentage(covered: string, total: string): string | null {
  const numerator = decimal(covered);
  const denominator = decimal(total);
  if (numerator > denominator) throw new Error('Covered count exceeds total');
  if (denominator === 0n) return null;
  const tenths = (numerator * 1000n + denominator / 2n) / denominator;
  return `${tenths / 10n}.${tenths % 10n}%`;
}
