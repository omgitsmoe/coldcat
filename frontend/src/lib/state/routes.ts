import type { Query } from '../api/client';
import type { operations } from '../api/generated/wire';
import { contract, schemas } from '../api/generated/contract';
import { assertSchema } from '../api/decode';
import { encodeQuery } from '../api/query';
import { literalText, validatePath } from '../format/path';

export type SearchRoute = Omit<Query<'GET /api/v1/search'>, 'cursor'>;
export type DirectoryRoute = Omit<Query<'GET /api/v1/snapshots/{id}/directory/entries'>, 'cursor'>;
export type LocationRoute = Omit<Query<'GET /api/v1/contents/{id}/observations'>, 'cursor'>;
export interface DetailContext {
  returnTo?: string;
  observation?: string;
}

export function withoutCursor(query: object): void {
  if (Object.hasOwn(query, 'cursor'))
    throw new Error('Cursors are session state, not route inputs');
}

function id(value: string) {
  assertSchema(value, schemas.ID, 'id', 'request');
  return encodeURIComponent(value);
}

function parseQuery<K extends keyof operations>(key: K, params: URLSearchParams): Query<K> {
  const operation = contract[key];
  const result: Record<string, unknown> = {};
  for (const name of params.keys()) {
    if (name === 'cursor' || !Object.hasOwn(operation.query, name)) {
      throw new Error(`Unexpected route parameter: ${name}`);
    }
    const schema = operation.query[name]!.schema;
    const values = params.getAll(name);
    if (schema.type !== 'array' && values.length !== 1)
      throw new Error(`Repeated parameter: ${name}`);
    const value = values[0]!;
    if (schema.type === 'integer') {
      if (!/^[0-9]+$/.test(value)) throw new Error(`Invalid integer: ${name}`);
      result[name] = Number(value);
    } else if (schema.type === 'boolean') {
      if (value !== 'true' && value !== 'false') throw new Error(`Invalid boolean: ${name}`);
      result[name] = value === 'true';
    } else result[name] = schema.type === 'array' ? values : value;
  }
  const validation =
    key === 'GET /api/v1/search' && (!params.has('q') || result.q === '')
      ? { ...result, q: 'route-validation' }
      : result;
  encodeQuery(operation, validation);
  return result as Query<K>;
}

export function parseSearch(params: URLSearchParams): SearchRoute {
  const query = parseQuery('GET /api/v1/search', params);
  literalText(query.q ?? '');
  if (query.directory !== undefined) validatePath(query.directory);
  return {
    field: 'name',
    match: 'substring',
    scope: 'current',
    replica_metric: 'disks',
    limit: 50,
    ...query,
    q: query.q ?? '',
  };
}

export function searchURL(query: SearchRoute): string {
  withoutCursor(query);
  literalText(query.q);
  if (query.directory !== undefined) validatePath(query.directory);
  const encoded = encodeQuery(
    contract['GET /api/v1/search'],
    query.q === '' ? { ...query, q: 'route-validation' } : query,
  );
  const params = new URLSearchParams(encoded);
  params.set('q', query.q);
  return `/?${params}`;
}

export function parseDirectory(params: URLSearchParams): DirectoryRoute {
  const query = parseQuery('GET /api/v1/snapshots/{id}/directory/entries', params);
  validatePath(query.path ?? '');
  return { path: '', recursive: false, replica_metric: 'disks', limit: 50, ...query };
}

export function parseLocations(params: URLSearchParams): LocationRoute {
  return {
    scope: 'current',
    limit: 50,
    ...parseQuery('GET /api/v1/contents/{id}/observations', params),
  };
}

export function parsePageLimit(params: URLSearchParams): { limit: number } {
  return { limit: 50, ...parseQuery('GET /api/v1/disks', params) };
}

export function parseInventoryRoute(params: URLSearchParams) {
  const context = new URLSearchParams();
  const paging = new URLSearchParams();
  for (const [name, value] of params) {
    (['return_to', 'observation'].includes(name) ? context : paging).append(name, value);
  }
  return { context: parseDetailContext(context), ...parsePageLimit(paging) };
}

export function parseContentRoute(params: URLSearchParams) {
  const context = new URLSearchParams();
  const locations = new URLSearchParams();
  for (const [name, value] of params) {
    (['return_to', 'observation'].includes(name) ? context : locations).append(name, value);
  }
  return { context: parseDetailContext(context), locations: parseLocations(locations) };
}

export function contentURL(value: string, locations: LocationRoute, context: DetailContext = {}) {
  withoutCursor(locations);
  const url = detailURL(`/contents/${id(value)}`, context);
  const params = new URLSearchParams(url.split('?')[1]);
  const query = new URLSearchParams(
    encodeQuery(contract['GET /api/v1/contents/{id}/observations'], locations),
  );
  for (const [name, entry] of query) params.append(name, entry);
  return `/contents/${id(value)}` + (params.size ? `?${params}` : '');
}

export function safeSearchReturn(value: string): string | undefined {
  if (
    !value.startsWith('/') ||
    value.startsWith('//') ||
    value.includes('\\') ||
    value.includes('#')
  )
    return undefined;
  try {
    const url = new URL(value, 'http://coldcat.local');
    if (url.origin !== 'http://coldcat.local' || url.pathname !== '/') return undefined;
    parseSearch(url.searchParams);
    return url.pathname + url.search;
  } catch {
    return undefined;
  }
}

export function parseDetailContext(params: URLSearchParams): DetailContext {
  for (const key of params.keys()) {
    if (!['return_to', 'observation'].includes(key) || params.getAll(key).length !== 1) {
      throw new Error(`Unexpected detail parameter: ${key}`);
    }
  }
  const context: DetailContext = {};
  if (params.has('observation')) {
    context.observation = params.get('observation')!;
    id(context.observation);
  }
  if (params.has('return_to')) {
    context.returnTo = safeSearchReturn(params.get('return_to')!);
    if (context.returnTo === undefined) throw new Error('Invalid search return destination');
  }
  return context;
}

function detailURL(path: string, context: DetailContext = {}) {
  const params = new URLSearchParams();
  if (context.returnTo !== undefined) {
    const value = safeSearchReturn(context.returnTo);
    if (value === undefined) throw new Error('Invalid search return destination');
    params.set('return_to', value);
  }
  if (context.observation !== undefined) {
    id(context.observation);
    params.set('observation', context.observation);
  }
  return path + (params.size ? `?${params}` : '');
}

export const routes = {
  search: '/',
  contents: '/contents',
  disks: '/disks',
  content: (value: string, context?: DetailContext) => detailURL(`/contents/${id(value)}`, context),
  observation: (value: string, context?: DetailContext) =>
    detailURL(`/observations/${id(value)}`, context),
  disk: (value: string, context?: DetailContext) => detailURL(`/disks/${id(value)}`, context),
  snapshot: (value: string, context?: DetailContext) =>
    detailURL(`/snapshots/${id(value)}`, context),
} as const;

export function directoryBrowseURL(snapshot: string, query: DirectoryRoute): string {
  withoutCursor(query);
  validatePath(query.path ?? '');
  return (
    `/snapshots/${id(snapshot)}/directory` +
    encodeQuery(contract['GET /api/v1/snapshots/{id}/directory/entries'], query)
  );
}

export function comparisonURL(
  snapshot: string,
  tab: 'replicas' | 'coverage',
  query: Omit<Query<'GET /api/v1/snapshots/{id}/directory/replicas'>, 'cursor'>,
): string {
  withoutCursor(query);
  validatePath(query.path ?? '');
  const params = new URLSearchParams(
    encodeQuery(contract['GET /api/v1/snapshots/{id}/directory/replicas'], query),
  );
  params.set('tab', tab);
  return `/snapshots/${id(snapshot)}/directory?${params}`;
}
