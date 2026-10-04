import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'
import { pocketBaseDevTarget } from './devProxy.ts'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, 'src'),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: pocketBaseDevTarget,
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../pb_public',
    emptyOutDir: true,
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts', 'vite.config.test.ts', 'noVendor.test.ts'],
  },
})
