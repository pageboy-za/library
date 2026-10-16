import { defineConfig } from 'vite';
import { sveltekit } from '@sveltejs/kit/vite';
import adapter from '@sveltejs/adapter-static';

export default defineConfig({
  plugins: [sveltekit({
    adapter: adapter({ pages: 'dist', assets: 'dist', fallback: 'book-view/index.html' }),
    paths: { relative: false },
    version: { name: process.env.LIBRARY_APP_VERSION || 'development' },
    prerender: { entries: ['/', '/join/', '/membership/', '/staff/members/', '/catalogue/', '/scan/', '/settings/', '/shelves/', '/borrowers/', '/loans/', '/discover/', '/book/', '/staff/', '/staff/login/', '/staff/account/', '/staff/accounts/', '/staff/catalogue/', '/staff/scan/', '/staff/settings/', '/staff/shelves/', '/staff/borrowers/', '/staff/loans/', '/staff/discover/'] },
  })],
  server: { proxy: { '/api': 'http://127.0.0.1:8080', '/covers': 'http://127.0.0.1:8080' } },
});
