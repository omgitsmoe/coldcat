import type { DirectoryRoute } from '../../state/routes';

export function directorySummary(query: DirectoryRoute): string {
  const parts = [query.recursive ? 'Recursive files' : 'Immediate entries'];
  parts.push(`current other ${query.replica_metric ?? 'disks'}`);
  if (query.other_replicas !== undefined) parts.push(`exactly ${query.other_replicas}`);
  if (query.min_other_replicas !== undefined) parts.push(`minimum ${query.min_other_replicas}`);
  if (query.max_other_replicas !== undefined) parts.push(`maximum ${query.max_other_replicas}`);
  if (
    query.other_replicas === undefined &&
    query.min_other_replicas === undefined &&
    query.max_other_replicas === undefined
  )
    parts.push('any replica count');
  parts.push(`page limit ${query.limit ?? 50}`);
  return parts.join(' · ');
}
