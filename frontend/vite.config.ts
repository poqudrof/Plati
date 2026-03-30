import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        ws: true
      },
      '/auth': 'http://localhost:8080',
      '/health': 'http://localhost:8080'
    }
  }
});
