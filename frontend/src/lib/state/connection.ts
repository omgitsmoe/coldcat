import type { Client, Result } from '../api/client';
import { ApiError } from '../api/errors';
import { RequestSlot } from './request';

export type InvalidationReason = 'disconnect' | 'reconnect' | 'revision' | 'metadata';
export type ConnectionState =
  | { status: 'checking' }
  | { status: 'ready'; catalog: Result<'GET /api/v1/catalog'> }
  | { status: 'unavailable'; error: unknown };

export class Connection {
  state: ConnectionState = { status: 'checking' };
  epoch = 0;
  private slot = new RequestSlot<Result<'GET /api/v1/catalog'>>();
  private listeners = new Set<(state: ConnectionState) => void>();
  private invalidators = new Set<(reason: InvalidationReason) => void>();
  private timer?: ReturnType<typeof setTimeout>;
  private attempt = 0;
  private disposed = false;
  private disconnected = false;
  private revision?: string;
  private generation = 0;

  constructor(readonly client: Client) {}

  subscribe(listener: (state: ConnectionState) => void) {
    this.listeners.add(listener);
    listener(this.state);
    return () => {
      this.listeners.delete(listener);
    };
  }
  onInvalidate(listener: (reason: InvalidationReason) => void) {
    this.invalidators.add(listener);
    return () => {
      this.invalidators.delete(listener);
    };
  }
  private publish(state: ConnectionState) {
    this.state = state;
    for (const listener of this.listeners) listener(state);
  }
  private invalidate(reason: InvalidationReason) {
    this.epoch++;
    for (const listener of this.invalidators) listener(reason);
  }

  async check() {
    if (this.disposed) return;
    const generation = ++this.generation;
    clearTimeout(this.timer);
    this.publish({ status: 'checking' });
    await this.slot.run(async (signal) => {
      await this.client.health(signal);
      return this.client.catalog(signal);
    });
    if (
      this.disposed ||
      generation !== this.generation ||
      this.slot.state.status === 'idle' ||
      this.slot.state.status === 'loading'
    )
      return;
    if (this.slot.state.status === 'success') {
      const catalog = this.slot.state.value;
      if (this.disconnected) this.invalidate('reconnect');
      else if (this.revision !== undefined && this.revision !== catalog.revision) {
        this.invalidate('revision');
      }
      this.disconnected = false;
      this.revision = catalog.revision;
      this.attempt = 0;
      this.publish({ status: 'ready', catalog });
    } else this.unavailable(this.slot.state.error);
  }

  async retry() {
    this.attempt = 0;
    await this.check();
  }

  report(error: unknown) {
    if (this.disposed || this.disconnected) return;
    if (error instanceof ApiError && (error.kind === 'transport' || error.status === 503)) {
      this.generation++;
      this.slot.cancel();
      this.unavailable(error);
    }
  }

  private unavailable(error: unknown) {
    if (!this.disconnected) {
      this.disconnected = true;
      this.invalidate('disconnect');
    }
    this.publish({ status: 'unavailable', error });
    if (this.timer !== undefined) clearTimeout(this.timer);
    const delay = [1000, 3000, 10000][this.attempt];
    if (delay !== undefined) {
      this.attempt++;
      this.timer = setTimeout(() => {
        this.timer = undefined;
        void this.check();
      }, delay);
    }
  }

  metadataChanged() {
    this.invalidate('metadata');
  }

  dispose() {
    this.disposed = true;
    this.generation++;
    clearTimeout(this.timer);
    this.slot.dispose();
    this.listeners.clear();
    this.invalidators.clear();
  }
}
