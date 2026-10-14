import { defineConfig } from 'astro/config';
import svelte from '@astrojs/svelte';
const cache = process.env.LIBRARY_CACHE_DIR;
export default defineConfig({ output: 'static', ...(cache ? {cacheDir: `${cache}/astro-cache`} : {}), devToolbar: { enabled: false }, integrations: [svelte()], vite: { ...(cache ? {cacheDir: `${cache}/vite`} : {}), server: { proxy: { '/api': 'http://127.0.0.1:8080', '/covers': 'http://127.0.0.1:8080' } } } });
