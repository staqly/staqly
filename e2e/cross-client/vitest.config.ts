import { defineConfig } from 'vitest/config'

// Live cross-client suite: real `staqly serve` processes, no Docker, no mocks.
// pnpm --filter @staqly/e2e test:cross-client
export default defineConfig({
  test: {
    root: import.meta.dirname,
    include: ['*.test.ts'],
    globalSetup: ['./setup.ts'],
    fileParallelism: false,
    testTimeout: 120_000,
    hookTimeout: 600_000,
    reporters: ['verbose'],
  },
})
