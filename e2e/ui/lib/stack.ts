// Real `staqly serve` processes for the dashboard UI suite: the binary is built
// from this checkout with the dashboard's static export embedded (as
// `make dashboard` does), each server gets its own temporary data directory.
import { type ChildProcess, execFile, spawn } from 'node:child_process'
import {
  closeSync,
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  readdirSync,
  readFileSync,
  rmSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { cloudDatabaseURL } from '../../lib/cloud-db.js'

export const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../../..')

export interface ServerInfo {
  mode: 'local' | 'cloud'
  /** API base URL (127.0.0.1:port). */
  api: string
  /** Dashboard base URL. */
  dash: string
  dataDir: string
  pid: number
  /** Local mode: the access token every request needs (the server made it in dataDir/access_token). */
  token?: string
}

export interface RunningServer extends ServerInfo {
  proc: ChildProcess
  log: () => string
  stop: (signal?: NodeJS.Signals) => Promise<void>
}

export function run(cmd: string, args: string[], opts: { cwd?: string; env?: NodeJS.ProcessEnv } = {}) {
  return new Promise<string>((resolvePromise, reject) => {
    execFile(
      cmd,
      args,
      { cwd: opts.cwd, env: opts.env ?? process.env, timeout: 900_000, maxBuffer: 64 << 20 },
      (err, stdout, stderr) => {
        if (err) {
          reject(new Error(`${cmd} ${args.join(' ')}: ${err.message}\n${stdout}\n${stderr}`))

          return
        }

        resolvePromise(stdout)
      },
    )
  })
}

/**
 * Builds the dashboard export, embeds it (apps/api/web, like `make dashboard`) and builds the
 * staqly binary. UI_BIN=/path/to/staqly skips all of it; UI_SKIP_DASHBOARD=1 reuses the export
 * already in apps/api/web.
 */
export async function buildBinary(): Promise<string> {
  if (process.env.UI_BIN) {
    return process.env.UI_BIN
  }

  const web = join(ROOT, 'apps/api/web')

  if (process.env.UI_SKIP_DASHBOARD !== '1') {
    await run('pnpm', ['turbo', 'run', 'build', '--filter=@staqly/platform'], { cwd: ROOT })

    for (const entry of readdirSync(web)) {
      if (entry !== '.gitkeep') {
        rmSync(join(web, entry), { recursive: true, force: true })
      }
    }

    cpSync(join(ROOT, 'apps/platform/out'), web, { recursive: true })
  }

  if (!existsSync(join(web, 'index.html'))) {
    throw new Error('apps/api/web has no dashboard export; unset UI_SKIP_DASHBOARD')
  }

  const out = join(mkdtempSync(join(tmpdir(), 'staqly-ui-bin-')), 'staqly')

  await run('go', ['build', '-o', out, './cmd/staqly'], { cwd: join(ROOT, 'apps/cli') })

  return out
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

/** Starts `staqly serve` with its own data directory and waits for /health. */
export async function startServer(
  bin: string,
  opts: {
    mode: 'local' | 'cloud'
    port: number
    dashboardPort: number
    env?: Record<string, string>
    dataDir?: string
  },
): Promise<RunningServer> {
  const dataDir = opts.dataDir ?? mkdtempSync(join(tmpdir(), `staqly-ui-${opts.mode}-`))

  mkdirSync(dataDir, { recursive: true })
  const api = `http://127.0.0.1:${opts.port}`
  const dash = `http://127.0.0.1:${opts.dashboardPort}`
  const logFile = `${dataDir}.log`
  const out = openSync(logFile, 'a')
  const proc = spawn(
    bin,
    ['serve', '--data', dataDir, '--port', String(opts.port), '--dashboard-port', String(opts.dashboardPort)],
    {
      env: {
        ...process.env,
        STAQLY_MODE: opts.mode,
        // Cloud without a Google client: the sign-in from before accounts (dev and tests only).
        AUTH_DEV_SIGNIN: 'true',
        // Test servers start every workspace on Pro so specs can invite people and make several keys.
        TRIAL_PLAN: 'pro',
        DATABASE_URL: opts.mode === 'cloud' ? cloudDatabaseURL('ui') : '',
        PUBLIC_API_URL: api,
        PUBLIC_DASHBOARD_URL: dash,
        WEBHOOK_ALLOW_LOOPBACK: 'true',
        NOTIFY_ALLOW_LOOPBACK: 'true',
        WEBHOOK_ALLOW_PRIVATE: 'true',
        WATCHER_ALLOW_PRIVATE: 'true',
        WEBHOOK_RETRY_BASE: '1s',
        COOKIE_SECURE: 'false',
        TAGGER_COMMAND: 'off',
        TAGGER_URL: '',
        STAQLY_API_KEY: '',
        // The token file the server makes in --data, as for people (tokenLink and localToken read it).
        STAQLY_LOCAL_TOKEN: '',
        LOG_LEVEL: 'warn',
        RATE_LIMIT_DISABLED: 'true',
        ...opts.env,
      },
      // A file, not a pipe: with UI_KEEP=1 the server outlives this process.
      stdio: ['ignore', out, out],
      detached: process.env.UI_KEEP === '1',
    },
  )

  closeSync(out)

  if (process.env.UI_KEEP === '1') {
    proc.unref()
  }

  const log = () => (existsSync(logFile) ? readFileSync(logFile, 'utf8') : '')
  const exited = new Promise<void>((r) => proc.once('exit', () => r()))

  for (let i = 0; ; i++) {
    if (proc.exitCode !== null) {
      throw new Error(`staqly serve (${opts.mode}) exited with ${proc.exitCode}:\n${log()}`)
    }

    try {
      if ((await fetch(`${api}/health`)).ok && (await fetch(`${dash}/`)).status < 500) {
        break
      }
    } catch {
      // not listening yet
    }

    if (i > 200) {
      proc.kill('SIGKILL')

      throw new Error(`staqly serve (${opts.mode}) did not become healthy on ${api}:\n${log()}`)
    }

    await sleep(100)
  }

  return {
    mode: opts.mode,
    api,
    dash,
    dataDir,
    pid: proc.pid!,
    token: opts.mode === 'local' ? localToken(dataDir) : undefined,
    proc,
    log,
    stop: async (signal: NodeJS.Signals = 'SIGTERM') => {
      if (proc.exitCode === null) {
        proc.kill(signal)
        await exited
      }
    },
  }
}

/** The access token a local server made on its first start (what `staqly serve token` prints). */
export function localToken(dataDir: string): string {
  return readFileSync(join(dataDir, 'access_token'), 'utf8').trim()
}

/** The sign-in link a local server prints at start: the dashboard with the token in the fragment. */
export function tokenLink(server: Pick<ServerInfo, 'dash' | 'dataDir'>, path = '/'): string {
  return `${server.dash}${path}#token=${localToken(server.dataDir)}`
}

/** A one-time dashboard sign-in link (`staqly serve connect-link`) for a local server. */
export async function connectLink(bin: string, server: ServerInfo): Promise<string> {
  const port = new URL(server.dash).port
  const out = await run(bin, ['serve', 'connect-link', '--data', server.dataDir, '--dashboard-port', port], {
    env: { ...process.env, PUBLIC_DASHBOARD_URL: server.dash },
  })
  const link = /https?:\/\/\S+/.exec(out)?.[0]

  if (!link) {
    throw new Error(`no link in connect-link output: ${out}`)
  }

  // The link names the dashboard host it was printed for; the browser uses 127.0.0.1.
  const url = new URL(link)

  return `${server.dash}${url.pathname}${url.search}${url.hash}`
}
