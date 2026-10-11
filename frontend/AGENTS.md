# Frontend task notes

- U02 foundation inspection: after `npm run build`, run
  `npx playwright test --config playwright.u02.config.ts`. It reuses U01's disposable
  API fixtures for 18 page captures and tests native shared controls at 1440/1024/768px.
  Preserve `test-results/` artifacts before another Playwright suite; never replace
  `doc/ui-ux/u01/baseline/` with redesigned captures. U01 is owner-approved.
- For browser focus-state assertions, use keyboard Tab/Shift+Tab. Programmatic focus
  following mouse interaction need not match native `:focus-visible`.
