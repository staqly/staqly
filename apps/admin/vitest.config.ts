import { fileURLToPath } from 'node:url'

import { defineConfig } from 'vitest/config'

export default defineConfig({
  resolve: { alias: { '@': fileURLToPath(new URL('./', import.meta.url)) } },
  test: { environment: 'jsdom', globals: true, include: ['lib/**/*.test.{ts,tsx}'], css: false },
})
