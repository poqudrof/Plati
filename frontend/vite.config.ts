import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const backendUrl = `http://${process.env.BACKEND_HOST ?? 'localhost'}:8080`;

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    proxy: {
      '/api': {
        target: backendUrl,
        ws: true
      },
      '/auth': backendUrl,
      '/health': backendUrl
    }
  }
});
