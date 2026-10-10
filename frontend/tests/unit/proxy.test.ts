import { describe, expect, it } from 'vitest';
import { backendTarget, proxyRules } from '../../dev-proxy';

describe('development proxy boundary', () => {
  it('uses an explicit loopback default and preserves API paths', () => {
    expect(backendTarget(undefined)).toBe('http://127.0.0.1:8080');
    const rules = proxyRules('http://127.0.0.1:8080');
    const matches = (path: string) => Object.keys(rules).some((key) => new RegExp(key).test(path));
    expect(matches('/healthz')).toBe(true);
    expect(matches('/healthz?x=1')).toBe(true);
    expect(matches('/api/v1/disks')).toBe(true);
    expect(matches('/healthz-extra')).toBe(false);
    expect(matches('/api/v10/disks')).toBe(false);
    expect(matches('/contents/123')).toBe(false);
  });

  it('rejects non-loopback, credentials, and path-bearing targets', () => {
    for (const target of [
      'https://example.com',
      'http://user@localhost:8080',
      'http://localhost:8080/api',
      'http://localhost:8080?x=1',
      '',
    ]) {
      expect(() => backendTarget(target)).toThrow();
    }
    expect(backendTarget('http://localhost:9090')).toBe('http://localhost:9090');
  });
});
