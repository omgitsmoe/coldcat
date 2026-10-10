import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { backendTarget, proxyRules } from './dev-proxy.ts';

export default defineConfig({
  plugins: [
    sveltekit({
      preprocess: vitePreprocess(),
      adapter: adapter({ fallback: 'index.html' }),
    }),
  ],
  server: {
    host: '127.0.0.1',
    strictPort: true,
    proxy: proxyRules(backendTarget(process.env.COLDCAT_BACKEND)),
  },
});
