import { readFileSync } from 'node:fs';
import { afterEach, expect, test, vi } from 'vitest';
import type { FullResult, TestCase, TestResult } from '@playwright/test/reporter';
import MatrixReporter from '../real/matrix-reporter';

const passed: FullResult = { status: 'passed', startTime: new Date(), duration: 0 };

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
});

test('full contract gate rejects missing operations and ignores failed-test evidence', async () => {
  vi.stubEnv('COLDCAT_REQUIRE_MATRIX', '1');
  const error = vi.spyOn(console, 'error').mockImplementation(() => {});
  vi.spyOn(console, 'log').mockImplementation(() => {});
  const reporter = new MatrixReporter();
  const contract = JSON.parse(readFileSync('../doc/openapi.json', 'utf8'));
  const operations = Object.entries(contract.paths)
    .filter(([path]) => path !== '/api/v1/snapshots/{id}/directories')
    .flatMap(([path, item]) =>
      Object.keys(item as object)
        .filter((method) => ['get', 'post', 'patch'].includes(method))
        .map((method) => `${method.toUpperCase()} ${path}`),
    );
  const evidence = {
    status: 'failed',
    attachments: [{ name: 'real-operations', body: Buffer.from(JSON.stringify(operations)) }],
  } as TestResult;
  reporter.onTestEnd({} as TestCase, evidence);
  expect(await reporter.onEnd(passed)).toEqual({ status: 'failed' });
  expect(error).toHaveBeenCalledWith(expect.stringContaining('POST /api/v1/disks'));
  reporter.onTestEnd({} as TestCase, { ...evidence, status: 'passed' });
  expect(await reporter.onEnd(passed)).toEqual({ status: 'passed' });
});

test('focused runs do not claim or require the full operation matrix', async () => {
  vi.stubEnv('COLDCAT_REQUIRE_MATRIX', '');
  const log = vi.spyOn(console, 'log');
  expect(await new MatrixReporter().onEnd(passed)).toBeUndefined();
  expect(log).not.toHaveBeenCalled();
});
