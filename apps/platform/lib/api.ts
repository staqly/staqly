import type { components, paths } from './api-types'

type Schemas = components['schemas']

export type Workspace = Schemas['Workspace']
// The OpenAPI spec lags the server on a few fields (notification channels
// limit/usage, the channels:manage scope, escalate's channel_id); they are
// added here as optional so older servers still type-check.
export type Plan = Omit<Schemas['Plan'], 'limits'> & {
  limits: Schemas['Plan']['limits'] & { notification_channels?: number }
}
export type PlanId = Plan['id']
export type PlanLimits = Plan['limits']
export type APIKey = Schemas['APIKey']
export type Scope = Schemas['Scope']
export type Session = Schemas['Session']
export type Collection = Schemas['Collection']
export type CollectionInput = Schemas['CollectionInput']
export type Post = Schemas['Post']
export type PostInput = Schemas['PostInput']
export type PostPatch = Schemas['PostPatch']
export type PostTarget = Schemas['PostTarget']
export type PostTargetInput = Schemas['PostTargetInput']
export type PostPage = Schemas['PostPage']
export type PostStatus = Schemas['PostStatus']
export type TargetStatus = Schemas['TargetStatus']
export type PostPreview = Schemas['PostPreview']
/** What testing a channel's connection says (`POST /v1/channels/{id}/test`). */
export interface ChannelTestResult {
  ok: boolean
  error?: string
  profile?: { handle?: string; name?: string; avatar_url?: string; profile_url?: string }
}

export type Channel = Schemas['Channel']
export type ChannelInput = Schemas['ChannelInput']
export type ChannelPatch = Schemas['ChannelPatch']
export type ChannelStatus = Schemas['ChannelStatus']
export type Provider = Schemas['Provider']
export type ProviderField = Schemas['ProviderField']
export type ProviderLimits = Schemas['ProviderLimits']
export type DeveloperAppInput = Schemas['DeveloperAppInput']
export type Schedule = Schemas['Schedule']
export type ScheduleInput = Schemas['ScheduleInput']
export type ScheduleRule = Schemas['ScheduleRule']
export type WorkItem = Schemas['WorkItem']
export type Job = Schemas['Job']
export type JobPage = Schemas['JobPage']
export type JobStats = Schemas['JobStats']
export type Media = Schemas['Media']
export type StashEvent = Schemas['Event']
export type EventPage = Schemas['EventPage']
export type Webhook = Schemas['Webhook']
export type WebhookInput = Schemas['WebhookInput']
export type Delivery = Schemas['Delivery']
export type ErrorBody = Schemas['ErrorBody']
// Alerts: where staqly tells people what happened (Slack, Telegram, Discord, ntfy, email).
export type Alert = Schemas['Alert']
export type AlertInput = Omit<Schemas['AlertInput'], 'config'> & {
  /** `null` removes a key; a masked secret (as returned by GET) keeps the stored one. */
  config?: { [K in keyof Schemas['AlertConfig']]?: Schemas['AlertConfig'][K] | null }
}
export type AlertType = Alert['type']
export type AlertDelivery = Schemas['AlertDelivery']
export type AlertTestResult = JSONOf<'/v1/alerts/{id}/test', 'post', 200>
export type WorkspaceExport = Schemas['WorkspaceExport']
export type Readiness = Schemas['Readiness']

type JSONOf<P extends keyof paths, M extends keyof paths[P], S extends number> = paths[P][M] extends {
  responses: infer R
}
  ? R extends Record<S, { content: { 'application/json': infer B } }>
    ? B
    : never
  : never

type QueryOf<P extends keyof paths, M extends keyof paths[P]> = paths[P][M] extends {
  parameters: { query?: infer Q }
}
  ? NonNullable<Q>
  : never

/** "local": single-user, no credentials on localhost. "cloud": hosted, many workspaces, sign-in with an admin API key. */
export type Mode = 'local' | 'cloud'
// Older servers did not report mode / signup_open, so keep them optional for the UI.
export type SetupStatus = Omit<JSONOf<'/v1/setup', 'get', 200>, 'mode' | 'signup_open'> & {
  mode?: Mode
  signup_open?: boolean
}
export type SetupResult = JSONOf<'/v1/workspaces', 'post', 201>
/** Recovery: a session plus a new admin API key and the next recovery key (the used one stops working). */
export type RecoverResult = JSONOf<'/v1/sessions/recover', 'post', 201>
export type Agent = Schemas['Agent']
export type SessionInfo = Omit<JSONOf<'/v1/session', 'get', 200>, 'mode'> & { mode?: Mode }
type RawUsage = JSONOf<'/v1/workspace/usage', 'get', 200>

export type WorkspaceUsage = Omit<RawUsage, 'plan' | 'usage'> & {
  plan: Plan
  usage: RawUsage['usage'] & { notification_channels?: number }
}
export type DeviceLink = JSONOf<'/v1/device-links', 'post', 201>
export type Billing = JSONOf<'/v1/billing', 'get', 200>

export type LimitKey = keyof PlanLimits
export const UNLIMITED = -1

/** Details of a 402 plan_limit_exceeded error. */
export interface PlanLimitDetails {
  limit?: LimitKey | (string & {})
  max?: number
  used?: number
  plan?: string
}
export type APIKeyList = JSONOf<'/v1/api-keys', 'get', 200>
export type Health = JSONOf<'/health', 'get', 200>

export type PostQuery = QueryOf<'/v1/posts', 'get'>
export type EventQuery = QueryOf<'/v1/events', 'get'>
export type DeliveryQuery = QueryOf<'/v1/webhook-deliveries', 'get'>
export type EventExportQuery = QueryOf<'/v1/events/export', 'get'>

export const POST_STATUSES: PostStatus[] = ['draft', 'scheduled', 'publishing', 'published', 'failed']

export const ALL_SCOPES: Scope[] = [
  'collections:read',
  'collections:write',
  'collections:delete',
  'posts:read',
  'posts:write',
  'posts:delete',
  'posts:publish',
  'channels:read',
  'channels:manage',
  'schedules:manage',
  'media:read',
  'media:write',
  'webhooks:manage',
  'alerts:manage',
  'events:read',
  'admin',
]

/** Error raised for any non-2xx API response. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly fields?: Record<string, string>
  readonly details?: Record<string, unknown>
  /** Seconds from a Retry-After header (429). */
  readonly retryAfter?: number

  constructor(
    status: number,
    body: (Partial<ErrorBody> & { details?: Record<string, unknown> }) | undefined,
    fallback?: string,
    retryAfter?: number,
  ) {
    super(body?.message || fallback || `Request failed (${status})`)
    this.name = 'ApiError'
    this.status = status
    this.code = body?.code || (status === 0 ? 'network_error' : 'http_' + status)
    this.fields = body?.fields
    this.details = body?.details && typeof body.details === 'object' ? body.details : undefined
    this.retryAfter = retryAfter
  }

  /** HTTP 402: the workspace plan does not allow this write. */
  get isPlanLimit() {
    return this.status === 402 || this.code === 'plan_limit_exceeded'
  }
  /** HTTP 429: too many requests from this client. */
  get isRateLimited() {
    return this.status === 429 || this.code === 'rate_limited'
  }
  get planLimit(): PlanLimitDetails | undefined {
    return this.isPlanLimit ? ((this.details ?? {}) as PlanLimitDetails) : undefined
  }

  get isConflict() {
    return this.status === 409
  }
  get isUnauthorized() {
    return this.status === 401
  }
}

/** A message shown on its own starts with a capital letter (API messages are written lowercase, to follow "error:"). */
export function sentence(msg: string): string {
  return msg.charAt(0).toUpperCase() + msg.slice(1)
}

export function errorMessage(err: unknown): string {
  return sentence(rawErrorMessage(err))
}

function rawErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.isRateLimited) {
      const wait =
        err.retryAfter && err.retryAfter > 0 ? ` Try again in ${err.retryAfter}s.` : ' Try again in a moment.'

      return 'Too many requests.' + wait
    }

    if (err.fields && Object.keys(err.fields).length) {
      const detail = Object.entries(err.fields)
        .map(([k, v]) => `${k} ${v}`)
        .join('; ')

      return detail.length < 200 ? `${err.message}: ${detail}` : err.message
    }

    return err.message
  }

  if (err instanceof Error) {
    return err.message
  }

  return 'Something went wrong'
}

// ---- transport ----

/** Same-origin prefix proxied to the Go API by Next.js rewrites. */
export const API_PREFIX = '/api'

let csrfToken: string | null = null
let csrfPromise: Promise<string> | null = null
let unauthorizedHandler: ((err: ApiError) => void) | null = null

/** null = unknown (fetch lazily); "" = known to be unnecessary (local mode). */
export function setCsrfToken(token: string | null | undefined) {
  csrfToken = token ?? null
}

export function getCsrfToken() {
  return csrfToken
}

/** Registered by the app providers; invoked on any 401 from an authenticated call. */
export function onUnauthorized(handler: ((err: ApiError) => void) | null) {
  unauthorizedHandler = handler
}

type QueryValue = string | string[] | number | boolean | undefined | null

export function buildQuery(query?: Record<string, QueryValue>): string {
  if (!query) {
    return ''
  }

  const sp = new URLSearchParams()

  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === '') {
      continue
    }

    for (const one of Array.isArray(v) ? v : [v]) {
      sp.append(k, String(one)) // arrays repeat the parameter (?agent=a&agent=b)
    }
  }

  const s = sp.toString()

  return s ? `?${s}` : ''
}

/** Same-origin download URL of the activity log export (the browser sends the session cookie). */
export function eventsExportUrl(query: EventExportQuery) {
  return `${API_PREFIX}/v1/events/export${buildQuery(query)}`
}

export function mediaUrl(mediaId: string, opts?: { download?: boolean }) {
  return `${API_PREFIX}/v1/media/${encodeURIComponent(mediaId)}${opts?.download ? '?download=1' : ''}`
}

/** Widths the API serves image thumbnails at (`GET /v1/media/{id}?w=`). */
export const THUMB_WIDTHS = [160, 320, 640, 1280] as const
export type ThumbWidth = (typeof THUMB_WIDTHS)[number]

/** A scaled-down copy of an uploaded image (never wider than the original; cached forever by the browser). */
export function mediaThumbUrl(mediaId: string, width: ThumbWidth) {
  return `${mediaUrl(mediaId)}?w=${width}`
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  query?: Record<string, QueryValue>
  json?: unknown
  form?: FormData
  signal?: AbortSignal
  /** Skip the global 401 redirect (setup / recovery / probe calls). */
  public?: boolean
  /** Let the request finish after the page is gone (an approval sent while the tab closes or reloads). */
  keepalive?: boolean
}

async function parseBody(res: Response): Promise<unknown> {
  if (res.status === 204) {
    return undefined
  }

  const text = await res.text()

  if (!text) {
    return undefined
  }

  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

async function fetchCsrf(): Promise<string> {
  if (!csrfPromise) {
    csrfPromise = (async () => {
      const res = await fetch(`${API_PREFIX}/v1/session`, { credentials: 'same-origin' })
      const body = (await parseBody(res)) as SessionInfo | { error?: ErrorBody } | undefined

      if (!res.ok) {
        throw new ApiError(res.status, (body as { error?: ErrorBody })?.error)
      }

      const token = (body as SessionInfo).csrf_token ?? ''

      setCsrfToken(token)

      return token
    })().finally(() => {
      csrfPromise = null
    })
  }

  return csrfPromise
}

export async function request<T>(path: string, opts: RequestOptions = {}, retried = false): Promise<T> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }
  let body: BodyInit | undefined

  if (opts.form) {
    body = opts.form
  } else if (opts.json !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.json)
  } else if (method === 'POST' || method === 'PATCH') {
    headers['Content-Type'] = 'application/json'
    body = '{}'
  }

  const unsafe = method !== 'GET'

  // Public sign-in requests send it too: a browser with an older session cookie is
  // still checked for CSRF. Without a session the lookup fails and nothing is sent.
  if (unsafe) {
    const token = csrfToken !== null ? csrfToken : await fetchCsrf().catch(() => null)

    if (token) {
      headers['X-CSRF-Token'] = token
    }
  }

  let res: Response

  try {
    res = await fetch(`${API_PREFIX}${path}${buildQuery(opts.query)}`, {
      method,
      headers,
      body,
      credentials: 'same-origin',
      signal: opts.signal,
      keepalive: opts.keepalive,
    })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') {
      throw e
    }

    throw new ApiError(0, undefined, 'Cannot reach the staqly API. Is the server running?')
  }

  const data = await parseBody(res)

  if (res.ok) {
    return data as T
  }

  const errBody = data && typeof data === 'object' && 'error' in data ? (data as { error: ErrorBody }).error : undefined
  const retryAfter = Number(res.headers.get('Retry-After')) || undefined
  const err = new ApiError(
    res.status,
    errBody,
    typeof data === 'string' && data.length < 200 ? data : undefined,
    retryAfter,
  )

  // The CSRF token is bound to the session; refresh it once (e.g. after re-login in another tab).
  if (err.status === 403 && err.code === 'csrf_failed' && !retried) {
    setCsrfToken(null)
    await fetchCsrf().catch(() => null)

    return request<T>(path, opts, true)
  }

  if (err.status === 401 && !opts.public) {
    setCsrfToken(null)
    unauthorizedHandler?.(err)
  }

  throw err
}

// ---- endpoints ----

export const api = {
  health: () => request<Health>('/health', { public: true }),
  setupStatus: () => request<SetupStatus>('/v1/setup', { public: true }),
  setup: (body: { name?: string; setup_token?: string }) =>
    request<SetupResult>('/v1/workspaces', { method: 'POST', json: body, public: true }),
  signInWithApiKey: (api_key: string) =>
    request<{ csrf_token?: string }>('/v1/sessions/api-key', { method: 'POST', json: { api_key }, public: true }),
  recover: (recovery_key: string) =>
    request<RecoverResult>('/v1/sessions/recover', {
      method: 'POST',
      json: { recovery_key },
      public: true,
    }),
  session: (opts?: { public?: boolean }) => request<SessionInfo>('/v1/session', { public: opts?.public }),
  logout: () => request<void>('/v1/session', { method: 'DELETE' }),
  sessions: () => request<{ data?: Session[] }>('/v1/sessions'),
  revokeSession: (id: string) => request<void>(`/v1/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  workspace: () => request<Workspace>('/v1/workspace'),
  plans: () => request<{ data?: Plan[] }>('/v1/plans', { public: true }),
  usage: () => request<WorkspaceUsage>('/v1/workspace/usage'),
  createDeviceLink: () => request<DeviceLink>('/v1/device-links', { method: 'POST' }),
  redeemDeviceLink: (token: string) =>
    request<{ csrf_token?: string }>('/v1/device-links/redeem', { method: 'POST', json: { token }, public: true }),
  renameWorkspace: (name: string) => request<Workspace>('/v1/workspace', { method: 'PATCH', json: { name } }),
  rotateRecoveryKey: () => request<{ recovery_key?: string }>('/v1/recovery-key/rotate', { method: 'POST' }),

  apiKeys: () => request<APIKeyList>('/v1/api-keys'),
  createApiKey: (body: { name: string; scopes: Scope[]; expires_at?: string }) =>
    request<APIKey>('/v1/api-keys', { method: 'POST', json: body }),
  revokeApiKey: (id: string) => request<void>(`/v1/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  rotateApiKey: (id: string) => request<APIKey>(`/v1/api-keys/${encodeURIComponent(id)}/rotate`, { method: 'POST' }),

  collections: (opts?: { includeArchived?: boolean }) =>
    request<{ data?: Collection[] }>('/v1/collections', {
      query: { include_archived: opts?.includeArchived ? 'true' : undefined },
    }),
  collection: (id: string) => request<Collection>(`/v1/collections/${encodeURIComponent(id)}`),
  createCollection: (body: CollectionInput) => request<Collection>('/v1/collections', { method: 'POST', json: body }),
  updateCollection: (id: string, body: CollectionInput) =>
    request<Collection>(`/v1/collections/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteCollection: (id: string, confirm: string) =>
    request<void>(`/v1/collections/${encodeURIComponent(id)}`, { method: 'DELETE', query: { confirm } }),
  agents: () => request<{ data?: Agent[] }>('/v1/agents'),
  forgetAgent: (id: string) => request<void>(`/v1/agents/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  tags: () => request<{ data: { tag: string; posts: number }[] }>('/v1/tags'),

  // Media
  uploadMedia: (file: File | Blob, fileName?: string) => {
    const form = new FormData()

    form.append('file', file, fileName ?? (file instanceof File ? file.name : 'upload'))

    return request<Media>('/v1/media', { method: 'POST', form })
  },
  media: (id: string) => request<Media>(`/v1/media/${encodeURIComponent(id)}`),
  deleteMedia: (id: string) => request<void>(`/v1/media/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // Posts
  posts: (query: PostQuery, signal?: AbortSignal) => request<PostPage>('/v1/posts', { query, signal }),
  post: (id: string) => request<Post>(`/v1/posts/${encodeURIComponent(id)}`),
  createPost: (body: PostInput) => request<Post>('/v1/posts', { method: 'POST', json: body }),
  updatePost: (id: string, body: PostPatch) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deletePost: (id: string) => request<void>(`/v1/posts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  schedulePost: (id: string, publish_at: string) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}/schedule`, { method: 'POST', json: { publish_at } }),
  publishPost: (id: string) => request<Post>(`/v1/posts/${encodeURIComponent(id)}/publish`, { method: 'POST' }),
  cancelPost: (id: string) => request<Post>(`/v1/posts/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),
  duplicatePost: (id: string) => request<Post>(`/v1/posts/${encodeURIComponent(id)}/duplicate`, { method: 'POST' }),
  retryTarget: (id: string, channelId: string) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}/targets/${encodeURIComponent(channelId)}/retry`, {
      method: 'POST',
    }),
  reportTargetResult: (
    id: string,
    channelId: string,
    body: { status: 'published' | 'failed'; url?: string; external_id?: string; error?: string },
  ) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}/targets/${encodeURIComponent(channelId)}/result`, {
      method: 'POST',
      json: body,
    }),
  postEvents: (id: string, cursor?: string) =>
    request<EventPage>(`/v1/posts/${encodeURIComponent(id)}/events`, { query: { cursor, limit: 100 } }),
  postPreview: (id: string) => request<PostPreview>(`/v1/posts/${encodeURIComponent(id)}/preview`),

  // Channels and providers
  providers: () => request<{ data?: Provider[] }>('/v1/providers'),
  setDeveloperApp: (provider: string, body: DeveloperAppInput) =>
    request<Provider>(`/v1/providers/${encodeURIComponent(provider)}/app`, { method: 'PUT', json: body }),
  clearDeveloperApp: (provider: string) =>
    request<void>(`/v1/providers/${encodeURIComponent(provider)}/app`, { method: 'DELETE' }),
  channels: () => request<{ data?: Channel[] }>('/v1/channels'),
  channel: (id: string) => request<Channel>(`/v1/channels/${encodeURIComponent(id)}`),
  connectChannel: (body: ChannelInput) => request<Channel>('/v1/channels', { method: 'POST', json: body }),
  oauthStart: (provider: string) =>
    request<{ url: string }>(`/v1/channels/oauth/${encodeURIComponent(provider)}/start`),
  updateChannel: (id: string, body: ChannelPatch) =>
    request<Channel>(`/v1/channels/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteChannel: (id: string) => request<void>(`/v1/channels/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  /** Asks the platform who the account is: `ok` false with `error` when the sign-in no longer works. */
  testChannel: (id: string) =>
    request<ChannelTestResult>(`/v1/channels/${encodeURIComponent(id)}/test`, { method: 'POST' }),

  // Schedules (a collection's publishing rules) and the work agents are asked for
  schedules: (collectionId?: string) =>
    request<{ data?: Schedule[] }>('/v1/schedules', { query: { collection_id: collectionId || undefined } }),
  createSchedule: (body: ScheduleInput) => request<Schedule>('/v1/schedules', { method: 'POST', json: body }),
  updateSchedule: (id: string, body: Partial<ScheduleInput>) =>
    request<Schedule>(`/v1/schedules/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteSchedule: (id: string) => request<void>(`/v1/schedules/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  runSchedule: (id: string) => request<Schedule>(`/v1/schedules/${encodeURIComponent(id)}/run`, { method: 'POST' }),
  work: () => request<{ data?: WorkItem[] }>('/v1/work'),

  // The job queue (Settings → System)
  jobs: (query: { status?: string; kind?: string; cursor?: string; limit?: number }) =>
    request<JobPage>('/v1/jobs', { query }),
  jobStats: () => request<JobStats>('/v1/jobs/stats'),
  retryJob: (id: string) => request<Job>(`/v1/jobs/${encodeURIComponent(id)}/retry`, { method: 'POST' }),
  cancelJob: (id: string) => request<Job>(`/v1/jobs/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),

  events: (query: EventQuery) => request<EventPage>('/v1/events', { query }),

  webhooks: () => request<{ data?: Webhook[] }>('/v1/webhooks'),
  createWebhook: (body: WebhookInput) => request<Webhook>('/v1/webhooks', { method: 'POST', json: body }),
  updateWebhook: (id: string, body: WebhookInput) =>
    request<Webhook>(`/v1/webhooks/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteWebhook: (id: string) => request<void>(`/v1/webhooks/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  testWebhook: (id: string) => request<Delivery>(`/v1/webhooks/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  deliveries: (query: DeliveryQuery) => request<{ data?: Delivery[] }>('/v1/webhook-deliveries', { query }),
  retryDelivery: (id: string) =>
    request<Delivery>(`/v1/webhook-deliveries/${encodeURIComponent(id)}/retry`, { method: 'POST' }),

  alerts: () => request<{ data?: Alert[] }>('/v1/alerts'),
  createAlert: (body: AlertInput) => request<Alert>('/v1/alerts', { method: 'POST', json: body }),
  updateAlert: (id: string, body: AlertInput) =>
    request<Alert>(`/v1/alerts/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteAlert: (id: string) => request<void>(`/v1/alerts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  testAlert: (id: string) => request<AlertTestResult>(`/v1/alerts/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  alertDeliveries: (id: string) =>
    request<{ data?: AlertDelivery[] }>(`/v1/alerts/${encodeURIComponent(id)}/deliveries`, { query: { limit: 50 } }),

  requestWorkspaceExport: () => request<WorkspaceExport>('/v1/workspace/export', { method: 'POST' }),
  switchToFree: () => request<WorkspaceUsage>('/v1/workspace/plan', { method: 'POST', json: { plan: 'free' } }),
  billing: () => request<Billing>('/v1/billing'),
  billingCheckout: () => request<{ url: string }>('/v1/billing/checkout', { method: 'POST' }),
  billingPortal: () => request<{ url: string }>('/v1/billing/portal', { method: 'POST' }),
  workspaceExport: (id: string) => request<WorkspaceExport>(`/v1/workspace/exports/${encodeURIComponent(id)}`),
  workspaceExports: () => request<{ data: WorkspaceExport[] }>('/v1/workspace/exports'),
  deleteWorkspace: (confirm: string) => request<void>('/v1/workspace', { method: 'DELETE', json: { confirm } }),
  /** Readiness probe; 503 still carries the checks, so it is not treated as an error. */
  readyz: async (): Promise<Readiness> => {
    let res: Response

    try {
      res = await fetch(`${API_PREFIX}/readyz`, { headers: { Accept: 'application/json' }, cache: 'no-store' })
    } catch {
      throw new ApiError(0, undefined, 'Cannot reach the staqly API. Is the server running?')
    }

    const body = (await parseBody(res)) as Readiness | undefined

    if ((res.ok || res.status === 503) && body && typeof body === 'object' && 'checks' in body) {
      return body
    }

    throw new ApiError(res.status, undefined, `Readiness check failed (${res.status})`)
  },
}

/** Same-origin URL of a finished workspace export (download_url is relative to the API). */
export function exportDownloadUrl(x: Pick<WorkspaceExport, 'id' | 'download_url'>) {
  return `${API_PREFIX}${x.download_url || `/v1/workspace/exports/${encodeURIComponent(x.id)}/download`}`
}
