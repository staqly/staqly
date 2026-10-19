// HTTP core: auth, timeouts, retries with backoff, idempotency keys, error
// mapping. Uses only web-standard APIs (fetch, AbortController, crypto,
// FormData) so it runs on Node ≥20, Bun, Deno, browsers and edge runtimes.
import {
  APIConnectionError,
  type APIError,
  APIUserAbortError,
  makeAPIError,
  parseRetryAfter,
  StaqlyError,
  TimeoutError,
} from './errors.js'
import type { RequestOptions } from './types.js'
import { VERSION } from './version.js'

export const DEFAULT_BASE_URL = 'http://localhost:3333'
export const DEFAULT_TIMEOUT_MS = 60_000
export const DEFAULT_MAX_RETRIES = 2
/** A `Retry-After` longer than this is not waited for; the 429 is thrown instead. */
export const MAX_RETRY_AFTER_SECONDS = 60

export type FetchLike = (input: string | URL | Request, init?: RequestInit) => Promise<Response>

export interface ClientOptions {
  /**
   * API base URL. Defaults to the `STAQLY_API_URL` environment variable, then
   * `http://localhost:3333` (a local-mode server). Cloud: e.g. `https://staqly.example.com`.
   */
  baseUrl?: string
  /**
   * API key (`stk_...`) for cloud mode. Defaults to `STAQLY_API_KEY`. Omit it for
   * a local-mode server, which needs no credentials. Pass `null` to ignore the env var.
   */
  apiKey?: string | null
  /** Per-request timeout in milliseconds (default 60 000). */
  timeout?: number
  /** Retries for 408/429/5xx/network errors with exponential backoff and jitter (default 2). */
  maxRetries?: number
  /** Custom fetch implementation (tests, proxies, instrumentation). Defaults to the global fetch. */
  fetch?: FetchLike
  /** Headers sent with every request. */
  defaultHeaders?: Record<string, string>
  /** Appended to the User-Agent, e.g. `my-agent/1.2`. */
  userAgent?: string
}

type QueryValue = string | number | boolean | undefined | null

export type Query = Record<string, QueryValue>

export interface RequestSpec {
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  path: string
  query?: Query
  body?: unknown
  form?: FormData
  /** Send no Authorization header (public endpoints such as workspace signup). */
  noAuth?: boolean
  /** Return the raw Response instead of parsing JSON. */
  raw?: boolean
  options?: RequestOptions
}

function readEnv(name: string): string | undefined {
  try {
    const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process
    const value = proc?.env?.[name]

    if (value) {
      return value
    }

    const deno = (globalThis as { Deno?: { env?: { get(k: string): string | undefined } } }).Deno

    return deno?.env?.get(name) || undefined
  } catch {
    return undefined // e.g. Deno without --allow-env
  }
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      return reject(new APIUserAbortError())
    }

    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    const onAbort = () => {
      clearTimeout(timer)
      reject(new APIUserAbortError())
    }

    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

export { sleep as sleepWithSignal }

function randomId(): string {
  const c = (globalThis as { crypto?: Crypto }).crypto

  if (c?.randomUUID) {
    return c.randomUUID()
  }

  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`
}

const RETRYABLE_STATUS = new Set([408, 429, 500, 502, 503, 504])

/** Backoff for attempt n (0-based): 0.5s · 2^n capped at 8s, with ±25% jitter. */
export function backoffMs(attempt: number): number {
  const base = Math.min(500 * 2 ** attempt, 8_000)

  return base * (0.75 + Math.random() * 0.25)
}

const INSPECT = Symbol.for('nodejs.util.inspect.custom')

export class HTTPClient {
  readonly baseUrl: string
  // A true private field: not enumerable, not in JSON.stringify / console.log.
  readonly #apiKey: string | undefined
  readonly timeout: number
  readonly maxRetries: number
  private readonly fetchImpl: FetchLike
  private readonly defaultHeaders: Record<string, string>
  private readonly userAgent: string

  constructor(opts: ClientOptions = {}) {
    this.baseUrl = (opts.baseUrl ?? readEnv('STAQLY_API_URL') ?? DEFAULT_BASE_URL).replace(/\/+$/, '')
    this.#apiKey = opts.apiKey === null ? undefined : (opts.apiKey ?? readEnv('STAQLY_API_KEY'))
    this.timeout = opts.timeout ?? DEFAULT_TIMEOUT_MS
    this.maxRetries = Math.max(0, opts.maxRetries ?? DEFAULT_MAX_RETRIES)
    const f = opts.fetch ?? (globalThis.fetch as FetchLike | undefined)

    if (!f) {
      throw new StaqlyError('No fetch implementation found; pass `fetch` in the client options.')
    }

    this.fetchImpl = opts.fetch ? f : (input, init) => globalThis.fetch(input, init)
    this.defaultHeaders = { ...opts.defaultHeaders }
    this.userAgent = `staqly-sdk-ts/${VERSION}${opts.userAgent ? ` ${opts.userAgent}` : ''}`
  }

  /** The configured API key (read-only; redacted from inspection and JSON). */
  get apiKey(): string | undefined {
    return this.#apiKey
  }

  /** What `console.log` / `util.inspect` show: never the key. */
  [INSPECT](): Record<string, unknown> {
    return this.toJSON()
  }

  toJSON(): Record<string, unknown> {
    return {
      baseUrl: this.baseUrl,
      apiKey: this.#apiKey ? '***' : undefined,
      timeout: this.timeout,
      maxRetries: this.maxRetries,
    }
  }

  buildURL(path: string, query?: Query): string {
    const url = new URL(this.baseUrl + path)

    for (const [k, v] of Object.entries(query ?? {})) {
      if (v !== undefined && v !== null && v !== '') {
        url.searchParams.set(k, String(v))
      }
    }

    return url.toString()
  }

  async request<T>(spec: RequestSpec): Promise<T> {
    const opts = spec.options ?? {}
    const maxRetries = Math.max(0, opts.maxRetries ?? this.maxRetries)
    const timeout = opts.timeout ?? this.timeout
    const url = this.buildURL(spec.path, spec.query)
    const headers: Record<string, string> = {
      Accept: 'application/json',
      'User-Agent': this.userAgent,
      ...this.defaultHeaders,
    }

    if (this.#apiKey && !spec.noAuth) {
      headers.Authorization = `Bearer ${this.#apiKey}`
    }

    let body: BodyInit | undefined

    if (spec.form) {
      body = spec.form
    } else if (spec.body !== undefined) {
      headers['Content-Type'] = 'application/json'
      body = JSON.stringify(spec.body)
    }

    // Unsafe JSON requests carry an Idempotency-Key so a retry can never apply
    // twice; the server replays the first response for a repeated key. The
    // server does not cover multipart bodies, so uploads are only retried when
    // the server says it did not process them (429).
    const unsafe = spec.method !== 'GET'
    const idempotent = !unsafe || (!spec.form && (opts.idempotencyKey !== undefined || maxRetries > 0))

    if (unsafe && !spec.form && idempotent) {
      headers['Idempotency-Key'] = opts.idempotencyKey ?? `sdk-${randomId()}`
    }

    Object.assign(headers, opts.headers)

    for (let attempt = 0; ; attempt++) {
      const canRetry = attempt < maxRetries
      let res: Response
      const ctrl = new AbortController()
      const onAbort = () => ctrl.abort(opts.signal?.reason)

      if (opts.signal?.aborted) {
        throw new APIUserAbortError()
      }

      opts.signal?.addEventListener('abort', onAbort, { once: true })
      let timedOut = false
      const timer = setTimeout(() => {
        timedOut = true
        ctrl.abort()
      }, timeout)

      try {
        res = await this.fetchImpl(url, { method: spec.method, headers, body, signal: ctrl.signal })
      } catch (err) {
        clearTimeout(timer)
        opts.signal?.removeEventListener('abort', onAbort)

        if (opts.signal?.aborted) {
          throw new APIUserAbortError(undefined, { cause: err })
        }

        const failure = timedOut
          ? new TimeoutError(`Request timed out after ${timeout}ms: ${spec.method} ${spec.path}`, { cause: err })
          : new APIConnectionError(`Connection error: ${spec.method} ${spec.path}: ${describe(err)}`, { cause: err })

        if (canRetry && idempotent) {
          await sleep(backoffMs(attempt), opts.signal)
          continue
        }

        throw failure
      }

      let errBody: unknown
      // Raw (streamed) responses keep listening to opts.signal until the body
      // is consumed, so a download can still be aborted mid-stream.
      let keepAbortListener = false

      try {
        if (res.ok) {
          if (spec.raw) {
            if (res.body && opts.signal) {
              keepAbortListener = true

              return abortableUntilConsumed(res, () => opts.signal?.removeEventListener('abort', onAbort)) as T
            }

            return res as T
          }

          if (res.status === 204) {
            return undefined as T
          }

          const text = await res.text()

          if (!text) {
            return undefined as T
          }

          try {
            return JSON.parse(text) as T
          } catch (err) {
            throw new APIConnectionError(`Invalid JSON in ${res.status} response to ${spec.method} ${spec.path}`, {
              cause: err,
            })
          }
        }

        errBody = await readBody(res)
      } catch (err) {
        if (err instanceof StaqlyError) {
          throw err
        }

        if (opts.signal?.aborted) {
          throw new APIUserAbortError(undefined, { cause: err })
        }

        if (timedOut) {
          throw new TimeoutError(`Timed out reading the response to ${spec.method} ${spec.path}`, { cause: err })
        }

        throw new APIConnectionError(`Connection error: ${spec.method} ${spec.path}: ${describe(err)}`, { cause: err })
      } finally {
        clearTimeout(timer)

        if (!keepAbortListener) {
          opts.signal?.removeEventListener('abort', onAbort)
        }
      }

      const error = makeAPIError(res.status, res.headers, errBody)
      const retryAfter = parseRetryAfter(res.headers.get('retry-after'))

      if (canRetry && shouldRetry(error, idempotent) && (retryAfter ?? 0) <= MAX_RETRY_AFTER_SECONDS) {
        await sleep(retryAfter !== undefined ? retryAfter * 1000 : backoffMs(attempt), opts.signal)
        continue
      }

      throw error
    }
  }
}

/**
 * The same response, but `cleanup` runs once its body has been read to the
 * end, failed (e.g. aborted via the request controller) or was cancelled.
 */
function abortableUntilConsumed(res: Response, cleanup: () => void): Response {
  let done = false
  const finish = () => {
    if (!done) {
      done = true
      cleanup()
    }
  }
  const reader = res.body!.getReader()
  const relay = new ReadableStream<Uint8Array>({
    async pull(controller) {
      try {
        const chunk = await reader.read()

        if (chunk.done) {
          finish()
          controller.close()
        } else {
          controller.enqueue(chunk.value)
        }
      } catch (err) {
        finish()
        controller.error(err)
      }
    },
    cancel(reason) {
      finish()

      return reader.cancel(reason)
    },
  })
  const out = new Response(relay, { status: res.status, statusText: res.statusText, headers: res.headers })

  Object.defineProperty(out, 'url', { value: res.url })

  return out
}

function shouldRetry(error: APIError, idempotent: boolean): boolean {
  if (error.status === 429) {
    return true
  } // not processed: always safe

  if (!idempotent) {
    return false
  }

  if (error.status === 409 && error.code === 'idempotency_in_progress') {
    return true
  }

  return RETRYABLE_STATUS.has(error.status)
}

async function readBody(res: Response): Promise<unknown> {
  const text = await res.text()

  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

function describe(err: unknown): string {
  if (err instanceof Error) {
    const cause = (err as { cause?: unknown }).cause

    return cause instanceof Error ? `${err.message} (${cause.message})` : err.message
  }

  return String(err)
}
