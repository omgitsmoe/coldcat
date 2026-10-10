import assert from 'node:assert/strict';
import { Buffer } from 'node:buffer';
import console from 'node:console';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { format } from 'prettier';

const [input, output] = process.argv.slice(2);
assert(
  input && output,
  'Usage: node scripts/summarize-responsiveness.mjs <report.json> <evidence.json>',
);
const report = JSON.parse(readFileSync(input, 'utf8'));
assert.equal(report.stats.expected, 20);
for (const key of ['unexpected', 'flaky', 'skipped']) assert.equal(report.stats[key], 0);
const runs = [];
function collect(suite) {
  for (const child of suite.suites ?? []) collect(child);
  for (const spec of suite.specs ?? []) {
    for (const test of spec.tests) {
      assert.equal(test.results.length, 1, 'No retries in measurement evidence');
      const result = test.results[0];
      assert.equal(result.status, 'passed');
      const attachments = result.attachments.filter((a) => a.name === 'ui-responsiveness-evidence');
      assert.equal(attachments.length, 1);
      const run = JSON.parse(Buffer.from(attachments[0].body, 'base64').toString('utf8'));
      runs.push(run);
    }
  }
}
for (const suite of report.suites) collect(suite);
assert.equal(runs.length, 20);
runs.sort((a, b) => a.repeat - b.repeat);
const first = runs[0];
for (const [index, run] of runs.entries()) {
  assert.equal(run.repeat, index + 1);
  assert.equal(run.browser, first.browser);
  assert.deepEqual(run.host, first.host);
  assert.deepEqual(run.profile, first.profile);
}
function distribution(values) {
  const sorted = [...values].sort((a, b) => a - b);
  return { min: sorted[0], max: sorted.at(-1), p95: sorted[Math.ceil(sorted.length * 0.95) - 1] };
}
const measurements = runs.map((run) => {
  const e = run.evidence;
  const settled = e.scheduled.filter((request) => !request.continuation);
  assert.equal(settled.length, 1);
  assert.equal(e.handlers.length, 6);
  assert.equal(e.renders.length, 12);
  assert.equal(run.calls.length, 12);
  const initialTasks = e.longTasks.filter(
    (task) => task.start < e.firstRenderAt && task.start + task.duration > e.firstInputAt,
  );
  const metrics = {
    inputMax: Math.max(...e.handlers),
    scheduling: settled[0].afterLastInput,
    firstRender: e.renders[0],
    singleViewMax: Math.max(e.renders[0], e.renders[10], e.renders[11]),
    accumulationMax: Math.max(...e.renders.slice(1, 10)),
    bodyWaitMax: Math.max(...e.scheduled.map((request) => request.bodyWait)),
    initialLongTaskMax: Math.max(0, ...initialTasks.map((task) => task.duration)),
    traversalLongTaskMax: Math.max(0, ...e.longTasks.map((task) => task.duration)),
  };
  assert(metrics.inputMax < 50);
  assert(metrics.scheduling >= 150 && metrics.scheduling <= 250);
  assert(metrics.singleViewMax <= 50);
  assert(metrics.accumulationMax <= 100);
  assert(metrics.initialLongTaskMax <= 50);
  assert.deepEqual(
    run.samples.map((sample) => sample.rows),
    [0, 50, 500, 50, 50, 0],
  );
  return {
    repeat: run.repeat,
    metrics,
    handlers: e.handlers,
    renders: e.renders,
    responseBodyWaits: e.scheduled.map((request) => request.bodyWait),
    initialWindow: { start: e.firstInputAt, end: e.firstRenderAt },
    longTasks: e.longTasks,
    gcIntervals: run.gcIntervals,
    samples: run.samples,
  };
});
const summary = Object.fromEntries(
  Object.keys(measurements[0].metrics).map((key) => [
    key,
    distribution(measurements.map((run) => run.metrics[key])),
  ]),
);
const evidence = {
  schema: 1,
  applicationBaseline: execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(),
  measurementTestSHA256: createHash('sha256')
    .update(readFileSync('tests/browser/responsiveness.spec.ts'))
    .digest('hex'),
  startedAt: report.stats.startTime,
  durationMS: report.stats.duration,
  runs: 20,
  freshContextPerRun: 'Playwright default isolated page/context fixture; serial worker, no retries',
  browser: first.browser,
  host: first.host,
  container: {
    cpuMax: readFileSync('/sys/fs/cgroup/cpu.max', 'utf8').trim(),
    memoryMax: readFileSync('/sys/fs/cgroup/memory.max', 'utf8').trim(),
  },
  profile: first.profile,
  statistic:
    'Nearest-rank p95 across 20 per-run worst-case values (rank 19); scheduling is one value per run',
  units: 'Timing milliseconds; heap bytes; DOM counters from Chromium CDP',
  approvedBudgets: {
    inputMaxExclusiveMS: 50,
    schedulingRangeMS: [150, 250],
    singleViewMaxMS: 50,
    accumulationMaxMS: 100,
    initialLongTaskMaxMS: 50,
  },
  budgetResult: '20/20 passed every budget; no retries, skipped runs or flaky results',
  summary,
  measurements,
};
mkdirSync(dirname(output), { recursive: true });
writeFileSync(output, await format(JSON.stringify(evidence), { parser: 'json', printWidth: 100 }));
console.log(JSON.stringify({ budgetResult: evidence.budgetResult, summary }, null, 2));
