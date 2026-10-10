import type { Operation } from './schema';
import { assertSchema } from './decode';
import { literalText } from '../format/path';

export function validateGlobRules(rules: { allow?: string[]; block?: string[] }): void {
  const patterns = [...(rules.allow ?? []), ...(rules.block ?? [])];
  if (patterns.length > 100) throw new Error('At most 100 allow/block patterns combined');
  let total = 0;
  for (const pattern of patterns) {
    literalText(pattern);
    const bytes = new TextEncoder().encode(pattern).length;
    if (bytes === 0 || bytes > 1024) throw new Error('Each pattern must be 1..1024 UTF-8 bytes');
    total += bytes;
  }
  if (total > 16384) throw new Error('Patterns exceed 16384 UTF-8 bytes combined');
}

export function encodeQuery(operation: Operation, input: object): string {
  const query = input as Record<string, unknown>;
  for (const key of Object.keys(query)) {
    if (!Object.hasOwn(operation.query, key)) throw new Error(`Unexpected query key: ${key}`);
  }
  const params = new URLSearchParams();
  for (const [key, parameter] of Object.entries(operation.query)) {
    const value = query[key];
    if (value === undefined) {
      if (parameter.required) throw new Error(`Missing query key: ${key}`);
      continue;
    }
    assertSchema(value, parameter.schema, key, 'request');
    const values = Array.isArray(value) ? value : [value];
    for (const entry of values) {
      if (typeof entry === 'string') literalText(entry);
      params.append(key, String(entry));
    }
  }
  if (Object.hasOwn(operation.query, 'allow')) {
    validateGlobRules(query as { allow?: string[]; block?: string[] });
  }
  const encoded = params.toString();
  return encoded ? `?${encoded}` : '';
}
