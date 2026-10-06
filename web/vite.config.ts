/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import vuetify from 'vite-plugin-vuetify'
import { defineConfig, type Plugin } from 'vite'

// runsten-api during development (make run-api). The dev server relays to it, keeping
// Host like runsten-web does: CrossOriginProtection compares it with the Origin.
const api = process.env.RUNSTEN_WEB_API_URL ?? 'http://127.0.0.1:8081'

// The map tiles during development, as runsten-web writes them into the page:
// OpenStreetMap's unless RUNSTEN_WEB_MAP_TILES says otherwise, empty for none.
const tiles = process.env.RUNSTEN_WEB_MAP_TILES ?? 'https://tile.openstreetmap.org/{z}/{x}/{y}.png'
const attribution = process.env.RUNSTEN_WEB_MAP_ATTRIBUTION || '© OpenStreetMap contributors'
const escape = (s: string) => s.replace(/[&<>"]/g, (c) => `&#${c.charCodeAt(0)};`)
const mapMeta: Plugin = {
  name: 'runsten-map-meta',
  apply: 'serve',
  transformIndexHtml: (html) =>
    tiles
      ? html.replace(
          '<meta name="csp-nonce" content="" />',
          `$&<meta name="map-tiles" content="${escape(tiles)}" /><meta name="map-attribution" content="${escape(attribution)}" />`,
        )
      : html,
}

export default defineConfig({
  // Relative assets: the app works under a reverse proxy prefix (<base href>).
  base: './',
  plugins: [vue(), vuetify({ autoImport: true }), mapMeta],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@test': fileURLToPath(new URL('./test', import.meta.url)),
      // The API's reference responses, written by its Go tests: the front end's fixtures.
      '@fixtures': fileURLToPath(new URL('../internal/api/testdata', import.meta.url)),
    },
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    // As runsten-web relays them (internal/web/web.go, relayed).
    proxy: {
      '/api/': { target: api, changeOrigin: false },
      '/auth/': { target: api, changeOrigin: false },
      '/.well-known/oauth-protected-resource': { target: api, changeOrigin: false },
      '/.well-known/oauth-authorization-server': { target: api, changeOrigin: false },
    },
  },
  build: {
    // Vuetify alone is over the default 500 kB warning before gzip.
    chunkSizeWarningLimit: 1024,
    // A font is always a file of its own: the CSP allows fonts from the origin, not data:
    // URLs, which Vite would make of the small ones.
    assetsInlineLimit: (file) => (/\.woff2?$/.test(file) ? false : undefined),
  },
  test: {
    environment: 'jsdom',
    include: ['test/**/*.test.ts'],
    setupFiles: ['test/unit.setup.ts'],
    // Every core, not Vitest's default of all but one: the CI's runner has two, which left
    // a single worker and tripled the run.
    maxWorkers: '100%',
    server: { deps: { inline: ['vuetify'] } },
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,vue}'],
      exclude: ['src/app/main.ts', 'src/shared/api/schema.d.ts'],
      thresholds: { lines: 70, functions: 70, branches: 70, statements: 70 },
    },
  },
})
