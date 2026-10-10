import { readFileSync } from 'node:fs';
import type { FullResult, Reporter, TestCase, TestResult } from '@playwright/test/reporter';

export default class MatrixReporter implements Reporter {
  private covered = new Set<string>();

  onTestEnd(_test: TestCase, result: TestResult) {
    if (result.status !== 'passed') return;
    for (const attachment of result.attachments) {
      if (attachment.name !== 'real-operations' || !attachment.body) continue;
      for (const operation of JSON.parse(attachment.body.toString()) as string[])
        this.covered.add(operation);
    }
  }

  async onEnd(result: FullResult) {
    if (process.env.COLDCAT_REQUIRE_MATRIX !== '1') return;
    const contract = JSON.parse(readFileSync('../doc/openapi.json', 'utf8'));
    const expected: string[] = [];
    const excluded = 'GET /api/v1/snapshots/{id}/directories';
    for (const [path, item] of Object.entries(contract.paths)) {
      for (const method of Object.keys(item as object)) {
        if (!['get', 'post', 'patch'].includes(method)) continue;
        const operation = `${method.toUpperCase()} ${path}`;
        if (operation !== excluded) expected.push(operation);
      }
    }
    const missing = expected.filter((operation) => !this.covered.has(operation));
    console.log(
      `Real browser contract matrix: ${expected.length - missing.length}/${expected.length} UI-exposed operations; directories-only is not exposed.`,
    );
    if (missing.length) {
      console.error(`Missing successful browser operations: ${missing.join(', ')}`);
      return { status: 'failed' as const };
    }
    return { status: result.status };
  }
}
