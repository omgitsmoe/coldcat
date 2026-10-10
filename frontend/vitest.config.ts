import { svelte, vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { svelteTesting } from '@testing-library/svelte/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [svelte({ configFile: false, preprocess: vitePreprocess() }), svelteTesting()],
  test: { environment: 'jsdom', include: ['tests/unit/**/*.test.ts'] },
});
