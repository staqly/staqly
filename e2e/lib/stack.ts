// The Docker stack under test: compose.yml + compose.e2e.yml with e2e/e2e.env.
import { execFile, spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'

import { waitFor } from './wait.js'

const run = promisify(execFile)

export const ROOT = fileURLToPath(new URL('../../', import.meta.url))
export const API = 'http://localhost:13333'
export const DASHBOARD = 'http://localhost:18080'
export const RECEIVER = 'http://localhost:19000'
// The receiver as the API container sees it (compose service name).
export const RECEIVER_INTERNAL = 'http://receiver:9000'
export const API_CONTAINER = 'staqly-e2e'
export const SETUP_TOKEN = 'e2e-token'
// Local mode's access token, fixed by STAQLY_LOCAL_TOKEN in e2e.env (every local request needs it).
export const LOCAL_TOKEN = 'stk_local_e2etokene2etokene2etokene2etoken'
// Cloud mode runs on PostgreSQL: the e2e stack's postgres service, as the api container reaches it.
export const CLOUD_DATABASE_URL = 'postgres://staqly:staqly@postgres:5432/staqly?sslmode=disable'

const COMPOSE = ['compose', '-f', 'compose.yml', '-f', 'compose.e2e.yml', '--env-file', 'e2e/e2e.env']

/** Runs `docker <args>` and returns stdout; throws with stderr on failure. */
export async function docker(args: string[], env: Record<string, string> = {}): Promise<string> {
  try {
    const { stdout } = await run('docker', args, {
      cwd: ROOT,
      env: { ...process.env, ...env },
      maxBuffer: 64 << 20,
    })

    return stdout
  } catch (err) {
    const { stderr, stdout } = err as { stderr?: string; stdout?: string }

    throw new Error(`docker ${args.join(' ')} failed:\n${stderr || stdout || String(err)}`, { cause: err })
  }
}

/** Runs `docker compose <args>` against the e2e project. */
export function compose(args: string[], env: Record<string, string> = {}): Promise<string> {
  return docker([...COMPOSE, ...args], env)
}

/** Like `compose`, but streams the output to the terminal (slow commands such as builds). */
export function composeVisible(args: string[], env: Record<string, string> = {}): Promise<void> {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', [...COMPOSE, ...args], {
      cwd: ROOT,
      env: { ...process.env, ...env },
      stdio: ['ignore', 'inherit', 'inherit'],
    })

    child.on('error', reject)
    child.on('exit', (code) => {
      if (code === 0) {
        resolve()
      } else {
        reject(new Error(`docker compose ${args.join(' ')} exited with ${code}`))
      }
    })
  })
}

/** The container's log lines (stdout and stderr) since an ISO timestamp. */
export async function containerLogs(since: string, container = API_CONTAINER): Promise<string[]> {
  const { stdout, stderr } = await run('docker', ['logs', '--since', since, container], { maxBuffer: 64 << 20 })

  return `${stdout}\n${stderr}`.split('\n').filter(Boolean)
}

/** Runs the operator CLI inside the api container, e.g. `server connect-link`. */
export async function server(...args: string[]): Promise<string> {
  return (await docker(['exec', API_CONTAINER, 'server', ...args])).trim()
}

/** Waits until a URL answers 2xx. */
export async function waitForHttp(url: string, timeout = 90_000): Promise<void> {
  await waitFor(
    async () => {
      try {
        return (await fetch(url)).ok
      } catch {
        return false
      }
    },
    { timeout, message: `${url} did not answer 2xx` },
  )
}

/** Waits until the API and the dashboard answer. */
export async function waitForStack(): Promise<void> {
  await waitForHttp(`${API}/health`)
  await waitForHttp(`${DASHBOARD}/`)
}
