import { ApiError } from '../api/errors';

export interface TraversalIdentity {
  endpoint: string;
  resource: string;
  query: string;
}
export const traversalKey = (identity: TraversalIdentity) =>
  JSON.stringify([identity.endpoint, identity.resource, identity.query]);

export interface CursorPage<T> {
  items: T[];
  next_cursor: string | null;
  revision?: string;
}
export interface RetainedPage<P> {
  number: number;
  value: P;
}
export interface TraversalState<P> {
  status: 'idle' | 'loading' | 'success' | 'loading-more' | 'failure' | 'stale';
  pages: RetainedPage<P>[];
  active: number;
  paged: boolean;
  error?: unknown;
}

export class CursorTraversal<T, P extends CursorPage<T> = CursorPage<T>> {
  readonly key: string;
  state: TraversalState<P> = { status: 'idle', pages: [], active: 0, paged: false };
  private generation = 0;
  private controller?: AbortController;
  private disposed = false;
  private suspended = false;
  private listeners = new Set<(state: TraversalState<P>) => void>();

  constructor(
    identity: TraversalIdentity,
    private load: (cursor: string | undefined, signal: AbortSignal) => Promise<P>,
    readonly retainedPages = 10,
  ) {
    if (!Number.isInteger(retainedPages) || retainedPages < 1)
      throw new Error('Invalid page bound');
    this.key = traversalKey(identity);
  }

  subscribe(listener: (state: TraversalState<P>) => void) {
    this.listeners.add(listener);
    listener(this.state);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private publish(state: TraversalState<P>) {
    this.state = state;
    for (const listener of this.listeners) listener(state);
  }

  get items(): T[] {
    const pages = this.state.paged
      ? this.state.pages.filter((page) => page.number === this.state.active)
      : this.state.pages;
    return pages.flatMap((page) => page.value.items);
  }

  get busy() {
    return this.state.status === 'loading' || this.state.status === 'loading-more';
  }
  get canMore() {
    return (
      !this.busy &&
      !this.suspended &&
      !this.state.paged &&
      this.state.pages.length < this.retainedPages &&
      this.state.pages.at(-1)?.value.next_cursor != null
    );
  }
  get canNext() {
    return (
      !this.busy &&
      this.state.pages.length > 0 &&
      (this.state.active < this.state.pages.at(-1)!.number ||
        (!this.suspended && this.state.pages.at(-1)!.value.next_cursor !== null))
    );
  }
  get canPrevious() {
    const first = this.state.pages[0];
    return (
      !this.busy && this.state.paged && first !== undefined && this.state.active > first.number
    );
  }

  retire(status: 'idle' | 'stale' = 'idle') {
    this.suspended = false;
    this.generation++;
    this.controller?.abort();
    this.controller = undefined;
    this.publish({ status, pages: [], active: 0, paged: false });
  }

  suspend() {
    this.suspended = true;
    this.generation++;
    this.controller?.abort();
    this.controller = undefined;
    this.publish({
      ...this.state,
      status: 'failure',
      error: new ApiError(
        'transport',
        'Backend unavailable; retained pages are not freshly verified',
      ),
    });
  }

  async restart() {
    if (this.disposed) throw new Error('Traversal disposed');
    this.retire();
    await this.fetchPage(undefined, false);
  }

  async more() {
    if (!this.canMore) return;
    await this.fetchPage(this.state.pages.at(-1)!.value.next_cursor!, false);
  }

  async next() {
    if (!this.canNext) return;
    if (this.state.active < this.state.pages.at(-1)!.number) {
      this.publish({
        ...this.state,
        status: this.suspended ? 'failure' : 'success',
        error: this.suspended ? this.state.error : undefined,
        active: this.state.active + 1,
        paged: true,
      });
      return;
    }
    await this.fetchPage(this.state.pages.at(-1)!.value.next_cursor!, true);
  }

  previous() {
    if (this.canPrevious)
      this.publish({
        ...this.state,
        status: this.suspended ? 'failure' : 'success',
        error: this.suspended ? this.state.error : undefined,
        active: this.state.active - 1,
      });
  }

  private async fetchPage(cursor: string | undefined, paged: boolean) {
    const generation = ++this.generation;
    const controller = new AbortController();
    this.controller = controller;
    this.publish({
      ...this.state,
      error: undefined,
      status: cursor === undefined ? 'loading' : 'loading-more',
    });
    try {
      const value = await this.load(cursor, controller.signal);
      if (generation !== this.generation) return;
      const revision = this.state.pages[0]?.value.revision;
      if (revision !== undefined && value.revision !== revision) {
        this.stale();
        return;
      }
      const number = (this.state.pages.at(-1)?.number ?? 0) + 1;
      const pages = [...this.state.pages, { number, value }].slice(-this.retainedPages);
      this.publish({ status: 'success', pages, active: number, paged });
    } catch (error) {
      if (generation !== this.generation) return;
      if (error instanceof ApiError && error.status === 409 && error.code === 'stale_cursor') {
        this.stale();
      } else if (error instanceof ApiError && error.kind === 'cancelled') {
        this.publish({ ...this.state, status: this.state.pages.length ? 'success' : 'idle' });
      } else this.publish({ ...this.state, status: 'failure', error });
    } finally {
      if (generation === this.generation) this.controller = undefined;
    }
  }

  private stale() {
    this.publish({ status: 'stale', pages: [], active: 0, paged: false });
  }

  dispose() {
    this.retire();
    this.disposed = true;
    this.listeners.clear();
  }
}
