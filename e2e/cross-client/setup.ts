// Builds the staqly binary from this checkout and starts three real servers
// for the whole run, each with its own temporary data directory:
//   local      local mode, every client with its access token (XC_LOCAL_PORT, default 23333; dashboard +5000)
//   cloud      cloud mode, rate limits off (XC_CLOUD_PORT, default 24333)
//   limited    cloud mode with a tiny per-key rate limit, for 429 + Retry-After (XC_LIMITED_PORT, default 25333)
// XC_BIN=/path/to/staqly skips the build.
import type { TestProject } from 'vitest/node'

import { buildBinary, type RunningServer, type ServerInfo, startServer } from './lib.js'

declare module 'vitest' {
  export interface ProvidedContext {
    bin: string
    local: ServerInfo
    cloud: ServerInfo
    limited: ServerInfo
  }
}

const port = (name: string, def: number) => Number(process.env[name] ?? def)
const info = ({ url, mode, dataDir, databaseURL, token, pid }: RunningServer): ServerInfo => ({
  url,
  mode,
  dataDir,
  databaseURL,
  token,
  pid,
})

export default async function setup(project: TestProject): Promise<() => Promise<void>> {
  const bin = await buildBinary()
  const servers: RunningServer[] = []

  try {
    const local = await startServer(bin, {
      mode: 'local',
      port: port('XC_LOCAL_PORT', 23333),
      dashboardPort: port('XC_LOCAL_PORT', 23333) + 5000,
    })

    servers.push(local)
    const cloud = await startServer(bin, {
      mode: 'cloud',
      port: port('XC_CLOUD_PORT', 24333),
      dashboardPort: port('XC_CLOUD_PORT', 24333) + 5000,
      env: { TRUST_PROXY: 'true' }, // see signupRaw
    })

    servers.push(cloud)
    const limited = await startServer(bin, {
      mode: 'cloud',
      port: port('XC_LIMITED_PORT', 25333),
      dashboardPort: port('XC_LIMITED_PORT', 25333) + 5000,
      // 3 requests at once, then one per second per key (Retry-After: 1)
      env: {
        RATE_LIMIT_DISABLED: 'false',
        RATE_LIMIT_AUTH_PER_MINUTE: '60',
        RATE_LIMIT_AUTH_BURST: '3',
        TRUST_PROXY: 'true',
      },
    })

    servers.push(limited)
    project.provide('bin', bin)
    project.provide('local', info(local))
    project.provide('cloud', info(cloud))
    project.provide('limited', info(limited))
  } catch (err) {
    await Promise.all(servers.map((s) => s.stop()))

    throw err
  }

  return async () => {
    if (process.env.XC_KEEP === '1') {
      console.log(`XC_KEEP=1: servers still running: ${servers.map((s) => `${s.url} (pid ${s.pid})`).join(', ')}`)

      return
    }

    await Promise.all(servers.map((s) => s.stop()))
  }
}
