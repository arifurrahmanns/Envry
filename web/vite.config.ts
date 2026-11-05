import { mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

// The Go binary embeds the UI with go:embed, which cannot reach outside its
// package directory, so the build goes straight into internal/webui/dist.
const outDir = fileURLToPath(new URL('../internal/webui/dist', import.meta.url))

// Where `make dev` runs the Go server. Override with ENVRY_DEV_API.
const api = process.env.ENVRY_DEV_API ?? 'http://127.0.0.1:8025'

// emptyOutDir wipes the committed .gitkeep; put it back so a fresh clone
// (and git status) stay clean and `go build` works without a UI build.
function keepDistPlaceholder(): Plugin {
  return {
    name: 'envry:keep-dist-placeholder',
    apply: 'build',
    closeBundle() {
      mkdirSync(outDir, { recursive: true })
      writeFileSync(`${outDir}/.gitkeep`, '')
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepDistPlaceholder()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: api, changeOrigin: true },
      '/ws': {
        target: api.replace(/^http/, 'ws'),
        ws: true,
        changeOrigin: true,
        // The server only accepts same-origin WebSockets; present the
        // proxied request as coming from the API's own origin.
        configure: (proxy) => {
          proxy.on('proxyReqWs', (proxyReq) => proxyReq.setHeader('origin', api))
        },
      },
    },
  },
  build: {
    outDir,
    emptyOutDir: true,
    rolldownOptions: {
      output: {
        // Third-party code changes rarely; keep it in its own cacheable chunk.
        advancedChunks: {
          groups: [{ name: 'vendor', test: /[\\/]node_modules[\\/]/ }],
        },
      },
    },
  },
})
