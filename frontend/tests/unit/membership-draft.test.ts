import { describe, expect, it } from 'vitest';
import { parseMembershipDraft } from '../../src/lib/features/contents/membership-draft';
import { contentsURL } from '../../src/lib/state/routes';

describe('content membership bookmark correction', () => {
  it('retains an invalid raw disk ID and literal root in a non-requestable draft', () => {
    const draft = parseMembershipDraft(new URLSearchParams('disk_id=not-an-id&directory='));
    expect(draft.disk_id).toBe('not-an-id');
    expect(draft.directory).toBe('');
    expect(() => contentsURL(draft)).toThrow();
  });
  it('keeps unloaded large IDs as strings, not labels or numeric approximations', () => {
    const draft = parseMembershipDraft(new URLSearchParams('disk_id=9007199254740993'));
    expect(draft.disk_id).toBe('9007199254740993');
    expect(contentsURL(draft)).toContain('disk_id=9007199254740993');
  });
  it.each([
    'disk_id=bad&scope=history&other_replicas=0',
    'disk_id=bad&disk_id=2',
    'disk_id=bad&cursor=old',
    'disk_id=bad&unexpected=x',
    'disk_id=bad&limit=no',
  ])('does not broaden correction to unrelated invalid query %s', (query) => {
    expect(() => parseMembershipDraft(new URLSearchParams(query))).toThrow();
  });
});
