// REST clients for the Docker suite: a thin typed wrapper over raw HTTP (no
// SDK, so this suite tests the server's contract itself, and keeps working
// while the SDKs are rewritten), a signed-in browser session and unique names.
import { createHmac } from 'node:crypto'
import { readFileSync } from 'node:fs'

import { type HttpInit, type HttpResponse, http, Session } from './http.js'
import { API, DASHBOARD, LOCAL_TOKEN, ROOT, server } from './stack.js'

export interface Collection {
  id: string
  name: string
  archived: boolean
  processing_timeout_seconds: number | null
  post_count: number
  status_counts: Record<string, number>
}

export interface Channel {
  id: string
  provider: string
  name: string
  handle: string
  status: 'connected' | 'needs_reconnect' | 'disabled'
  last_error: string
}

export interface Target {
  channel_id: string
  channel_name: string
  provider: string
  content: string | null
  settings: Record<string, string>
  status: 'pending' | 'publishing' | 'published' | 'failed'
  external_id: string
  external_url: string
  error: string
  attempts: number
  published_at: string | null
}

export interface Post {
  id: string
  collection_id: string
  collection_name: string
  content: string
  media: string[]
  media_items: { id: string; mime_type: string; size_bytes: number; original_name: string }[]
  status: 'draft' | 'scheduled' | 'publishing' | 'published' | 'failed'
  publish_at: string | null
  published_at: string | null
  tags: string[]
  metadata: Record<string, unknown>
  source: string
  external_ref: string
  version: number
  targets: Target[]
  created_at: string
  updated_at: string
}

export interface Schedule {
  id: string
  collection_id: string
  name: string
  channels: string[]
  kind: 'once' | 'recurring'
  run_at: string | null
  rule: Record<string, unknown>
  pick: 'next' | 'selected' | 'all'
  post_ids: string[]
  source: 'none' | 'agent' | 'endpoint'
  endpoint_url: string
  enabled: boolean
  next_run_at: string | null
  last_run_at: string | null
  last_result: string
}

export interface Job {
  id: string
  kind: string
  key: string
  status: 'queued' | 'running' | 'done' | 'failed' | 'cancelled'
  run_at: string
  attempts: number
  max_attempts: number
  last_error: string
  payload: { post_id?: string; channel_id?: string; schedule_id?: string }
}

export interface Event {
  id: string
  seq: number
  type: string
  collection_id: string | null
  post_id: string | null
  channel_id?: string | null
  schedule_id?: string | null
  actor: { type: string; id: string; name: string }
  data: Record<string, unknown>
  created_at: string
}

export interface Webhook {
  id: string
  url: string
  events: string[]
  secret: string
  active: boolean
}

export interface Delivery {
  id: string
  webhook_id: string
  event_type: string
  status: 'pending' | 'succeeded' | 'failed'
  attempts: number
  last_status_code: number | null
  last_error: string
  next_attempt_at: string | null
}

export interface Alert {
  id: string
  name: string
  type: string
  enabled: boolean
  events: string[]
  disabled_reason: string
  fail_count: number
}

export interface AlertDelivery {
  id: string
  status: string
  attempts: number
}

export interface Media {
  id: string
  mime_type: string
  size_bytes: number
  original_name: string
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly response: HttpResponse,
  ) {
    super(`${status} ${code}: ${message}`)
  }
}

/**
 * REST client as a local agent uses it (the access token) or with an API key in
 * cloud mode (null: no credentials). `req` throws ApiError on any non-2xx answer;
 * `raw` never throws.
 */
export class Api {
  constructor(
    readonly base = API,
    readonly key: string | null = LOCAL_TOKEN,
  ) {}

  /** Sends the request; like the SDK it waits out 429 answers (Retry-After) so long tests stay under the rate limit. */
  async raw(path: string, init: HttpInit = {}): Promise<HttpResponse> {
    const headers = { ...init.headers }

    if (this.key) {
      headers.Authorization = `Bearer ${this.key}`
    }

    for (let attempt = 0; ; attempt++) {
      const res = await http(`${this.base}${path}`, { ...init, headers })

      if (res.status !== 429 || attempt >= 10) {
        return res
      }

      await new Promise((r) => setTimeout(r, Number(res.headers['retry-after'] ?? '1') * 1000))
    }
  }

  async req<T = Record<string, unknown>>(path: string, init: HttpInit = {}): Promise<T> {
    const res = await this.raw(path, init)

    if (res.status < 200 || res.status > 299) {
      const err = res.text() ? (res.json<{ error?: { code?: string; message?: string } }>().error ?? {}) : {}

      throw new ApiError(res.status, err.code ?? 'error', err.message ?? res.text(), res)
    }

    return (res.text() ? res.json<T>() : {}) as T
  }

  get = <T = Record<string, unknown>>(path: string) => this.req<T>(path)
  post = <T = Record<string, unknown>>(path: string, json: unknown = {}) => this.req<T>(path, { method: 'POST', json })
  patch = <T = Record<string, unknown>>(path: string, json: unknown) => this.req<T>(path, { method: 'PATCH', json })
  del = (path: string) => this.req(path, { method: 'DELETE' })

  // ---- the things every test file needs ----

  collection(body: Record<string, unknown>): Promise<Collection> {
    return this.post<Collection>('/v1/collections', body)
  }

  /** A channel on "your own automation" (provider custom): staqly asks a webhook or an agent to publish. */
  customChannel(name?: string): Promise<Channel> {
    return this.post<Channel>('/v1/channels', { provider: 'custom', ...(name ? { name } : {}) })
  }

  createPost(collection: string, body: Record<string, unknown>): Promise<Post> {
    return this.post<Post>('/v1/posts', { collection, ...body })
  }

  getPost(id: string): Promise<Post> {
    return this.get<Post>(`/v1/posts/${id}`)
  }

  async listPosts(query: Record<string, string | number> = {}): Promise<Post[]> {
    const qs = new URLSearchParams(Object.entries(query).map(([k, v]) => [k, String(v)]))

    return (await this.get<{ data: Post[] }>(`/v1/posts?${qs}`)).data
  }

  publish(id: string): Promise<Post> {
    return this.post<Post>(`/v1/posts/${id}/publish`)
  }

  schedule(id: string, publishAt: string): Promise<Post> {
    return this.post<Post>(`/v1/posts/${id}/schedule`, { publish_at: publishAt })
  }

  /** What your automation reports for a custom target. */
  result(postId: string, channelId: string, body: Record<string, unknown>): Promise<Post> {
    return this.post<Post>(`/v1/posts/${postId}/targets/${channelId}/result`, body)
  }

  retryTarget(postId: string, channelId: string): Promise<Post> {
    return this.post<Post>(`/v1/posts/${postId}/targets/${channelId}/retry`)
  }

  async history(postId: string): Promise<Event[]> {
    return (await this.get<{ data: Event[] }>(`/v1/posts/${postId}/events?limit=100`)).data
  }

  createSchedule(body: Record<string, unknown>): Promise<Schedule> {
    return this.post<Schedule>('/v1/schedules', body)
  }

  runSchedule(id: string): Promise<Schedule> {
    return this.post<Schedule>(`/v1/schedules/${id}/run`)
  }

  async jobs(query: Record<string, string> = {}): Promise<Job[]> {
    return (await this.get<{ data: Job[] }>(`/v1/jobs?${new URLSearchParams(query)}`)).data
  }

  webhook(body: Record<string, unknown>): Promise<Webhook> {
    return this.post<Webhook>('/v1/webhooks', body)
  }

  async deliveries(query: Record<string, string>): Promise<Delivery[]> {
    return (await this.get<{ data: Delivery[] }>(`/v1/webhook-deliveries?${new URLSearchParams(query)}`)).data
  }

  alert(body: Record<string, unknown>): Promise<Alert> {
    return this.post<Alert>('/v1/alerts', body)
  }

  async alertDeliveries(id: string): Promise<AlertDelivery[]> {
    return (await this.get<{ data: AlertDelivery[] }>(`/v1/alerts/${id}/deliveries`)).data
  }

  async upload(bytes: Buffer, name: string, type = 'image/png'): Promise<Media> {
    const form = new FormData()

    form.set('file', new Blob([new Uint8Array(bytes)], { type }), name)

    return this.req<Media>('/v1/media', { method: 'POST', form })
  }

  /** The /v1/setup document (mode, initialized): public, sent without credentials (in cloud mode the local token is no key). */
  setup(): Promise<{ mode: string; initialized: boolean }> {
    return new Api(this.base, null).get<{ mode: string; initialized: boolean }>('/v1/setup')
  }
}

/** Local client with the access token, or one with an API key (cloud mode; null: no credentials). */
export function api(key: string | null = LOCAL_TOKEN): Api {
  return new Api(API, key)
}

/** A fixture file's bytes (e2e/fixtures). */
export function fixture(name: string): Buffer {
  return readFileSync(`${ROOT}e2e/fixtures/${name}`)
}

/**
 * Signs a browser in to the dashboard the way `make up` does: the access token
 * (the printed #token= link) traded for a session through the dashboard's /api proxy.
 */
export async function signInBrowser(token = LOCAL_TOKEN): Promise<Session> {
  const browser = new Session(DASHBOARD, { Origin: DASHBOARD })
  const res = await browser.signInWithToken(token)

  if (res.status !== 201) {
    throw new Error(`signing in with the access token failed: ${res.status} ${res.text()}`)
  }

  return browser
}

/** Signs a browser in with a one-time link from `server connect-link` (the phone path). */
export async function signInWithConnectLink(): Promise<Session> {
  const link = await server('connect-link')
  const token = link.slice(link.indexOf('#') + 1)
  const browser = new Session(DASHBOARD, { Origin: DASHBOARD })
  const res = await browser.redeemDeviceLink(token)

  if (res.status !== 201) {
    throw new Error(`redeeming the connect link failed: ${res.status} ${res.text()}`)
  }

  return browser
}

/** Unique names, so every test file creates its own data. */
export function unique(prefix: string): string {
  return `${prefix} ${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
}

/** The HTTP status a client call ends with (200 for any success). */
export async function statusOf(promise: Promise<unknown>): Promise<number> {
  try {
    await promise

    return 200
  } catch (err) {
    if (err instanceof ApiError) {
      return err.status
    }

    throw err
  }
}

/** The signature staqly puts on webhooks and schedule endpoint calls: HMAC-SHA256 over "<ts>.<body>". */
export function sign(secret: string, ts: string | number, body: string): string {
  return `sha256=${createHmac('sha256', secret).update(`${ts}.${body}`).digest('hex')}`
}

/** Verifies a received request the way an SDK does: signature over "<ts>.<body>", timestamp within tolerance. */
export function isValidSignature(
  secret: string,
  body: string,
  headers: Record<string, string | string[] | undefined>,
  opts: { now?: number; tolerance?: number } = {},
): boolean {
  const ts = String(headers['x-staqly-timestamp'] ?? '')
  const given = String(headers['x-staqly-signature'] ?? '')
  const now = opts.now ?? Date.now() / 1000

  if (!ts || !given || Math.abs(now - Number(ts)) > (opts.tolerance ?? 300)) {
    return false
  }

  return given === sign(secret, ts, body)
}

/** Readable post title as staqly makes it: the first line of the content. */
export function firstLine(content: string): string {
  return content.split('\n')[0]!.trim()
}
