import type { operations, components } from './generated/wire';
import { contract, schemas } from './generated/contract';
import { assertSchema } from './decode';
import { encodeQuery } from './query';
import { ApiError } from './errors';

export type Query<K extends keyof operations> = operations[K]['query'];
export type Result<K extends keyof operations> = operations[K]['response'];
export type Fetcher = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export function createClient(fetcher: Fetcher = (input, init) => fetch(input, init)) {
  async function request<K extends keyof operations>(
    key: K,
    query: operations[K]['query'],
    signal?: AbortSignal,
    id?: string,
    body?: operations[K]['body'],
  ): Promise<Result<K>> {
    const operation = contract[key];
    const write = operation.method !== 'GET';
    const cancelled = (cause?: unknown) =>
      new ApiError('cancelled', 'Request cancelled', { cause, uncertainWrite: write });
    if (signal?.aborted) throw new ApiError('cancelled', 'Request cancelled before dispatch');
    let url: string;
    let encodedBody: string | undefined;
    try {
      let path = operation.path;
      if (path.includes('{id}')) {
        assertSchema(id, schemas.ID, 'id', 'request');
        path = path.replace('{id}', encodeURIComponent(id!));
      }
      url = path + encodeQuery(operation, query);
      if (operation.body) {
        assertSchema(body, operation.body, 'body', 'request');
        encodedBody = JSON.stringify(body);
        if (new TextEncoder().encode(encodedBody).length > 65536)
          throw new Error('Disk write exceeds 65536 bytes');
      }
    } catch (cause) {
      throw new ApiError('request', 'Invalid API request', { cause });
    }

    let response: Response;
    let text: string;
    try {
      response = await fetcher(url, {
        method: operation.method,
        headers: {
          Accept: 'application/json',
          ...(write ? { 'Content-Type': 'application/json' } : {}),
        },
        signal,
        body: encodedBody,
      });
      text = await response.text();
    } catch (cause) {
      if (
        signal?.aborted ||
        (typeof cause === 'object' &&
          cause !== null &&
          'name' in cause &&
          cause.name === 'AbortError')
      )
        throw cancelled(cause);
      throw new ApiError('transport', 'Backend connection failed', {
        cause,
        uncertainWrite: write,
      });
    }
    if (signal?.aborted) throw cancelled();

    const contentType = response.headers.get('Content-Type')?.split(';')[0]?.trim().toLowerCase();
    if (
      (response.status === 502 || response.status === 504) &&
      contentType !== 'application/json'
    ) {
      throw new ApiError('transport', 'Backend proxy failed', {
        status: response.status,
        uncertainWrite: write,
      });
    }

    let payload: unknown;
    try {
      if (contentType !== 'application/json') {
        throw new Error('Expected JSON response content type');
      }
      payload = JSON.parse(text);
      if (!response.ok) assertSchema(payload, schemas.Error);
      else {
        if (response.status !== operation.status) throw new Error('Unexpected success status');
        assertSchema(payload, operation.response);
      }
    } catch (cause) {
      throw new ApiError('contract', 'Unexpected backend response', {
        status: response.status,
        cause,
        uncertainWrite: write,
      });
    }
    if (!response.ok) {
      const { error } = payload as components['schemas']['Error'];
      throw new ApiError('http', error.message, { status: response.status, code: error.code });
    }
    return payload as Result<K>;
  }

  return {
    health: (signal?: AbortSignal) => request('GET /healthz', {}, signal),
    catalog: (signal?: AbortSignal) => request('GET /api/v1/catalog', {}, signal),
    search: (query: Query<'GET /api/v1/search'>, signal?: AbortSignal) =>
      request('GET /api/v1/search', query, signal),
    contents: (query: Query<'GET /api/v1/contents'> = {}, signal?: AbortSignal) =>
      request('GET /api/v1/contents', query, signal),
    lookupContent: (query: Query<'GET /api/v1/contents/lookup'>, signal?: AbortSignal) =>
      request('GET /api/v1/contents/lookup', query, signal),
    content: (id: string, query: Query<'GET /api/v1/contents/{id}'> = {}, signal?: AbortSignal) =>
      request('GET /api/v1/contents/{id}', query, signal, id),
    contentObservations: (
      id: string,
      query: Query<'GET /api/v1/contents/{id}/observations'> = {},
      signal?: AbortSignal,
    ) => request('GET /api/v1/contents/{id}/observations', query, signal, id),
    observation: (id: string, signal?: AbortSignal) =>
      request('GET /api/v1/observations/{id}', {}, signal, id),
    disks: (query: Query<'GET /api/v1/disks'> = {}, signal?: AbortSignal) =>
      request('GET /api/v1/disks', query, signal),
    createDisk: (body: components['schemas']['CreateDisk'], signal?: AbortSignal) =>
      request('POST /api/v1/disks', {}, signal, undefined, body),
    disk: (id: string, signal?: AbortSignal) => request('GET /api/v1/disks/{id}', {}, signal, id),
    updateDisk: (id: string, body: components['schemas']['UpdateDisk'], signal?: AbortSignal) =>
      request('PATCH /api/v1/disks/{id}', {}, signal, id, body),
    diskSnapshots: (
      id: string,
      query: Query<'GET /api/v1/disks/{id}/snapshots'> = {},
      signal?: AbortSignal,
    ) => request('GET /api/v1/disks/{id}/snapshots', query, signal, id),
    snapshot: (id: string, signal?: AbortSignal) =>
      request('GET /api/v1/snapshots/{id}', {}, signal, id),
    directories: (
      id: string,
      query: Query<'GET /api/v1/snapshots/{id}/directories'> = {},
      signal?: AbortSignal,
    ) => request('GET /api/v1/snapshots/{id}/directories', query, signal, id),
    directory: (
      id: string,
      query: Query<'GET /api/v1/snapshots/{id}/directory'> = {},
      signal?: AbortSignal,
    ) => request('GET /api/v1/snapshots/{id}/directory', query, signal, id),
    directoryEntries: (
      id: string,
      query: Query<'GET /api/v1/snapshots/{id}/directory/entries'> = {},
      signal?: AbortSignal,
    ) => request('GET /api/v1/snapshots/{id}/directory/entries', query, signal, id),
    directoryReplicas: (
      id: string,
      query: Query<'GET /api/v1/snapshots/{id}/directory/replicas'> = {},
      signal?: AbortSignal,
    ) => request('GET /api/v1/snapshots/{id}/directory/replicas', query, signal, id),
    directoryCoverage: (
      id: string,
      query: Query<'GET /api/v1/snapshots/{id}/directory/coverage'> = {},
      signal?: AbortSignal,
    ) => request('GET /api/v1/snapshots/{id}/directory/coverage', query, signal, id),
  };
}

export type Client = ReturnType<typeof createClient>;
