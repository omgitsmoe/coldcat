import { describe, expect, it } from 'vitest';
import { directorySummary } from '../../src/lib/features/directories/filters';

describe('directory applied summary', () => {
  it('names immediate navigation and current destination semantics', () => {
    expect(directorySummary({ path: '' })).toBe(
      'Immediate entries · current other disks · any replica count · page limit 50',
    );
  });
  it('preserves zero, recursive files and locations without a history prohibition', () => {
    expect(
      directorySummary({
        recursive: true,
        replica_metric: 'locations',
        other_replicas: '0',
        limit: 1,
      }),
    ).toBe('Recursive files · current other locations · exactly 0 · page limit 1');
  });
  it('names both active range endpoints exactly', () => {
    expect(
      directorySummary({ min_other_replicas: '0', max_other_replicas: '9007199254740993' }),
    ).toContain('minimum 0 · maximum 9007199254740993');
  });
});
