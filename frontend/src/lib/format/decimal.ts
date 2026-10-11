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

export type ByteUnits = 'binary' | 'decimal';

export function readableBytes(
  value: string | null,
  units: ByteUnits = 'binary',
  locale?: string,
): { text: string; approximate: boolean; exactBytes: string | null; scaled: boolean } {
  if (value === null)
    return { text: 'Unknown', approximate: false, exactBytes: null, scaled: false };

  const bytes = decimal(value);
  const base = units === 'binary' ? 1024n : 1000n;
  const labels =
    units === 'binary'
      ? ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']
      : ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB'];
  let power = 1n;
  let index = 0;
  while (index < labels.length - 1 && bytes >= power * base) {
    power *= base;
    index++;
  }
  let hundredths = (bytes * 100n + power / 2n) / power;
  if (hundredths >= base * 100n && index < labels.length - 1) {
    power *= base;
    index++;
    hundredths = (bytes * 100n + power / 2n) / power;
  }
  const fraction = (hundredths % 100n).toString().padStart(2, '0').replace(/0+$/, '');
  const amount = `${hundredths / 100n}${fraction ? `.${fraction}` : ''}`;
  const approximate = hundredths * power !== bytes * 100n;
  return {
    text: index === 0 ? formatBytes(value, locale) : `${amount} ${labels[index]}`,
    approximate,
    exactBytes: formatBytes(value, locale),
    scaled: index !== 0,
  };
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
