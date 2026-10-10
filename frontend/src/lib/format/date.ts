export type DateMeaning = 'captured_at' | 'imported_at' | 'mtime';

export function utcDate(value: string): Date {
  const match = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(?:\.\d{1,9})?Z$/.exec(value);
  if (!match) throw new Error('Expected UTC RFC3339 timestamp');
  const date = new Date(value);
  if (
    !Number.isFinite(date.getTime()) ||
    date.toISOString().slice(0, 19) !== `${match[1]}T${match[2]}`
  ) {
    throw new Error('Invalid UTC timestamp');
  }
  return date;
}

export function presentDate(
  value: string | null,
  meaning: DateMeaning,
  locale?: string,
  timeZone?: string,
): { label: string; text: string; exactUTC: string | null } {
  const label = { captured_at: 'Captured at', imported_at: 'Imported at', mtime: 'Source mtime' }[
    meaning
  ];
  return {
    label,
    text:
      value === null
        ? 'Unknown'
        : new Intl.DateTimeFormat(locale, {
            dateStyle: 'medium',
            timeStyle: 'long',
            timeZone,
          }).format(utcDate(value)),
    exactUTC: value,
  };
}
