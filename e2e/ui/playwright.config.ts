import { join } from 'node:path'

import { defineConfig, devices } from '@playwright/test'

// Dashboard UI suite: a real browser against real `staqly serve` processes
// built from this checkout (local and cloud mode). See e2e/README.md.
//   pnpm --filter @staqly/e2e test:ui
const local = `http://127.0.0.1:${process.env.UI_LOCAL_DASHBOARD_PORT ?? 61080}`
const cloud = `http://127.0.0.1:${process.env.UI_CLOUD_DASHBOARD_PORT ?? 61480}`

export default defineConfig({
  testDir: './tests',
  outputDir: './.results',
  globalSetup: './global-setup.ts',
  // One shared server per mode: specs run one after another so counts and lists stay predictable.
  workers: 1,
  fullyParallel: false,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { outputFolder: './.report', open: 'never' }]],
  use: {
    ...devices['Desktop Chrome'],
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    actionTimeout: 15_000,
    // The design editor's background removal runs on WebGPU when there is one: a software adapter here.
    launchOptions: {
      args: [
        '--enable-unsafe-webgpu',
        '--enable-features=Vulkan',
        '--use-angle=swiftshader',
        '--enable-unsafe-swiftshader',
      ],
    },
  },
  projects: [
    {
      name: 'local',
      testMatch: /local\/.*\.spec\.ts/,
      use: { baseURL: local, storageState: join(import.meta.dirname, '.state/local.json') },
    },
    {
      name: 'cloud',
      testMatch: /cloud\/.*\.spec\.ts/,
      use: { baseURL: cloud, storageState: join(import.meta.dirname, '.state/cloud.json') },
    },
  ],
})
