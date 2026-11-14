import { defineConfig } from 'vitest/config'

// Responsive layout check of the dashboard: real `staqly serve` processes (local and cloud mode),
// headless Chromium at phone, tablet and desktop widths. Needs `make dashboard` first.
// pnpm --filter @staqly/e2e test:ui-responsive
export default defineConfig({
  test: {
    root: import.meta.dirname,
    include: ['*.test.ts'],
    globalSetup: ['./setup.ts'],
    testTimeout: 90_000,
    // a busy machine can miss one wait; a layout problem fails every time
    retry: 1,
    hookTimeout: 600_000,
    reporters: ['default'],
  },
})
