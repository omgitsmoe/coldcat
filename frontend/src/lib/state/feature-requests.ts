import type { Query, Result } from '../api/client';
import { contract } from '../api/generated/contract';
import { encodeQuery } from '../api/query';
import type { Connection } from './connection';
import { RequestSlot } from './request';
import { CursorTraversal } from './traversal';
import type { SearchRoute, LocationRoute, DirectoryRoute } from './routes';
import { withoutCursor } from './routes';

function bindTraversal<T, P extends { items: T[]; next_cursor: string | null }>(
  connection: Connection,
  traversal: CursorTraversal<T, P>,
) {
  const unbind = connection.onInvalidate((reason) => {
    if (reason === 'disconnect') traversal.suspend();
    else traversal.retire(reason === 'revision' ? 'stale' : 'idle');
  });
  const unsubscribe = traversal.subscribe((state) => {
    if (state.status === 'failure') connection.report(state.error);
  });
  return {
    traversal,
    dispose: () => {
      unbind();
      unsubscribe();
      traversal.dispose();
    },
  };
}

export function searchPages(connection: Connection, query: SearchRoute) {
  withoutCursor(query);
  const input = {
    field: 'name',
    match: 'substring',
    scope: 'current',
    replica_metric: 'disks',
    limit: 50,
    ...query,
  } as Query<'GET /api/v1/search'>;
  const encoded = encodeQuery(contract['GET /api/v1/search'], input);
  return bindTraversal(
    connection,
    new CursorTraversal<
      Result<'GET /api/v1/search'>['items'][number],
      Result<'GET /api/v1/search'>
    >({ endpoint: 'search', resource: '', query: encoded }, (cursor, signal) =>
      connection.client.search({ ...input, cursor }, signal),
    ),
  );
}

export function locationPages(connection: Connection, id: string, query: LocationRoute = {}) {
  withoutCursor(query);
  const input = { scope: 'current', limit: 50, ...query } as LocationRoute;
  const encoded = encodeQuery(contract['GET /api/v1/contents/{id}/observations'], input);
  return bindTraversal(
    connection,
    new CursorTraversal<
      Result<'GET /api/v1/contents/{id}/observations'>['items'][number],
      Result<'GET /api/v1/contents/{id}/observations'>
    >({ endpoint: 'content-observations', resource: id, query: encoded }, (cursor, signal) =>
      connection.client.contentObservations(id, { ...input, cursor }, signal),
    ),
  );
}

export function diskPages(connection: Connection, limit = 50) {
  const encoded = encodeQuery(contract['GET /api/v1/disks'], { limit });
  return bindTraversal(
    connection,
    new CursorTraversal<Result<'GET /api/v1/disks'>['items'][number], Result<'GET /api/v1/disks'>>(
      { endpoint: 'disks', resource: '', query: encoded },
      (cursor, signal) => connection.client.disks({ limit, cursor }, signal),
    ),
  );
}

export function snapshotPages(connection: Connection, id: string, limit = 50) {
  const encoded = encodeQuery(contract['GET /api/v1/disks/{id}/snapshots'], { limit });
  return bindTraversal(
    connection,
    new CursorTraversal<
      Result<'GET /api/v1/disks/{id}/snapshots'>['items'][number],
      Result<'GET /api/v1/disks/{id}/snapshots'>
    >({ endpoint: 'disk-snapshots', resource: id, query: encoded }, (cursor, signal) =>
      connection.client.diskSnapshots(id, { limit, cursor }, signal),
    ),
  );
}

export function directoryPages(connection: Connection, id: string, query: DirectoryRoute = {}) {
  withoutCursor(query);
  const input: DirectoryRoute = {
    path: '',
    recursive: false,
    replica_metric: 'disks',
    limit: 50,
    ...query,
  };
  const encoded = encodeQuery(contract['GET /api/v1/snapshots/{id}/directory/entries'], input);
  return bindTraversal(
    connection,
    new CursorTraversal<
      Result<'GET /api/v1/snapshots/{id}/directory/entries'>['items'][number],
      Result<'GET /api/v1/snapshots/{id}/directory/entries'>
    >({ endpoint: 'directory-entries', resource: id, query: encoded }, (cursor, signal) =>
      connection.client.directoryEntries(id, { ...input, cursor }, signal),
    ),
  );
}

function detail<T>(connection: Connection, load: (signal: AbortSignal) => Promise<T>) {
  const request = new RequestSlot<T>();
  const unbind = connection.onInvalidate(() => request.cancel());
  const unsubscribe = request.subscribe((state) => {
    if (state.status === 'failure') connection.report(state.error);
  });
  return {
    request,
    reload: () => request.run(load),
    dispose: () => {
      unbind();
      unsubscribe();
      request.dispose();
    },
  };
}

export function contentDetail(
  connection: Connection,
  id: string,
  query: Query<'GET /api/v1/contents/{id}'> = {},
) {
  const input = { ...query };
  return detail(connection, (signal) => connection.client.content(id, input, signal));
}
export const observationDetail = (connection: Connection, id: string) =>
  detail(connection, (signal) => connection.client.observation(id, signal));
export const diskDetail = (connection: Connection, id: string) =>
  detail(connection, (signal) => connection.client.disk(id, signal));
export const snapshotDetail = (connection: Connection, id: string) =>
  detail(connection, (signal) => connection.client.snapshot(id, signal));
export const directoryDetail = (connection: Connection, id: string, path = '') =>
  detail(connection, (signal) => connection.client.directory(id, { path }, signal));
