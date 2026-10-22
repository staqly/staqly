import { defineConfig } from '@playwright/test'

// The README's demo recording (demo/record.spec.ts): one run makes both GIFs. `pnpm --filter @staqly/e2e demo:gif`
export default defineConfig({
  testDir: '.',
  testMatch: 'record.spec.ts',
  timeout: Number(process.env.DEMO_TIMEOUT ?? 10 * 60_000),
  workers: 1,
  reporter: 'list',
  use: { browserName: 'chromium', actionTimeout: 15_000 },
})
