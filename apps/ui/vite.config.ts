import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [tailwindcss(), sveltekit()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      // Auth routes proxy verbatim; /auth/login and /auth/callback do 302
      // redirects that must terminate at the CP host so cookies land there.
      '/auth': 'http://localhost:8080'
    }
  }
});
