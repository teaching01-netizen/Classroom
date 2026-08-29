/// <reference types="vitest" />
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

const buildId = process.env['FRONTEND_BUILD_ID']?.trim() || Date.now().toString(36)
const versionPayload = JSON.stringify({ success: true, data: { buildId } })
const buildVersionPlugin: Plugin = {
  name: 'build-version',
  configureServer(server) {
    server.middlewares.use('/version.json', (_request, response) => {
      response.statusCode = 200
      response.setHeader('Cache-Control', 'no-store, no-cache, must-revalidate, max-age=0')
      response.setHeader('Content-Type', 'application/json; charset=utf-8')
      response.end(versionPayload)
    })
  },
  generateBundle() {
    this.emitFile({
      type: 'asset',
      fileName: 'version.json',
      source: versionPayload,
    })
  },
}

export default defineConfig({
  define: {
    'import.meta.env.VITE_APP_BUILD_ID': JSON.stringify(buildId),
  },
  plugins: [react(), buildVersionPlugin],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.{test,spec}.{js,jsx,ts,tsx}'],
    setupFiles: ['./src/testing/setup.ts'],
  },
  server: {
    port: 5175,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:3001',
        changeOrigin: true
      },
      '/ws': {
        target: 'ws://127.0.0.1:3001',
        ws: true
      }
    }
  }
})
