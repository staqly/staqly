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
export type Scope = Schemas['Scope'] | 'channels:manage'
export type Session = Schemas['Session']
export type ActionDef = Schemas['ActionDef']
export type Collection = Schemas['Collection']
export type CollectionInput = Schemas['CollectionInput']
export type Item = Schemas['Item']
export type ItemStatus = Schemas['ItemStatus']
export type ItemType = Schemas['ItemType']
export type ItemFields = Schemas['ItemFields']
export type CreateItemInput = Schemas['CreateItemInput']
export type UpdateItemInput = Schemas['UpdateItemInput']
export type ItemPage = Schemas['ItemPage']
export type MediaRef = Schemas['MediaRef']
export type Media = Schemas['Media']
export type StashEvent = Schemas['Event']
export type EventPage = Schemas['EventPage']
export type Webhook = Schemas['Webhook']
export type WebhookInput = Schemas['WebhookInput']
export type Delivery = Schemas['Delivery']
export type ErrorBody = Schemas['ErrorBody']
export type CollectionView = NonNullable<Collection['default_view']>
export type Rule = Omit<Schemas['Rule'], 'params'> & { params: RuleParams }
export type RuleInput = Omit<Schemas['RuleInput'], 'params'> & { params?: RuleParams }
export type RuleMatch = Schemas['RuleMatch']
export type RuleParams = Schemas['RuleParams'] & {
  /** escalate: notify only this channel (empty = every channel subscribed to item.escalated). */
  channel_id?: string
}
export type RuleAction = Rule['action']
export type RuleCondition = NonNullable<RuleMatch['conditions']>[number]
export type RuleOp = RuleCondition['op']
export type Watcher = Schemas['Watcher']
export type WatcherInput = Schemas['WatcherInput']
export type WatcherPreset = Schemas['WatcherPreset']
export type WatcherPresetParam = WatcherPreset['params'][number]
export type WatcherKind = Watcher['kind']
export type WatcherStatus = Watcher['status']
export type NotificationChannel = Schemas['NotificationChannel']
export type NotificationChannelInput = Omit<Schemas['NotificationChannelInput'], 'config'> & {
  /** `null` removes a key; a masked secret (as returned by GET) keeps the stored one. */
  config?: { [K in keyof Schemas['NotificationChannelConfig']]?: Schemas['NotificationChannelConfig'][K] | null }
}
export type ChannelType = NotificationChannel['type']
export type NotificationDelivery = Schemas['NotificationDelivery']
export type ChannelTestResult = JSONOf<'/v1/notification-channels/{id}/test', 'post', 200>
export type WorkspaceExport = Schemas['WorkspaceExport']
export type PurgeResult = Schemas['PurgeResult']
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

/** "local": single-user, no credentials on localhost. "cloud": hosted, many workspaces, sessions + recovery key. */
export type Mode = 'local' | 'cloud'
// Older servers did not report mode / signup_open, so keep them optional for the UI.
export type SetupStatus = Omit<JSONOf<'/v1/setup', 'get', 200>, 'mode' | 'signup_open'> & {
  mode?: Mode
  signup_open?: boolean
}
export type SetupResult = JSONOf<'/v1/workspaces', 'post', 201>
export type SessionInfo = Omit<JSONOf<'/v1/session', 'get', 200>, 'mode'> & { mode?: Mode }
type RawUsage = JSONOf<'/v1/workspace/usage', 'get', 200>

export type WorkspaceUsage = Omit<RawUsage, 'plan' | 'usage'> & {
  plan: Plan
  usage: RawUsage['usage'] & { notification_channels?: number }
}
export type DeviceLink = JSONOf<'/v1/device-links', 'post', 201>

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
export type ItemVersion = NonNullable<JSONOf<'/v1/items/{id}/versions', 'get', 200>['data']>[number]
export type BulkResult = NonNullable<JSONOf<'/v1/items/bulk-actions', 'post', 200>['data']>[number]
export type Health = JSONOf<'/health', 'get', 200>
export type WatcherRun = JSONOf<'/v1/watchers/{id}/run', 'post', 200>
export type FromPresetInput = NonNullable<
  paths['/v1/watchers/from-preset']['post']['requestBody']
>['content']['application/json']

export type ItemQuery = QueryOf<'/v1/items', 'get'>
export type EventQuery = QueryOf<'/v1/events', 'get'>
export type DeliveryQuery = QueryOf<'/v1/webhook-deliveries', 'get'>
export type EventExportQuery = QueryOf<'/v1/events/export', 'get'>

export const ITEM_STATUSES: ItemStatus[] = [
  'draft',
  'pending',
  'approved',
  'rejected',
  'expired',
  'processing',
  'completed',
  'failed',
  'archived',
]
export const ITEM_TYPES: ItemType[] = ['image', 'video', 'text', 'link', 'json', 'file']
export const ALL_SCOPES: Scope[] = [
  'collections:read',
  'collections:write',
  'items:read',
  'items:write',
  'items:review',
  'actions:execute',
  'webhooks:manage',
  'rules:manage',
  'watchers:manage',
  'channels:manage',
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

export function errorMessage(err: unknown): string {
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

type QueryValue = string | number | boolean | undefined | null

export function buildQuery(query?: Record<string, QueryValue>): string {
  if (!query) {
    return ''
  }

  const sp = new URLSearchParams()

  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === '') {
      continue
    }

    sp.set(k, String(v))
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
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  query?: Record<string, QueryValue>
  json?: unknown
  form?: FormData
  signal?: AbortSignal
  /** Skip the global 401 redirect (setup / recovery / probe calls). */
  public?: boolean
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

  if (unsafe && !opts.public) {
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

export interface ActionBody {
  feedback?: string
  edits?: ItemFields
  payload?: unknown
  expected_version?: number
}

export const api = {
  health: () => request<Health>('/health', { public: true }),
  setupStatus: () => request<SetupStatus>('/v1/setup', { public: true }),
  setup: (body: { name?: string; setup_token?: string }) =>
    request<SetupResult>('/v1/workspaces', { method: 'POST', json: body, public: true }),
  recover: (recovery_key: string) =>
    request<{ csrf_token?: string }>('/v1/sessions/recover', {
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
  createItem: (collectionId: string, body: CreateItemInput) =>
    request<Item>(`/v1/collections/${encodeURIComponent(collectionId)}/items`, { method: 'POST', json: body }),
  uploadItem: (collectionId: string, file: File | Blob, item?: CreateItemInput, fileName?: string) => {
    const form = new FormData()

    if (item && Object.keys(item).length) {
      form.append('item', JSON.stringify(item))
    }

    form.append('file', file, fileName ?? (file instanceof File ? file.name : 'upload'))

    return request<Item>(`/v1/collections/${encodeURIComponent(collectionId)}/items`, { method: 'POST', form })
  },

  items: (query: ItemQuery, signal?: AbortSignal) => request<ItemPage>('/v1/items', { query, signal }),
  item: (id: string) => request<Item>(`/v1/items/${encodeURIComponent(id)}`),
  updateItem: (id: string, body: UpdateItemInput) =>
    request<Item>(`/v1/items/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteItem: (id: string) => request<void>(`/v1/items/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  itemActions: (id: string) => request<{ data?: ActionDef[] }>(`/v1/items/${encodeURIComponent(id)}/actions`),
  runAction: (id: string, action: string, body: ActionBody = {}) =>
    request<Item>(`/v1/items/${encodeURIComponent(id)}/actions/${encodeURIComponent(action)}`, {
      method: 'POST',
      json: body,
    }),
  bulkAction: (body: { ids: string[]; action: string; feedback?: string }) =>
    request<{ data?: BulkResult[] }>('/v1/items/bulk-actions', { method: 'POST', json: body }),
  itemEvents: (id: string, cursor?: string) =>
    request<EventPage>(`/v1/items/${encodeURIComponent(id)}/events`, { query: { cursor, limit: 100 } }),
  itemVersions: (id: string) => request<{ data?: ItemVersion[] }>(`/v1/items/${encodeURIComponent(id)}/versions`),

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

  rules: () => request<{ data?: Rule[] }>('/v1/rules'),
  createRule: (body: RuleInput) => request<Rule>('/v1/rules', { method: 'POST', json: body }),
  updateRule: (id: string, body: RuleInput) =>
    request<Rule>(`/v1/rules/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteRule: (id: string) => request<void>(`/v1/rules/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  watcherPresets: () => request<{ data?: WatcherPreset[] }>('/v1/watcher-presets'),
  watchers: () => request<{ data?: Watcher[] }>('/v1/watchers'),
  createWatcher: (body: WatcherInput) => request<Watcher>('/v1/watchers', { method: 'POST', json: body }),
  createWatcherFromPreset: (body: FromPresetInput) =>
    request<Watcher>('/v1/watchers/from-preset', { method: 'POST', json: body }),
  updateWatcher: (id: string, body: WatcherInput) =>
    request<Watcher>(`/v1/watchers/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteWatcher: (id: string) => request<void>(`/v1/watchers/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  runWatcher: (id: string) => request<WatcherRun>(`/v1/watchers/${encodeURIComponent(id)}/run`, { method: 'POST' }),

  channels: () => request<{ data?: NotificationChannel[] }>('/v1/notification-channels'),
  createChannel: (body: NotificationChannelInput) =>
    request<NotificationChannel>('/v1/notification-channels', { method: 'POST', json: body }),
  updateChannel: (id: string, body: NotificationChannelInput) =>
    request<NotificationChannel>(`/v1/notification-channels/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      json: body,
    }),
  deleteChannel: (id: string) =>
    request<void>(`/v1/notification-channels/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  testChannel: (id: string) =>
    request<ChannelTestResult>(`/v1/notification-channels/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  channelDeliveries: (id: string) =>
    request<{ data?: NotificationDelivery[] }>(`/v1/notification-channels/${encodeURIComponent(id)}/deliveries`, {
      query: { limit: 50 },
    }),

  requestWorkspaceExport: () => request<WorkspaceExport>('/v1/workspace/export', { method: 'POST' }),
  workspaceExport: (id: string) => request<WorkspaceExport>(`/v1/workspace/exports/${encodeURIComponent(id)}`),
  deleteWorkspace: (confirm: string) => request<void>('/v1/workspace', { method: 'DELETE', json: { confirm } }),
  purgeItem: (id: string) => request<PurgeResult>(`/v1/items/${encodeURIComponent(id)}/purge`, { method: 'POST' }),

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
