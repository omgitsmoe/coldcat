import type { Result } from '../../api/client';
import type { Connection } from '../../state/connection';
import { searchPages } from '../../state/feature-requests';
import { searchURL, type SearchRoute } from '../../state/routes';
import type { TraversalState } from '../../state/traversal';
import { literalText, validatePath } from '../../format/path';

export type SearchPage = Result<'GET /api/v1/search'>;
export interface SearchIssue {
  kind: 'idle' | 'short' | 'invalid';
  message: string;
}
export function searchIssue(input: SearchRoute): SearchIssue | undefined {
  try {
    literalText(input.q);
    if (new TextEncoder().encode(input.q).length > 1024)
      throw new Error('Query must be at most 1024 UTF-8 bytes.');
    searchURL(input);
    if (input.directory !== undefined) {
      validatePath(input.directory);
      if (new TextEncoder().encode(input.directory).length > 1024)
        throw new Error('Directory must be at most 1024 UTF-8 bytes.');
      if (!input.disk_id && !input.snapshot_id)
        throw new Error('Directory membership requires a disk or snapshot ID.');
    }
    const bounds = [input.other_replicas, input.min_other_replicas, input.max_other_replicas];
    if (input.scope === 'history' && bounds.some((v) => v !== undefined))
      throw new Error(
        'Other-copy bounds require current scope; remove bounds explicitly or choose Current.',
      );
    if (input.other_replicas !== undefined && bounds.slice(1).some((v) => v !== undefined))
      throw new Error('Exact other-copy count cannot be combined with minimum/maximum bounds.');
    if (
      input.min_other_replicas !== undefined &&
      input.max_other_replicas !== undefined &&
      BigInt(input.min_other_replicas) > BigInt(input.max_other_replicas)
    )
      throw new Error('Maximum other-copy count must be at least the minimum.');
  } catch (error) {
    return {
      kind: 'invalid',
      message: error instanceof Error ? error.message : 'Invalid search inputs.',
    };
  }
  if (input.q === '')
    return { kind: 'idle', message: 'Search for a remembered filename or disk-relative path.' };
  if (input.match !== 'exact' && Array.from(input.q).length < 3)
    return { kind: 'short', message: 'Type at least 3 characters, or choose Exact.' };
}

export class SearchWorkspace {
  route?: SearchRoute;
  owner?: ReturnType<typeof searchPages>;
  selected?: string;
  scroll = 0;
  debouncing = false;
  issue?: SearchIssue;
  state: TraversalState<SearchPage> = { status: 'idle', pages: [], active: 0, paged: false };
  private timer?: ReturnType<typeof setTimeout>;
  private unbindPage?: () => void;
  private listeners = new Set<() => void>();
  private unbindInvalidation: () => void;

  constructor(readonly connection: Connection) {
    this.unbindInvalidation = connection.onInvalidate(() => {
      clearTimeout(this.timer);
      this.debouncing = false;
      this.selected = undefined;
      this.scroll = 0;
      this.publish();
    });
  }
  subscribe(listener: () => void) {
    this.listeners.add(listener);
    listener();
    return () => {
      this.listeners.delete(listener);
    };
  }
  private publish() {
    for (const listener of this.listeners) listener();
  }
  get items() {
    return this.owner?.traversal.items ?? [];
  }
  retire() {
    clearTimeout(this.timer);
    this.unbindPage?.();
    this.owner?.dispose();
    this.owner = undefined;
    this.selected = undefined;
    this.scroll = 0;
    this.debouncing = false;
    this.issue = undefined;
    this.state = { status: 'idle', pages: [], active: 0, paged: false };
    this.publish();
  }
  update(route: SearchRoute, immediate = false) {
    this.retire();
    this.route = { ...route };
    this.issue = searchIssue(route);
    if (!this.issue) {
      this.debouncing = true;
      if (immediate) this.flush();
      else this.timer = setTimeout(() => this.flush(), 200);
    }
    this.publish();
  }
  flush() {
    clearTimeout(this.timer);
    if (!this.debouncing || !this.route || this.issue) return;
    this.debouncing = false;
    this.owner = searchPages(this.connection, this.route);
    this.unbindPage = this.owner.traversal.subscribe((state) => {
      this.state = state;
      this.publish();
    });
    void this.owner.traversal.restart();
  }
  select(id?: string) {
    this.selected = id;
    this.publish();
  }
  restart() {
    this.select();
    if (this.owner) void this.owner.traversal.restart();
    else if (this.route) this.update(this.route, true);
  }
  dispose() {
    this.retire();
    this.unbindInvalidation();
    this.listeners.clear();
  }
}

let retained: SearchWorkspace | undefined;
export function acquireSearch(connection: Connection, route: SearchRoute) {
  const candidate = retained;
  retained = undefined;
  if (
    candidate?.connection === connection &&
    candidate.route &&
    searchURL(candidate.route) === searchURL(route) &&
    candidate.state.status === 'success'
  )
    return { workspace: candidate, restored: true };
  candidate?.dispose();
  const workspace = new SearchWorkspace(connection);
  workspace.update(route, true);
  return { workspace, restored: false };
}
export function retainSearch(workspace: SearchWorkspace) {
  retained?.dispose();
  retained = undefined;
  if (workspace.state.status === 'success') retained = workspace;
  else workspace.dispose();
}
