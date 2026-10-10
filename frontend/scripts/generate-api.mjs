import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { resolve as absolutePath } from 'node:path';
import { createHash } from 'node:crypto';
import { format } from 'prettier';

const args = process.argv.slice(2);
let source = new URL('../../doc/openapi.json', import.meta.url);
let output = new URL('../src/lib/api/generated/', import.meta.url);
let check = false;
for (let index = 0; index < args.length; index++) {
  const argument = args[index];
  if (argument === '--check') check = true;
  else if (argument === '--source' || argument === '--output') {
    const path = args[++index];
    if (!path || path.startsWith('--')) throw new Error(`Missing value for ${argument}`);
    const url = pathToFileURL(absolutePath(path));
    if (argument === '--source') source = url;
    else output = new URL(`${url.href}/`);
  } else throw new Error(`Unknown argument: ${argument}`);
}
const bytes = await readFile(source, 'utf8');
const document = JSON.parse(bytes);
const resolve = (value) => {
  if (!value.$ref) return value;
  if (!value.$ref.startsWith('#/')) throw new Error(`Nonlocal reference: ${value.$ref}`);
  const result = value.$ref
    .split('/')
    .slice(1)
    .reduce((node, key) => node[key], document);
  if (!result) throw new Error(`Missing reference: ${value.$ref}`);
  return result;
};
const keywords = new Set([
  '$ref',
  'type',
  'required',
  'additionalProperties',
  'properties',
  'items',
  'const',
  'enum',
  'pattern',
  'format',
  'minimum',
  'maximum',
  'minLength',
  'maxLength',
  'minItems',
  'maxItems',
  'minProperties',
]);
const annotations = new Set(['description', 'default', 'example', 'examples', 'title']);

function schema(value) {
  if (value.$ref && Object.keys(value).some((key) => key !== '$ref' && !annotations.has(key))) {
    throw new Error('Unsupported schema reference siblings');
  }
  const result = {};
  for (const [key, entry] of Object.entries(value)) {
    if (annotations.has(key)) continue;
    if (!keywords.has(key)) throw new Error(`Unsupported schema keyword: ${key}`);
    if (key === '$ref') {
      resolve(value);
      if (!entry.startsWith('#/components/schemas/')) throw new Error(`Not a schema: ${entry}`);
      result.ref = entry.split('/').at(-1);
    } else if (key === 'properties') {
      result.properties = Object.fromEntries(Object.entries(entry).map(([k, v]) => [k, schema(v)]));
    } else if (key === 'items') {
      result.items = schema(entry);
    } else {
      result[key] = entry;
    }
  }
  if (
    result.additionalProperties !== undefined &&
    typeof result.additionalProperties !== 'boolean'
  ) {
    throw new Error('Unsupported additionalProperties schema');
  }
  if (result.format && result.format !== 'date-time')
    throw new Error(`Unsupported format: ${result.format}`);
  if (result.pattern) new RegExp(result.pattern);
  return result;
}

function type(value) {
  if (value.ref) return `components['schemas'][${JSON.stringify(value.ref)}]`;
  if ('const' in value) return JSON.stringify(value.const);
  if (value.enum) return value.enum.map((v) => JSON.stringify(v)).join(' | ');
  const types = Array.isArray(value.type) ? value.type : [value.type];
  return types
    .map((kind) => {
      switch (kind) {
        case 'null':
          return 'null';
        case 'string':
          return 'string';
        case 'boolean':
          return 'boolean';
        case 'integer':
        case 'number':
          return 'number';
        case 'array':
          return `Array<${type(value.items)}>`;
        case 'object':
          return `{${Object.entries(value.properties ?? {})
            .map(
              ([key, prop]) =>
                `${JSON.stringify(key)}${value.required?.includes(key) ? '' : '?'}: ${type(prop)};`,
            )
            .join('\n')}}`;
        default:
          throw new Error(`Unsupported schema type: ${kind}`);
      }
    })
    .join(' | ');
}

const schemas = Object.fromEntries(
  Object.entries(document.components.schemas).map(([k, v]) => [k, schema(v)]),
);
const operations = {};
let operationTypes = '';
for (const [path, methods] of Object.entries(document.paths)) {
  for (const [method, operation] of Object.entries(methods)) {
    if (!['get', 'post', 'patch'].includes(method))
      throw new Error(`Unsupported operation: ${method} ${path}`);
    const key = `${method.toUpperCase()} ${path}`;
    const parameters = (operation.parameters ?? []).map(resolve);
    const query = {};
    for (const parameter of parameters) {
      if (!['query', 'path'].includes(parameter.in))
        throw new Error('Unsupported parameter location');
      if (parameter.in === 'query')
        query[parameter.name] = {
          schema: schema(parameter.schema),
          required: parameter.required === true,
        };
    }
    const successes = Object.entries(operation.responses).filter(([status]) =>
      /^2\d\d$/.test(status),
    );
    if (successes.length !== 1) throw new Error(`Expected one success for ${key}`);
    const [status, rawResponse] = successes[0];
    const response = schema(resolve(rawResponse).content['application/json'].schema);
    const body = operation.requestBody
      ? schema(resolve(operation.requestBody).content['application/json'].schema)
      : undefined;
    operations[key] = {
      method: method.toUpperCase(),
      path,
      status: Number(status),
      query,
      response,
      ...(body ? { body } : {}),
    };
    const queryType =
      Object.keys(query).length === 0
        ? 'Record<string, never>'
        : `{${Object.entries(query)
            .map(
              ([name, p]) => `${JSON.stringify(name)}${p.required ? '' : '?'}: ${type(p.schema)};`,
            )
            .join('\n')}}`;
    operationTypes += `${JSON.stringify(key)}: { query: ${queryType}; body: ${body ? type(body) : 'never'}; response: ${type(response)}; };\n`;
  }
}

const header = `// Generated from doc/openapi.json; do not edit.\n// SHA-256: ${createHash('sha256').update(bytes).digest('hex')}\n`;
const files = {
  'wire.ts': `${header}export interface components { schemas: {${Object.entries(schemas)
    .map(([name, s]) => `${JSON.stringify(name)}: ${type(s)};`)
    .join('\n')}} }\nexport interface operations { ${operationTypes} }\n`,
  'contract.ts': `${header}import type { Schema, Operation } from '../schema';\nimport type { operations } from './wire';\nexport const schemas: Record<${Object.keys(
    schemas,
  )
    .map((k) => JSON.stringify(k))
    .join(
      ' | ',
    )}, Schema> = ${JSON.stringify(schemas)};\nexport const contract: Record<keyof operations, Operation> = ${JSON.stringify(operations)};\n`,
};

if (!check) await mkdir(output, { recursive: true });
for (const [name, content] of Object.entries(files)) {
  const formatted = await format(content, {
    parser: 'typescript',
    singleQuote: true,
    trailingComma: 'all',
    printWidth: 100,
  });
  const destination = new URL(name, output);
  if (check) {
    const existing = await readFile(destination, 'utf8');
    if (existing !== formatted)
      throw new Error(
        `Generated API drift: ${fileURLToPath(destination)}; run npm run api:generate`,
      );
  } else {
    await writeFile(destination, formatted);
  }
}
