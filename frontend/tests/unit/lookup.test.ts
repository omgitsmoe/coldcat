import { describe, expect, it } from 'vitest';
import { lookupInput, algorithms } from '../../src/lib/features/contents/lookup';
import { ApiError } from '../../src/lib/api/errors';
import { errorPresentation } from '../../src/lib/state/presentation';

describe('explicit hash identity', () => {
  it('does not present a malformed 404 as a confirmed missing resource', () => {
    expect(
      errorPresentation(new ApiError('contract', 'Malformed', { status: 404 }), 'Content').title,
    ).toBe('Unexpected backend response');
    expect(
      errorPresentation(new ApiError('http', 'Missing', { status: 404 }), 'Content').title,
    ).toBe('Content not found');
  });
  it('retains algorithm, literal case and scope without inferring equality', () => {
    expect(lookupInput('md5', 'AB'.repeat(16))).toEqual({
      hash_type: 'md5',
      hash: 'AB'.repeat(16),
      scope: 'history',
    });
    expect(algorithms).toContain('sha3_224');
  });
  it.each(['', 'abc', 'zz', ' ab ', 'ab\n'])('rejects invalid digest %j', (value) => {
    expect(() => lookupInput('sha256', value)).toThrow(/hexadecimal/);
  });
  it('rejects unknown algorithms and leaves supported-length checks to the server', () => {
    expect(() => lookupInput('invented', 'ab')).toThrow();
    expect(lookupInput('sha256', 'ab')).toEqual({
      hash_type: 'sha256',
      hash: 'ab',
      scope: 'history',
    });
  });
});
