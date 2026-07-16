import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({
      fallback: 'index.html'
    }),
    // We're a SPA with fallback: 'index.html'. Dynamic routes like /functions/[id]
    // can't be prerendered (no static crawl target) — the fallback handles them
    // at runtime.
    prerender: {
      handleUnseenRoutes: 'ignore'
    }
  }
};

export default config;
