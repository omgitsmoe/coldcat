import type { ContentsRoute } from '../../state/routes';

export function contentFilterCount(query: ContentsRoute): number {
  return [
    query.disk_id !== undefined,
    query.directory !== undefined,
    query.replica_metric === 'locations',
    query.other_replicas !== undefined ||
      query.min_other_replicas !== undefined ||
      query.max_other_replicas !== undefined,
  ].filter(Boolean).length;
}

export function contentSummary(query: ContentsRoute): string {
  const parts = [query.scope === 'history' ? 'History membership' : 'Current membership'];
  if (query.disk_id !== undefined) parts.push(`Disk ${query.disk_id}`);
  if (query.directory !== undefined)
    parts.push(query.directory === '' ? 'Directory: root' : `Directory: ${query.directory}`);
  const metric = query.replica_metric === 'locations' ? 'locations' : 'disks';
  parts.push(`Other-copy metric: ${metric}`);
  if (query.other_replicas !== undefined) parts.push(`Exactly ${query.other_replicas}`);
  if (query.min_other_replicas !== undefined) parts.push(`Minimum ${query.min_other_replicas}`);
  if (query.max_other_replicas !== undefined) parts.push(`Maximum ${query.max_other_replicas}`);
  return parts.join(' · ');
}
