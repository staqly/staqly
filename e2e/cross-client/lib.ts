// Helpers of the cross-client suite: real servers (`staqly serve` from this
// checkout), and every way a user talks to them — the TypeScript SDK, the
// Python SDK (sync and async, through py_worker.py), the staqly CLI, curl and
// MCP over Streamable HTTP. No mocks: everything goes over loopback HTTP.
import { type ChildProcess, execFile, spawn } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { createServer, type IncomingHttpHeaders, type Server as HttpServer } from 'node:http'
import type { AddressInfo } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { createInterface } from 'node:readline'
import { fileURLToPath } from 'node:url'

import { SCOPES, Staqly, type ClientOptions } from '@staqly/sdk'

export const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

// ---- servers ----

export interface ServerInfo {
  url: string
  mode: 'local' | 'cloud'
  dataDir: string
  /** Cloud mode: the PostgreSQL database (with its own schema); '' in local mode. */
  databaseURL: string
  pid: number
}

export interface RunningServer extends ServerInfo {
  proc: ChildProcess
  log: () => string
  stop: (signal?: NodeJS.Signals) => Promise<void>
}

/** Builds the staqly CLI (it also serves) from this checkout, unless XC_BIN points at one. */
export async function buildBinary(): Promise<string> {
  if (process.env.XC_BIN) {
    return process.env.XC_BIN
  }

  const out = join(mkdtempSync(join(tmpdir(), 'staqly-xc-bin-')), 'staqly')

  await run('go', ['build', '-o', out, './cmd/staqly'], { cwd: join(ROOT, 'apps/cli'), timeout: 600_000 })

  return out
}

/**
 * Cloud mode runs on PostgreSQL: STAQLY_E2E_DATABASE_URL points at a server (CI: a service container;
 * locally e.g. `docker run -d -p 55432:5432 -e POSTGRES_PASSWORD=staqly postgres:17.6-alpine`). Each
 * cloud server gets its own schema (search_path), which staqly creates.
 */
export function cloudDatabaseURL(): string {
  const base = process.env.STAQLY_E2E_DATABASE_URL

  if (!base) {
    throw new Error('cloud mode needs PostgreSQL: set STAQLY_E2E_DATABASE_URL=postgres://... (see e2e/README.md)')
  }

  const url = new URL(base)

  url.searchParams.set('search_path', `xc_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`)

  return url.toString()
}

/** Starts `staqly serve` on 127.0.0.1:port with its own temporary data directory. */
export async function startServer(
  bin: string,
  opts: { mode: 'local' | 'cloud'; port: number; dashboardPort: number; env?: Record<string, string> },
): Promise<RunningServer> {
  const dataDir = mkdtempSync(join(tmpdir(), `staqly-xc-${opts.mode}-`))
  const databaseURL = opts.mode === 'cloud' ? cloudDatabaseURL() : ''
  const url = `http://127.0.0.1:${opts.port}`
  let log = ''
  const proc = spawn(
    bin,
    ['serve', '--data', dataDir, '--port', String(opts.port), '--dashboard-port', String(opts.dashboardPort)],
    {
      env: {
        ...process.env,
        STAQLY_MODE: opts.mode,
        // Cloud without a Google client: the sign-in from before accounts (dev and tests only).
        AUTH_DEV_SIGNIN: 'true',
        // New workspaces start on Pro so suites can make several keys and export; the Starter trial test
        // puts its own workspace on Starter with the operator command (operator() below).
        TRIAL_PLAN: 'pro',
        PUBLIC_API_URL: url,
        WEBHOOK_ALLOW_LOOPBACK: 'true',
        NOTIFY_ALLOW_LOOPBACK: 'true',
        WEBHOOK_ALLOW_PRIVATE: 'true',
        WEBHOOK_RETRY_BASE: '1s',
        COOKIE_SECURE: 'false',
        STAQLY_API_KEY: '',
        LOG_LEVEL: 'warn',
        RATE_LIMIT_DISABLED: 'true',
        DATABASE_URL: databaseURL,
        ...opts.env,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    },
  )

  proc.stdout?.on('data', (d: Buffer) => (log += d.toString()))
  proc.stderr?.on('data', (d: Buffer) => (log += d.toString()))
  const exited = new Promise<void>((r) => proc.once('exit', () => r()))

  for (let i = 0; ; i++) {
    if (proc.exitCode !== null) {
      throw new Error(`staqly serve (${opts.mode}) exited with ${proc.exitCode}:\n${log}`)
    }

    try {
      if ((await fetch(`${url}/health`)).ok) {
        break
      }
    } catch {
      // not listening yet
    }

    if (i > 150) {
      proc.kill('SIGKILL')

      throw new Error(`staqly serve (${opts.mode}) did not become healthy on ${url}:\n${log}`)
    }

    await sleep(100)
  }

  return {
    url,
    mode: opts.mode,
    dataDir,
    databaseURL,
    pid: proc.pid!,
    proc,
    log: () => log,
    stop: async (signal: NodeJS.Signals = 'SIGTERM') => {
      if (proc.exitCode === null) {
        proc.kill(signal)
        await exited
      }

      rmSync(dataDir, { recursive: true, force: true })
    },
  }
}

export const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

export function sdk(url: string, apiKey: string | null, opts: Omit<ClientOptions, 'baseUrl' | 'apiKey'> = {}): Staqly {
  return new Staqly({ baseUrl: url, apiKey, ...opts })
}

export interface CloudWorkspace {
  workspaceId: string
  /** The signup key: scope `admin`. */
  adminKey: string
  /** Every scope except admin, the way an agent is set up. */
  agentKey: string
}

export interface SignupResponse {
  api_key: { key: string }
  recovery_key: string
  csrf_token: string
  workspace: { id: string; name: string }
}

/**
 * POST /v1/workspaces. Signups are capped at 5 per hour per client address (also with
 * RATE_LIMIT_DISABLED), so the cloud servers trust X-Forwarded-For (TRUST_PROXY, as behind a
 * proxy) and each signup comes from its own documentation address (198.51.100.0/24).
 */
export async function signupRaw(url: string, name = `xc-${Date.now().toString(36)}`): Promise<SignupResponse> {
  const res = await fetch(`${url}/v1/workspaces`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Forwarded-For': `198.51.100.${1 + Math.floor(Math.random() * 254)}`,
    },
    body: JSON.stringify({ name }),
  })

  if (res.status !== 201) {
    throw new Error(`signup: ${res.status} ${await res.text()}`)
  }

  return (await res.json()) as SignupResponse
}

/** Signs a new cloud workspace up (Pro trial) and mints an agent key. */
export async function signup(url: string, name?: string): Promise<CloudWorkspace> {
  const ws = await signupRaw(url, name)
  const admin = sdk(url, ws.api_key.key)
  const agent = await admin.apiKeys.create({ name: 'xc agent', scopes: SCOPES.filter((s) => s !== 'admin') })

  return { workspaceId: ws.workspace.id, adminKey: ws.api_key.key, agentKey: agent.key! }
}

// ---- processes ----

/** An operator command (`staqly serve <args>`) against a running server's data directory and database. */
export function operator(bin: string, server: ServerInfo, args: string[]) {
  return run(bin, ['serve', '--data', server.dataDir, ...args], {
    env: {
      PATH: process.env.PATH ?? '',
      STAQLY_MODE: server.mode,
      AUTH_DEV_SIGNIN: 'true',
      DATABASE_URL: server.databaseURL,
      LOG_LEVEL: 'error',
    },
  })
}

export interface RunResult {
  code: number
  out: string
  err: string
}

export function run(
  cmd: string,
  args: string[],
  opts: { cwd?: string; env?: NodeJS.ProcessEnv; input?: string; timeout?: number } = {},
): Promise<RunResult> {
  return new Promise((resolvePromise, reject) => {
    const child = execFile(
      cmd,
      args,
      { cwd: opts.cwd, env: opts.env ?? process.env, timeout: opts.timeout ?? 120_000, maxBuffer: 64 << 20 },
      (err, stdout, stderr) => {
        const code = err ? (typeof err.code === 'number' ? err.code : -1) : 0

        if (err && typeof err.code !== 'number') {
          reject(new Error(`${cmd} ${args.join(' ')}: ${err.message}\n${stderr}`))

          return
        }

        resolvePromise({ code, out: String(stdout), err: String(stderr) })
      },
    )

    if (opts.input !== undefined) {
      child.stdin?.end(opts.input)
    } else {
      child.stdin?.end()
    }
  })
}

// ---- CLI ----

export interface CLIResult extends RunResult {
  json<T = Record<string, unknown>>(): T
}

/** Runs the staqly CLI with an isolated config file and no inherited credentials. */
export function cliRunner(
  bin: string,
  url: string,
  key = '',
  configDir = mkdtempSync(join(tmpdir(), 'staqly-xc-cli-')),
) {
  return async (args: string[], opts: { input?: string; env?: Record<string, string> } = {}): Promise<CLIResult> => {
    const res = await run(bin, args, {
      env: {
        PATH: process.env.PATH,
        HOME: configDir,
        STAQLY_CONFIG: join(configDir, 'config.json'),
        STAQLY_API_URL: url,
        STAQLY_API_KEY: key,
        NO_COLOR: '1',
        ...opts.env,
      },
      input: opts.input,
    })

    return { ...res, json: <T>() => JSON.parse(res.out) as T }
  }
}

// ---- curl ----

export interface CurlResult {
  status: number
  headers: Record<string, string>
  body: string
  json<T = Record<string, unknown>>(): T
}

/** One request with the real curl binary (`-F name=@file` style multipart via `form`). */
export async function curl(
  url: string,
  init: {
    method?: string
    key?: string
    json?: unknown
    headers?: Record<string, string>
    form?: string[]
    output?: string
  } = {},
): Promise<CurlResult> {
  const args = ['-sS', '-D', '-', '-X', init.method ?? (init.json !== undefined || init.form ? 'POST' : 'GET')]

  if (init.key) {
    args.push('-H', `Authorization: Bearer ${init.key}`)
  }

  for (const [k, v] of Object.entries(init.headers ?? {})) {
    args.push('-H', `${k}: ${v}`)
  }

  if (init.json !== undefined) {
    args.push('-H', 'Content-Type: application/json', '--data-binary', JSON.stringify(init.json))
  }

  for (const f of init.form ?? []) {
    args.push('-F', f)
  }

  if (init.output) {
    args.push('-o', init.output)
  }

  args.push(url)
  const res = await run('curl', args)

  if (res.code !== 0) {
    throw new Error(`curl ${args.join(' ')} failed (${res.code}): ${res.err}`)
  }

  // Header blocks (100 Continue first, if any), then the body.
  let rest = res.out
  let status = 0
  const headers: Record<string, string> = {}

  while (rest.startsWith('HTTP/')) {
    const end = rest.indexOf('\r\n\r\n')
    const block = rest.slice(0, end).split('\r\n')

    rest = rest.slice(end + 4)
    status = Number(block[0]!.split(' ')[1])

    if (status >= 200) {
      for (const line of block.slice(1)) {
        const i = line.indexOf(':')

        headers[line.slice(0, i).toLowerCase()] = line.slice(i + 1).trim()
      }

      break
    }
  }

  return { status, headers, body: rest, json: <T>() => JSON.parse(rest) as T }
}

// ---- MCP (Streamable HTTP, JSON-RPC) ----

export class MCPToolError extends Error {
  constructor(
    readonly tool: string,
    readonly text: string,
  ) {
    super(`${tool}: ${text}`)
  }
}

let rpcId = 1

/** Calls one MCP tool; resolves with its structured result, rejects with MCPToolError when isError. */
export async function mcp<T = Record<string, unknown>>(
  url: string,
  key: string | null,
  tool: string,
  args: Record<string, unknown> = {},
): Promise<T> {
  const res = await fetch(`${url}/mcp`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      'User-Agent': 'xc-mcp-client/1',
      ...(key ? { Authorization: `Bearer ${key}` } : {}),
    },
    body: JSON.stringify({
      jsonrpc: '2.0',
      id: rpcId++,
      method: 'tools/call',
      params: { name: tool, arguments: args },
    }),
  })
  const text = await res.text()

  if (!res.ok) {
    throw new Error(`MCP ${tool}: HTTP ${res.status} ${text}`)
  }

  const data = res.headers.get('content-type')?.includes('text/event-stream')
    ? text
        .split('\n')
        .filter((l) => l.startsWith('data:'))
        .map((l) => l.slice(5).trim())
        .pop()!
    : text
  const answer = JSON.parse(data) as {
    result?: { isError?: boolean; structuredContent?: T; content?: { text?: string }[] }
    error?: { message: string }
  }

  if (answer.error) {
    throw new Error(`MCP ${tool}: ${answer.error.message}`)
  }

  if (answer.result?.isError) {
    throw new MCPToolError(tool, answer.result.content?.map((c) => c.text).join(' ') ?? '')
  }

  return answer.result!.structuredContent as T
}

// ---- Python SDK ----

export interface PyError {
  type: string
  mro: string[]
  message: string
  status?: number
  code?: string
  retry_after?: number | null
  limit?: string
  max?: number
  plan?: string
  traceback?: string
}

export class PyCallError extends Error {
  constructor(readonly error: PyError) {
    super(`${error.type}: ${error.message}${error.traceback ? `\n${error.traceback}` : ''}`)
  }
}

export interface PyClient {
  base_url: string
  api_key: string
  max_retries?: number
  timeout?: number
  drop_first_response?: boolean
  record?: boolean
  record_key?: string
  default_headers?: Record<string, string>
}

/** The Python SDK behind py_worker.py: `call('sync' | 'async', client, 'posts.create', args, kwargs)`. */
export class PythonSDK {
  private child?: ChildProcess
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>()
  private next = 1
  version = ''

  async start(): Promise<void> {
    this.child = spawn(
      'uv',
      ['run', '--quiet', '--project', join(ROOT, 'sdks/python'), 'python', join(ROOT, 'e2e/cross-client/py_worker.py')],
      { stdio: ['pipe', 'pipe', 'pipe'], env: { ...process.env, STAQLY_API_KEY: '', STAQLY_API_URL: '' } },
    )
    let stderr = ''

    this.child.stderr!.on('data', (d: Buffer) => (stderr += d.toString()))
    const ready = new Promise<void>((resolveReady, rejectReady) => {
      this.child!.once('exit', (code) => {
        const err = new Error(`py_worker exited ${code}: ${stderr}`)

        rejectReady(err)

        for (const p of this.pending.values()) {
          p.reject(err)
        }

        this.pending.clear()
      })
      createInterface({ input: this.child!.stdout! }).on('line', (line) => {
        const msg = JSON.parse(line) as {
          ready?: boolean
          version?: string
          id?: number
          ok?: boolean
          result?: unknown
          error?: PyError
        }

        if (msg.ready) {
          this.version = msg.version ?? ''
          resolveReady()

          return
        }

        const p = this.pending.get(msg.id!)

        this.pending.delete(msg.id!)

        if (msg.ok) {
          p?.resolve(msg.result)
        } else {
          p?.reject(new PyCallError(msg.error!))
        }
      })
    })

    await ready
  }

  call<T = unknown>(
    mode: 'sync' | 'async',
    client: PyClient,
    call: string,
    args: unknown[] = [],
    kwargs: Record<string, unknown> = {},
  ): Promise<T> {
    const id = this.next++

    return new Promise<T>((resolveCall, rejectCall) => {
      this.pending.set(id, { resolve: resolveCall as (v: unknown) => void, reject: rejectCall })
      this.child!.stdin!.write(`${JSON.stringify({ id, mode, client, call, args, kwargs })}\n`)
    })
  }

  /** Resolves with the Python error of a call that must fail. */
  async fail(...a: Parameters<PythonSDK['call']>): Promise<PyError> {
    try {
      await this.call(...a)
    } catch (err) {
      if (err instanceof PyCallError) {
        return err.error
      }

      throw err
    }

    throw new Error(`python ${a[2]} did not fail`)
  }

  stop(): void {
    this.child?.stdin?.end()
    this.child?.kill()
  }
}

export const b64 = (data: Uint8Array) => ({ $bytes: Buffer.from(data).toString('base64') })

// ---- webhook receiver ----

export interface Received {
  headers: IncomingHttpHeaders
  body: string
  at: number
}

export async function receiver(): Promise<{
  url: string
  received: Received[]
  close: () => void
  server: HttpServer
}> {
  const received: Received[] = []
  const server = createServer((req, res) => {
    const chunks: Buffer[] = []

    req.on('data', (d: Buffer) => chunks.push(d))
    req.on('end', () => {
      received.push({ headers: req.headers, body: Buffer.concat(chunks).toString('utf8'), at: Date.now() })
      res.writeHead(200).end('ok')
    })
  })

  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))

  return {
    url: `http://127.0.0.1:${(server.address() as AddressInfo).port}/hook`,
    received,
    close: () => server.close(),
    server,
  }
}

/** Polls `fn` until it returns a truthy value (or throws after `timeout` ms). */
export async function until<T>(
  fn: () => Promise<T | undefined | false> | T | undefined | false,
  timeout = 15_000,
): Promise<T> {
  const deadline = Date.now() + timeout

  for (;;) {
    const v = await fn()

    if (v) {
      return v
    }

    if (Date.now() > deadline) {
      throw new Error('until: timed out')
    }

    await sleep(100)
  }
}

/** Removes what legitimately differs between two equal operations (IDs, times, the client). */
export function normalize(v: unknown): unknown {
  const drop = new Set([
    'id',
    'post_id',
    'collection_id',
    'created_at',
    'updated_at',
    'published_at',
    'seq',
    'agent',
    // MCP's create_post takes no source (the server names the agent instead): the REST clients send 'xc'
    'source',
    'url',
    'sha256',
  ])

  if (Array.isArray(v)) {
    return v.map(normalize)
  }

  if (v && typeof v === 'object') {
    return Object.fromEntries(
      Object.entries(v as Record<string, unknown>)
        .filter(([k]) => !drop.has(k))
        .map(([k, x]) => [k, normalize(x)]),
    )
  }

  return v
}
