import { defineConfig } from '@playwright/test';
import real from './playwright.real.config';

export default defineConfig({
  ...real,
  projects: [{ name: 'deployed' }],
  reporter: [['list'], ['./tests/real/matrix-reporter.ts']],
});
