import { defineConfig } from 'astro/config';

// Static output. SITE_BASE lets a host that serves from a sub-path (for
// example GitHub Pages project pages) set it at build time; default is "/".
export default defineConfig({
  output: 'static',
  base: process.env.SITE_BASE || '/',
  trailingSlash: 'always',
  build: { format: 'directory' },
  devToolbar: { enabled: false },
});
