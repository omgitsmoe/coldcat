import { getContext, setContext } from 'svelte';
import type { Connection } from './connection';

const key = Symbol('coldcat-connection');
export function provideConnection(connection: Connection) {
  setContext(key, connection);
}
export function useConnection(): Connection {
  const connection = getContext<Connection | undefined>(key);
  if (!connection) throw new Error('Coldcat shell connection context is required');
  return connection;
}
