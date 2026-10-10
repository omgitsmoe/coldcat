import { ApiError } from '../api/errors';

export type RequestState<T> =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'success'; value: T }
  | { status: 'failure'; error: unknown };

export class RequestSlot<T> {
  state: RequestState<T> = { status: 'idle' };
  private generation = 0;
  private controller?: AbortController;
  private disposed = false;
  private listeners = new Set<(state: RequestState<T>) => void>();

  subscribe(listener: (state: RequestState<T>) => void) {
    this.listeners.add(listener);
    listener(this.state);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private publish(state: RequestState<T>) {
    this.state = state;
    for (const listener of this.listeners) listener(state);
  }

  cancel() {
    this.generation++;
    this.controller?.abort();
    this.controller = undefined;
    this.publish({ status: 'idle' });
  }

  async run(load: (signal: AbortSignal) => Promise<T>): Promise<void> {
    if (this.disposed) throw new Error('Request slot disposed');
    this.cancel();
    const generation = this.generation;
    const controller = new AbortController();
    this.controller = controller;
    this.publish({ status: 'loading' });
    try {
      const value = await load(controller.signal);
      if (generation === this.generation) this.publish({ status: 'success', value });
    } catch (error) {
      if (generation !== this.generation) return;
      if (error instanceof ApiError && error.kind === 'cancelled') this.publish({ status: 'idle' });
      else this.publish({ status: 'failure', error });
    } finally {
      if (generation === this.generation) this.controller = undefined;
    }
  }

  dispose() {
    this.cancel();
    this.disposed = true;
    this.listeners.clear();
  }
}
