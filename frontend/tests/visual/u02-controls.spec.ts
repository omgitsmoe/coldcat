import { expect, test } from '@playwright/test';
import { diskFixture } from '../fixtures/api';

const literal = 'archive/雪 e\u0301/literal ?#% <report>/' + 'long-segment-'.repeat(30);

test('U02 native foundation controls at approved desktop widths', async ({ page }, testInfo) => {
  await page.route('**/healthz', (route) => route.fulfill({ json: { status: 'ready' } }));
  await page.route('**/api/v1/catalog', (route) =>
    route.fulfill({
      json: {
        revision: '1',
        scope: 'current',
        disk_count: '1',
        file_count: '0',
        content_count: '0',
      },
    }),
  );
  await page.route('**/api/v1/disks*', (route) =>
    route.fulfill({ json: { revision: '1', items: [diskFixture()], next_cursor: null } }),
  );
  await page.goto('/disks');
  await expect(page.getByText('Backend ready')).toBeVisible();
  const css = await page.evaluate(() =>
    Array.from(document.styleSheets)
      .flatMap((sheet) => Array.from(sheet.cssRules, (rule) => rule.cssText))
      .join('\n'),
  );
  await page.setContent(`
    <style>${css}</style>
    <main class="shell-main">
      <h1>U02 native shared foundation</h1>
      <section class="panel" aria-label="Controls">
        <h2>Controls and action variants</h2>
        <fieldset>
          <legend>Native fields</legend>
          <div class="control-group">
            <label class="field">Label <input value="Literal 雪 label" /></label>
            <label class="field">Scope <select><option>Current</option><option>History</option></select></label>
            <label class="check-field"><input type="checkbox" /> Include history</label>
            <label class="field">Search <input class="input-search" type="search" /></label>
            <label class="field">Invalid <input aria-invalid="true" aria-describedby="invalid-help" /></label>
            <label class="field">Unavailable <input disabled value="Still legible" /></label>
          </div>
          <p id="invalid-help" class="status-error">Enter a valid exact value.</p>
          <label class="field">Notes <textarea rows="2">Unknown is not zero.</textarea></label>
        </fieldset>
        <p class="actions">
          <button class="button-primary">Apply</button>
          <button class="button-secondary">Cancel</button>
          <button class="button-quiet">Quiet action</button>
          <button class="button-compact">Select</button>
          <button aria-pressed="true">Selected</button>
          <button disabled>Unavailable action</button>
          <a class="button button-quiet" href="#details">Native link</a>
        </p>
        <p class="actions">
          <button id="stable" class="button-primary">
            <span class="button-labels"><span id="idle">Save</span><span id="busy" aria-hidden="true">Saving changes…</span></span>
          </button>
          <button id="long" style="max-inline-size: 180px">A very long literal action label that must wrap without clipping 雪</button>
        </p>
      </section>
      <section id="details" class="panel" aria-label="Summary">
        <h2>Summary and states</h2>
        <div class="metrics">
          <dl class="metric"><dt>Known subtotal</dt><dd>9,223,372,036,854,775,807 B</dd></dl>
          <dl class="metric"><dt>Unknown files</dt><dd>1</dd></dl>
          <dl class="metric"><dt>Zero bytes</dt><dd>0 B</dd></dl>
        </div>
        <p><span class="badge status-success">Verified</span> <span class="badge status-warning">Incomplete</span> <span class="badge status-error">Unavailable</span></p>
        <table class="data-table">
          <caption>Literal paths remain complete; counts are catalog-wide</caption>
          <thead><tr><th>Path</th><th class="numeric">Copies</th><th>Action</th></tr></thead>
          <tbody><tr aria-selected="true"><td id="literal" class="literal"></td><td class="numeric">0</td><td><button class="button-compact">Open</button></td></tr></tbody>
        </table>
      </section>
      <header id="feature-header">Feature header: no inherited shell border or padding</header>
      <nav id="feature-nav" aria-label="Local links"><a href="#details">Summary</a></nav>
    </main>
  `);
  await page.locator('#literal').evaluate((element, text) => (element.textContent = text), literal);

  for (const width of [1440, 1024, 768]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.getByLabel('Label', { exact: true })).toHaveCSS('min-height', '32px');
    await expect(page.getByRole('button', { name: 'Select', exact: true })).toHaveCSS(
      'min-height',
      '28px',
    );
    await expect(page.getByRole('searchbox')).toHaveCSS('min-height', '36px');
    await page.getByText('Include history', { exact: true }).click();
    await expect(page.getByRole('checkbox')).toBeChecked();
    await page.getByRole('checkbox').uncheck();
    await page.getByLabel('Scope').selectOption('History');
    await expect(page.getByLabel('Scope')).toHaveValue('History');

    const apply = page.getByRole('button', { name: 'Apply', exact: true });
    await apply.hover();
    await expect(apply).toHaveCSS('background-color', 'rgb(165, 212, 255)');
    await apply.focus();
    await page.keyboard.press('Tab');
    await page.keyboard.press('Shift+Tab');
    await expect(apply).toHaveCSS('outline-color', 'rgb(255, 203, 105)');
    await expect(apply).toHaveCSS('outline-width', '2px');
    await expect(page.getByRole('button', { name: 'Unavailable action' })).toBeDisabled();
    await expect(page.locator('#feature-header')).toHaveCSS('padding', '0px');
    await expect(page.locator('#feature-header')).toHaveCSS('border-bottom-width', '0px');
    await expect(page.locator('#feature-nav')).toHaveCSS('display', 'block');

    const before = await page.locator('#stable').boundingBox();
    await page.locator('#stable').evaluate((button) => {
      button.setAttribute('disabled', '');
      button.setAttribute('aria-busy', 'true');
      button.querySelector('#idle')!.setAttribute('aria-hidden', 'true');
      button.querySelector('#busy')!.removeAttribute('aria-hidden');
    });
    await expect(page.getByRole('button', { name: 'Saving changes…' })).toBeVisible();
    await page.locator('#stable').hover();
    await expect(page.locator('#stable')).toHaveCSS('background-color', 'rgb(24, 34, 45)');
    await expect(page.locator('#stable')).toHaveCSS('color', 'rgb(172, 185, 200)');
    const after = await page.locator('#stable').boundingBox();
    expect(after!.width).toBe(before!.width);
    expect(after!.height).toBe(before!.height);
    const wrapping = await page.locator('#long').evaluate((element) => ({
      height: element.getBoundingClientRect().height,
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
    }));
    expect(wrapping.height).toBeGreaterThan(32);
    expect(wrapping.scrollHeight).toBe(wrapping.clientHeight);
    await expect(page.locator('#literal')).toHaveText(literal);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    await page.screenshot({ path: testInfo.outputPath(`controls-${width}.png`), fullPage: true });
    await page.locator('#stable').evaluate((button) => {
      button.removeAttribute('disabled');
      button.removeAttribute('aria-busy');
      button.querySelector('#idle')!.removeAttribute('aria-hidden');
      button.querySelector('#busy')!.setAttribute('aria-hidden', 'true');
    });
  }

  await page.locator('#stable').focus();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await page.locator('#stable').evaluate((button) => button.setAttribute('aria-pressed', 'true'));
  await expect(page.locator('#stable')).toHaveCSS('background-color', 'rgb(165, 212, 255)');
  const contrasts = await page.evaluate(() => {
    function luminance(color: string) {
      const channels = color.match(/\d+/g)!.slice(0, 3).map(Number);
      const linear = channels.map((value) => {
        const component = value / 255;
        return component <= 0.04045 ? component / 12.92 : ((component + 0.055) / 1.055) ** 2.4;
      });
      return linear[0]! * 0.2126 + linear[1]! * 0.7152 + linear[2]! * 0.0722;
    }
    function ratio(first: string, second: string) {
      const a = luminance(first);
      const b = luminance(second);
      return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
    }
    const primary = getComputedStyle(document.querySelector('#stable')!);
    const disabled = getComputedStyle(document.querySelector('button:disabled')!);
    const field = getComputedStyle(document.querySelector('input')!);
    return {
      primary: ratio(primary.color, primary.backgroundColor),
      disabled: ratio(disabled.color, disabled.backgroundColor),
      field: ratio(field.color, field.backgroundColor),
      boundary: ratio(field.borderColor, field.backgroundColor),
      focus: ratio(
        primary.outlineColor,
        getComputedStyle(document.querySelector('.panel')!).backgroundColor,
      ),
    };
  });
  expect(contrasts.primary).toBeGreaterThanOrEqual(4.5);
  expect(contrasts.disabled).toBeGreaterThanOrEqual(4.5);
  expect(contrasts.field).toBeGreaterThanOrEqual(4.5);
  expect(contrasts.boundary).toBeGreaterThanOrEqual(3);
  expect(contrasts.focus).toBeGreaterThanOrEqual(3);
  await expect(page.locator('#stable')).toHaveCSS('outline-color', 'rgb(255, 203, 105)');
});
