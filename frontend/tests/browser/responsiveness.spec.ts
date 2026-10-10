import { cpus, totalmem, platform, release } from 'node:os';
import { expect, test } from '@playwright/test';
import { searchFixture } from '../fixtures/api';

test('browser evidence: settled typing, response render, bounded DOM and retained heap', async ({
  page,
  browser,
}, info) => {
  test.setTimeout(120_000);
  await page.addInitScript(() => {
    const evidence = {
      handlers: [] as number[],
      inputAt: 0,
      scheduled: [] as { continuation: boolean; afterLastInput: number; bodyWait: number }[],
      decodedAt: 0,
      renders: [] as number[],
      longTasks: [] as { start: number; duration: number }[],
    };
    Object.assign(window, { uiEvidence: evidence });
    let started = 0;
    document.addEventListener(
      'input',
      () => {
        started = performance.now();
        evidence.inputAt = started;
      },
      true,
    );
    window.addEventListener('input', () => evidence.handlers.push(performance.now() - started));
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries())
        evidence.longTasks.push({ start: entry.startTime, duration: entry.duration });
    }).observe({ type: 'longtask', buffered: true });
    const originalFetch = window.fetch;
    window.fetch = async (...args) => {
      const url = String(args[0]);
      const search = url.startsWith('/api/v1/search?');
      const dispatched = performance.now();
      const scheduling = {
        continuation: url.includes('cursor='),
        afterLastInput: dispatched - evidence.inputAt,
        bodyWait: 0,
      };
      if (search) evidence.scheduled.push(scheduling);
      const response = await originalFetch(...args);
      if (search) {
        const text = response.text.bind(response);
        response.text = async () => {
          const body = await text();
          evidence.decodedAt = performance.now();
          scheduling.bodyWait = evidence.decodedAt - dispatched;
          return body;
        };
      }
      return response;
    };
    new MutationObserver(() => {
      if (!evidence.decodedAt || !document.querySelector('tbody tr')) return;
      const decoded = evidence.decodedAt;
      evidence.decodedAt = 0;
      requestAnimationFrame(() => evidence.renders.push(performance.now() - decoded));
    }).observe(document, { childList: true, subtree: true });
  });
  await page.route('**/healthz', (r) => r.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (r) =>
    r.fulfill({
      json: {
        revision: '9007199254740993',
        scope: 'current',
        disk_count: '3',
        file_count: '600',
        content_count: '2',
      },
    }),
  );
  const calls: string[] = [];
  const unexpected: string[] = [];
  page.on('request', (r) => {
    const path = new URL(r.url()).pathname;
    if (path.startsWith('/api/v1/') && !['/api/v1/search', '/api/v1/catalog'].includes(path))
      unexpected.push(path);
  });
  await page.route('**/api/v1/search?**', async (r) => {
    calls.push(r.request().url());
    const n = Number(new URL(r.request().url()).searchParams.get('cursor') ?? '1');
    const fixture = searchFixture();
    fixture.items = Array.from({ length: 50 }, (_, i) => ({
      ...structuredClone(fixture.items[0]!),
      observation: { ...fixture.items[0]!.observation, id: String((n - 1) * 50 + i + 1) },
      basename: `page-${n}-row-${i + 1}`,
    }));
    fixture.next_cursor = n === 12 ? null : String(n + 1);
    await new Promise((resolve) => setTimeout(resolve, 1200));
    await r.fulfill({ json: fixture });
  });
  await page.goto('/');
  await expect(page.getByRole('searchbox')).toBeFocused();
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Performance.enable');
  const samples: { name: string; heap: unknown; dom: unknown; rows: number }[] = [];
  const gcIntervals: { start: number; end: number }[] = [];
  async function sample(name: string) {
    const start = await page.evaluate(() => performance.now());
    await cdp.send('HeapProfiler.collectGarbage');
    gcIntervals.push({ start, end: await page.evaluate(() => performance.now()) });
    samples.push({
      name,
      heap: await cdp.send('Runtime.getHeapUsage'),
      dom: await cdp.send('Memory.getDOMCounters'),
      rows: await page.locator('tbody tr').count(),
    });
  }
  await sample('empty');
  await page.keyboard.type('report', { delay: 20 });
  await expect(page.getByRole('searchbox')).toHaveValue('report');
  await expect(page.locator('tbody tr')).toHaveCount(50);
  expect(calls).toHaveLength(1);
  await sample('50-rows');
  for (let i = 1; i < 10; i++) {
    await page.getByRole('button', { name: 'Load more' }).click();
    await expect(page.locator('tbody tr')).toHaveCount((i + 1) * 50);
  }
  await sample('500-rows');
  for (const n of [11, 12]) {
    await page.getByRole('button', { name: 'Next page' }).click();
    await expect(page.getByRole('link', { name: `page-${n}-row-50`, exact: true })).toBeVisible();
    await expect(page.locator('tbody tr')).toHaveCount(50);
    await sample(`page-${n}`);
  }
  expect(calls).toHaveLength(12);
  expect(unexpected).toEqual([]);
  await page.getByRole('button', { name: 'Clear', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(0);
  await sample('cleared');
  const evidence = await page.evaluate(() => Reflect.get(window, 'uiEvidence') as unknown);
  expect(await page.evaluate(() => Reflect.get(window, 'uiEvidence').handlers.length)).toBe(6);
  expect(await page.evaluate(() => Reflect.get(window, 'uiEvidence').renders.length)).toBe(12);
  const report = {
    browser: browser.version(),
    host: {
      platform: platform(),
      release: release(),
      cpu: cpus()[0]!.model,
      logicalCPUs: cpus().length,
      totalMemory: totalmem(),
    },
    profile: {
      headless: true,
      viewport: page.viewportSize(),
      cpuThrottle: 'none',
      backend: 'intercepted 1200ms delay',
      pageSize: 50,
      pages: 12,
      forcedGC: true,
    },
    evidence,
    gcIntervals,
    samples,
    calls,
  };
  await info.attach('ui-responsiveness-evidence', {
    body: JSON.stringify(report, null, 2),
    contentType: 'application/json',
  });
});
