import { defineConfig } from '@playwright/test';
import baseline from './playwright.u01.config';

export default defineConfig({
  ...baseline,
  testMatch: ['u01-baseline.spec.ts', 'u02-controls.spec.ts'],
});
