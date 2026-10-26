import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vitest/config'
import { loadEnv } from 'vite'
import { fileURLToPath, URL } from 'node:url'
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  return {
    plugins: [react(), tailwindcss()],
    resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
    server: {
      host: '127.0.0.1',
      proxy: {
        '/api': {
          target: process.env.GLEAN_API_PROXY || env.GLEAN_API_PROXY || 'http://127.0.0.1:8080',
          changeOrigin: true,
        },
      },
    },
    test: {
      include: ['src/**/*.test.{ts,tsx}'],
      testTimeout: 20_000,
      maxWorkers: 2,
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      css: false,
      restoreMocks: true,
    },
  }
})
