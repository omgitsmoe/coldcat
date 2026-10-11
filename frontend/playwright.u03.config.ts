import { defineConfig } from '@playwright/test';
import baseline from './playwright.u01.config';

export default defineConfig({
  ...baseline,
  testDir: './tests',
  testMatch: ['visual/u03-shell.spec.ts', 'browser/shell.spec.ts'],
});
