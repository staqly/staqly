// Builds staqly with the dashboard embedded, starts a local-mode and a
// cloud-mode server plus a loopback receiver, seeds both workspaces through
// the REST API and signs a browser in to each (local: the #token= link the
// server prints, cloud: the admin API key). Everything the specs need lands in
// .state/state.json; the browser sessions in .state/local.json / cloud.json.
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'

import { chromium, type FullConfig } from '@playwright/test'

import { Api } from './lib/api.js'
import { FAL_E2E_KEY, startReceiver } from './lib/receiver.js'
import { seedWorkspace } from './lib/seed.js'
import { buildBinary, type RunningServer, startServer, tokenLink } from './lib/stack.js'
import { type State, state, STATE_DIR } from './lib/state.js'

const port = (name: string, def: number) => Number(process.env[name] ?? def)

export const SETUP_TOKEN = 'ui-e2e-setup-token'

/** UI_REUSE=1: servers left running by an earlier UI_KEEP=1 run are used again, as they are. */
async function reusable(): Promise<boolean> {
  if (process.env.UI_REUSE !== '1' || !existsSync(join(STATE_DIR, 'state.json'))) {
    return false
  }

  try {
    const s = state()

    return (await fetch(`${s.local.api}/health`)).ok && (await fetch(`${s.cloud.api}/health`)).ok
  } catch {
    return false
  }
}

export default async function globalSetup(_config: FullConfig): Promise<() => Promise<void>> {
  if (await reusable()) {
    const again = await startReceiver(port('UI_RECEIVER_PORT', 61590))

    return again.close
  }

  rmSync(STATE_DIR, { recursive: true, force: true })
  mkdirSync(STATE_DIR, { recursive: true })
  const bin = await buildBinary()
  const receiver = await startReceiver(port('UI_RECEIVER_PORT', 61590))
  const servers: RunningServer[] = []
  const stop = async () => {
    if (process.env.UI_KEEP === '1') {
      console.log(`UI_KEEP=1: still running: ${servers.map((s) => `${s.dash} (pid ${s.pid})`).join(', ')}`)

      return
    }

    await Promise.all(servers.map((s) => s.stop()))
    await receiver.close()

    for (const s of servers) {
      rmSync(s.dataDir, { recursive: true, force: true })
      rmSync(`${s.dataDir}.log`, { force: true })
    }

    // The binary this run built (not one passed in with UI_BIN).
    if (!process.env.UI_BIN) {
      rmSync(dirname(bin), { recursive: true, force: true })
    }
  }

  try {
    const local = await startServer(bin, {
      mode: 'local',
      port: port('UI_LOCAL_PORT', 61333),
      dashboardPort: port('UI_LOCAL_DASHBOARD_PORT', 61080),
      // As in the Docker image: the dashboard is reachable from other devices (connect links point there).
      // AI tools on the receiver's fake fal.ai (the cloud server has none: there they are hidden).
      env: { DASHBOARD_EXPOSED: 'true', AI_FAL_KEY: FAL_E2E_KEY, AI_FAL_BASE_URL: `${receiver.url}/fal` },
    })

    servers.push(local)
    const cloud = await startServer(bin, {
      mode: 'cloud',
      port: port('UI_CLOUD_PORT', 61433),
      dashboardPort: port('UI_CLOUD_DASHBOARD_PORT', 61480),
      env: { SETUP_TOKEN, TRUST_PROXY: 'true' },
    })

    servers.push(cloud)

    const localApi = new Api(local.api, local.token)
    const localSeed = await seedWorkspace(localApi, receiver.url)
    const signup = await new Api(cloud.api).req<{
      api_key: { key: string }
      recovery_key: string
      workspace: { id: string }
    }>('POST', '/v1/workspaces', { name: 'Acme content', setup_token: SETUP_TOKEN })
    const cloudApi = new Api(cloud.api, signup.api_key.key)
    const cloudSeed = await seedWorkspace(cloudApi, receiver.url, { small: true })
    const agent = await cloudApi.post<{ key: string; id: string }>('/v1/api-keys', {
      name: 'Content agent',
      scopes: ['collections:read', 'collections:write', 'posts:read', 'posts:write', 'posts:publish', 'channels:read'],
    })
    const readOnly = await cloudApi.post<{ key: string; id: string }>('/v1/api-keys', {
      name: 'Read-only dashboard',
      scopes: ['collections:read', 'posts:read'],
    })
    const revoked = await cloudApi.post<{ id: string }>('/v1/api-keys', { name: 'Old CI key', scopes: ['posts:read'] })

    await cloudApi.del(`/v1/api-keys/${revoked.id}`)
    // Let the agent key show a "last used" time.
    await cloudApi.withKey(agent.key).get('/v1/collections')

    const browser = await chromium.launch()

    try {
      const lctx = await browser.newContext()
      const lpage = await lctx.newPage()

      await lpage.goto(tokenLink(local))
      // Signed in once the calendar shows (the link opens the sign-in, then the page).
      await lpage.getByRole('heading', { level: 1, name: 'Schedule' }).waitFor({ timeout: 30_000 })
      await lctx.storageState({ path: join(STATE_DIR, 'local.json') })

      const cctx = await browser.newContext()
      const res = await cctx.request.post(`${cloud.dash}/api/v1/sessions/api-key`, {
        data: { api_key: signup.api_key.key },
      })

      if (res.status() !== 201) {
        throw new Error(`cloud sign-in: ${res.status()} ${await res.text()}`)
      }

      await cctx.storageState({ path: join(STATE_DIR, 'cloud.json') })
    } finally {
      await browser.close()
    }

    const out: State = {
      bin,
      receiver: receiver.url,
      local: {
        api: local.api,
        dash: local.dash,
        dataDir: local.dataDir,
        pid: local.pid,
        token: local.token!,
        seed: localSeed,
      },
      cloud: {
        api: cloud.api,
        dash: cloud.dash,
        dataDir: cloud.dataDir,
        pid: cloud.pid,
        seed: cloudSeed,
        adminKey: signup.api_key.key,
        recoveryKey: signup.recovery_key,
        agentKey: agent.key,
        readOnlyKey: readOnly.key,
        setupToken: SETUP_TOKEN,
      },
    }

    writeFileSync(join(STATE_DIR, 'state.json'), JSON.stringify(out, null, 2))
  } catch (err) {
    for (const s of servers) {
      console.error(`--- ${s.mode} log ---\n${s.log()}`)
    }

    await stop()

    throw err
  }

  return stop
}
