// Builds `staqly` from this checkout (with the dashboard already exported into apps/api/web by
// `make dashboard`), starts a local-mode and a cloud-mode server with temporary data directories,
// and seeds both with layout-hostile data through the REST API.
//   UI_LOCAL_PORT (default 43333) / UI_CLOUD_PORT (default 43433): API ports; dashboards at +5000
//   UI_BIN=/path/to/staqly skips the build; UI_KEEP=1 leaves the servers running
import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { existsSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { chromium } from 'playwright'
import type { TestProject } from 'vitest/node'

import { cloudDatabaseURL } from '../lib/cloud-db.js'
import { makeMedia } from './media.js'
import { seed, type SeedResult } from './seed.js'

export interface Target {
  mode: 'local' | 'cloud'
  /** dashboard origin */
  dashboard: string
  api: string
  /** cloud: an admin key that signs the browser in */
  key?: string
  data: SeedResult
}

declare module 'vitest' {
  export interface ProvidedContext {
    uiLocal: Target
    uiCloud: Target
  }
}

const view = (s: { api: string; dashboard: string }) => ({ api: s.api, dashboard: s.dashboard })

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

async function waitHealthy(url: string, proc: ChildProcess, log: () => string) {
  for (let i = 0; i < 150; i++) {
    if (proc.exitCode !== null) {
      throw new Error(`staqly exited (${proc.exitCode}):\n${log()}`)
    }

    try {
      if ((await fetch(`${url}/health`)).ok) {
        return
      }
    } catch {
      // not up yet
    }

    await new Promise((r) => setTimeout(r, 200))
  }

  throw new Error(`staqly did not start at ${url}:\n${log()}`)
}

async function start(bin: string, mode: 'local' | 'cloud', port: number) {
  const dir = mkdtempSync(join(tmpdir(), `staqly-ui-${mode}-`))
  const api = `http://127.0.0.1:${port}`
  let out = ''
  const proc = spawn(bin, ['serve', '--data', dir, '--port', String(port), '--dashboard-port', String(port + 5000)], {
    env: {
      ...process.env,
      STAQLY_MODE: mode,
      DATABASE_URL: mode === 'cloud' ? cloudDatabaseURL('resp') : '',
      PUBLIC_API_URL: api,
      COOKIE_SECURE: 'false',
      RATE_LIMIT_DISABLED: 'true',
      WATCHER_ALLOW_PRIVATE: 'true',
      WEBHOOK_ALLOW_PRIVATE: 'true',
      TAGGER_COMMAND: 'off',
      TAGGER_URL: '',
      STAQLY_API_KEY: '',
      LOG_LEVEL: 'warn',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })

  proc.stdout?.on('data', (d: Buffer) => (out += d.toString()))
  proc.stderr?.on('data', (d: Buffer) => (out += d.toString()))
  await waitHealthy(api, proc, () => out)

  return {
    api,
    dashboard: `http://127.0.0.1:${port + 5000}`,
    stop: async () => {
      if (proc.exitCode === null) {
        const exited = new Promise((r) => proc.once('exit', r))

        proc.kill('SIGTERM')
        await exited
      }

      rmSync(dir, { recursive: true, force: true })
    },
  }
}

export default async function setup(project: TestProject): Promise<() => Promise<void>> {
  if (!existsSync(join(ROOT, 'apps/api/web/index.html'))) {
    throw new Error(
      'apps/api/web has no dashboard: run `make dashboard` first (it exports the dashboard into the binary)',
    )
  }

  let bin = process.env.UI_BIN

  if (!bin) {
    bin = join(mkdtempSync(join(tmpdir(), 'staqly-ui-bin-')), 'staqly')
    execFileSync('go', ['build', '-o', bin, './cmd/staqly'], { cwd: join(ROOT, 'apps/cli'), stdio: 'inherit' })
  }

  const servers: Awaited<ReturnType<typeof start>>[] = []

  try {
    const local = await start(bin, 'local', Number(process.env.UI_LOCAL_PORT ?? 43333))

    servers.push(local)
    const cloud = await start(bin, 'cloud', Number(process.env.UI_CLOUD_PORT ?? 43433))

    servers.push(cloud)
    const browser = await chromium.launch()
    const media = await makeMedia(browser)

    await browser.close()
    const ws = (await (
      await fetch(`${cloud.api}/v1/workspaces`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ name: 'Acme Corporation Marketing & Growth Team Workspace — Zürich' }),
      })
    ).json()) as { api_key: { key: string } }
    const key = ws.api_key.key

    project.provide('uiLocal', { mode: 'local', ...view(local), data: await seed({ url: local.api }, media) })
    project.provide('uiCloud', { mode: 'cloud', ...view(cloud), key, data: await seed({ url: cloud.api, key }, media) })
  } catch (err) {
    await Promise.all(servers.map((s) => s.stop()))

    throw err
  }

  return async () => {
    if (process.env.UI_KEEP === '1') {
      console.log(`UI_KEEP=1: dashboards still running: ${servers.map((s) => s.dashboard).join(', ')}`)

      return
    }

    await Promise.all(servers.map((s) => s.stop()))
  }
}
