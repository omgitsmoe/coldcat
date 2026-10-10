import { schemas } from './generated/contract';
import type { Schema } from './schema';
import { decimal, capacityInput } from '../format/decimal';
import { utcDate } from '../format/date';

export function assertSchema(
  value: unknown,
  schema: Schema,
  path = '$',
  mode: 'response' | 'request' = 'response',
): void {
  const fail = (reason: string): never => {
    throw new Error(`${path}: ${reason}`);
  };
  if (schema.ref) {
    const referred = (schemas as Record<string, Schema>)[schema.ref];
    if (!referred) throw new Error(`${path}: unknown schema reference`);
    assertSchema(value, referred, path, mode);
    return;
  }
  const types = Array.isArray(schema.type) ? schema.type : [schema.type];
  if (value === null && types.includes('null')) return;
  const kind = Array.isArray(value) ? 'array' : typeof value;
  if (
    value === null ||
    !types.some(
      (type) =>
        type === kind || (type === 'integer' && kind === 'number' && Number.isSafeInteger(value)),
    )
  )
    fail('unexpected type');
  if ('const' in schema && value !== schema.const) fail('unexpected constant');
  if (schema.enum && !schema.enum.includes(value)) fail('unexpected enum value');
  if (typeof value === 'string') {
    if (schema.pattern && !new RegExp(schema.pattern).test(value)) fail('invalid string pattern');
    const length = [...value].length;
    if (schema.minLength !== undefined && length < schema.minLength) fail('string too short');
    if (schema.maxLength !== undefined && length > schema.maxLength) fail('string too long');
    if (schema.format === 'date-time') utcDate(value);
    if (
      schema.pattern &&
      ['^(0|[1-9][0-9]*)$', '^[1-9][0-9]*$', '^[0-9]+$'].includes(schema.pattern)
    ) {
      if (mode === 'request' && schema.pattern === '^[0-9]+$') capacityInput(value);
      else decimal(value);
    }
    if (mode === 'response' && path.endsWith('.next_cursor') && value === '')
      fail('empty continuation cursor');
  }
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) fail('nonfinite number');
    if (schema.minimum !== undefined && value < schema.minimum) fail('number below minimum');
    if (schema.maximum !== undefined && value > schema.maximum) fail('number above maximum');
  }
  if (Array.isArray(value)) {
    if (schema.minItems !== undefined && value.length < schema.minItems) fail('too few items');
    if (schema.maxItems !== undefined && value.length > schema.maxItems) fail('too many items');
    if (!schema.items) fail('missing item schema');
    value.forEach((item, index) => assertSchema(item, schema.items!, `${path}[${index}]`, mode));
  } else if (typeof value === 'object' && value !== null) {
    const object = value as Record<string, unknown>;
    for (const key of schema.required ?? []) {
      if (!Object.hasOwn(object, key)) fail(`missing ${key}`);
    }
    if (schema.minProperties !== undefined && Object.keys(object).length < schema.minProperties)
      fail('too few properties');
    for (const [key, entry] of Object.entries(object)) {
      const property =
        schema.properties && Object.hasOwn(schema.properties, key)
          ? schema.properties[key]
          : undefined;
      if (property) assertSchema(entry, property, `${path}.${key}`, mode);
      else if (schema.additionalProperties === false) fail(`unexpected ${key}`);
    }
    if (
      mode === 'response' &&
      schema.properties?.kind &&
      schema.properties.directory &&
      schema.properties.file
    ) {
      if (
        object.kind === 'directory'
          ? object.directory === null || object.file !== null
          : object.file === null || object.directory !== null
      )
        fail('inconsistent directory entry kind');
    }
  }
}
