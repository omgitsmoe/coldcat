import { describe, expect, it } from 'vitest';
import { contentFilterCount, contentSummary } from '../../src/lib/features/contents/filters';

describe('distinct content filter presentation', () => {
  it('distinguishes root and zero from absent restrictions', () => {
    const query = { scope: 'current' as const, disk_id: '9', directory: '', other_replicas: '0' };
    expect(contentFilterCount(query)).toBe(3);
    expect(contentSummary(query)).toBe(
      'Current membership · Disk 9 · Directory: root · Other-copy metric: disks · Exactly 0',
    );
  });
  it('counts a range once and preserves literal history membership', () => {
    const query = {
      scope: 'history' as const,
      directory: 'É/<x>&?',
      replica_metric: 'locations' as const,
      min_other_replicas: '0',
      max_other_replicas: '9223372036854775807',
    };
    expect(contentFilterCount(query)).toBe(3);
    expect(contentSummary(query)).toContain('History membership · Directory: É/<x>&?');
    expect(contentSummary(query)).toContain('Minimum 0 · Maximum 9223372036854775807');
  });
  it('has no default advanced restrictions', () => {
    expect(contentFilterCount({ scope: 'current', replica_metric: 'disks' })).toBe(0);
  });
});
