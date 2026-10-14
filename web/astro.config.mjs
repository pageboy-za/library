import { defineConfig } from 'astro/config';
import svelte from '@astrojs/svelte';
export default defineConfig({ output: 'static', devToolbar: { enabled: false }, integrations: [svelte()], vite: { server: { proxy: { '/api': 'http://127.0.0.1:8080', '/covers': 'http://127.0.0.1:8080' } } } });
