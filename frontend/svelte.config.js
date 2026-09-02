import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  preprocess: vitePreprocess(),
  kit: {
    // adapter-node : `npm run build` produit build/handler.js, embarqué par
    // server.js (cf. docker-compose.prod.yml). adapter-auto ne reconnaissait
    // aucune plateforme ici et faisait échouer le build.
    adapter: adapter()
  }
};

export default config;
