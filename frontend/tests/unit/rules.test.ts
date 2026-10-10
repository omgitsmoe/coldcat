import { describe, expect, it } from 'vitest';
import { ruleInput } from '../../src/lib/features/directories/rules';

describe('comparison rule drafts', () => {
  it('copies literal rows without trimming, splitting or interpreting escapes', () => {
    const draft = { allow: [' **/a\\?.txt ', '雪/{a,b}'], block: ['**/*.log'] };
    const applied = ruleInput(draft);
    expect(applied).toEqual(draft);
    draft.allow[0] = 'changed';
    expect(applied.allow[0]).toBe(' **/a\\?.txt ');
  });
  it('distinguishes no rules from an empty row and checks UTF-8 limits', () => {
    expect(ruleInput({ allow: [], block: [] })).toEqual({ allow: [], block: [] });
    expect(() => ruleInput({ allow: [''], block: [] })).toThrow('1..1024');
    expect(() => ruleInput({ allow: ['雪'.repeat(342)], block: [] })).toThrow('1..1024');
    expect(() => ruleInput({ allow: Array(101).fill('*'), block: [] })).toThrow('100');
    expect(() => ruleInput({ allow: Array(17).fill('a'.repeat(1024)), block: [] })).toThrow(
      '16384',
    );
    expect(() => ruleInput({ allow: ['a\0b'], block: [] })).toThrow();
  });
});
