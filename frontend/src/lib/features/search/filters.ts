import type { SearchRoute } from '../../state/routes';

export type BoundsMode = 'any' | 'exact' | 'range' | 'conflict';
export interface BoundsDraft {
  mode: BoundsMode;
  exact: string;
  minimum: string;
  maximum: string;
}

export function boundsMode(exact: string, minimum: string, maximum: string): BoundsMode {
  if (exact !== '') return minimum !== '' || maximum !== '' ? 'conflict' : 'exact';
  return minimum !== '' || maximum !== '' ? 'range' : 'any';
}

export function boundsQuery(draft: BoundsDraft) {
  const bounds: Pick<SearchRoute, 'other_replicas' | 'min_other_replicas' | 'max_other_replicas'> =
    {};
  if (draft.mode === 'conflict')
    throw new Error('Exact and range bounds conflict. Choose Any, Exactly or Range explicitly.');
  if (draft.mode === 'exact') {
    if (draft.exact === '') throw new Error('Enter an exact other-copy count, or choose Any.');
    bounds.other_replicas = draft.exact;
  }
  if (draft.mode === 'range') {
    if (draft.minimum === '' && draft.maximum === '')
      throw new Error('Enter at least one range bound, or choose Any.');
    if (draft.minimum !== '') bounds.min_other_replicas = draft.minimum;
    if (draft.maximum !== '') bounds.max_other_replicas = draft.maximum;
  }
  return bounds;
}

export function advancedCount(route: SearchRoute): number {
  return [
    route.disk_id !== undefined,
    route.snapshot_id !== undefined,
    route.directory !== undefined,
    route.replica_metric === 'locations',
    route.other_replicas !== undefined ||
      route.min_other_replicas !== undefined ||
      route.max_other_replicas !== undefined,
  ].filter(Boolean).length;
}

export function searchSummary(route: SearchRoute): string {
  const parts = [
    `${route.field === 'path' ? 'Path' : 'Name'} · ${route.match === 'exact' ? 'Exact' : 'Contains'}`,
    route.scope === 'history' ? 'History' : 'Current',
    `Query: ${route.q}`,
  ];
  if (route.disk_id !== undefined) parts.push(`Disk ${route.disk_id}`);
  if (route.snapshot_id !== undefined) parts.push(`Snapshot ${route.snapshot_id}`);
  if (route.directory !== undefined)
    parts.push(route.directory === '' ? 'Directory: root' : `Directory: ${route.directory}`);
  const metric = route.replica_metric === 'locations' ? 'locations' : 'disks';
  if (route.other_replicas !== undefined)
    parts.push(`Other ${metric}: exactly ${route.other_replicas}`);
  if (route.min_other_replicas !== undefined)
    parts.push(`Other ${metric}: ≥ ${route.min_other_replicas}`);
  if (route.max_other_replicas !== undefined)
    parts.push(`Other ${metric}: ≤ ${route.max_other_replicas}`);
  if (
    route.replica_metric === 'locations' &&
    route.other_replicas === undefined &&
    route.min_other_replicas === undefined &&
    route.max_other_replicas === undefined
  )
    parts.push('Other-copy metric: locations');
  return parts.join(' · ');
}
