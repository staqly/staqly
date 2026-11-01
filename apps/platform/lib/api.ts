import type { components, operations, paths } from './api-types'
import { browserTimeZone } from './time-zone'

type Schemas = components['schemas']

export type Workspace = Schemas['Workspace']
export type ProviderCheck = Schemas['ProviderCheck']
// The OpenAPI spec lags the server on a few fields (notification channels
// limit/usage, the channels:manage scope, escalate's channel_id); they are
// added here as optional so older servers still type-check.
export type Plan = Omit<Schemas['Plan'], 'limits'> & {
  limits: Schemas['Plan']['limits'] & { notification_channels?: number }
}
/** A product sold on top of a plan (Stripe Entitlements raise limits or switch capabilities on). */
export type Addon = Schemas['Addon']
export type PlanLimits = Plan['limits']
export type APIKey = Schemas['APIKey']
export type Scope = Schemas['Scope']
export type Session = Schemas['Session']
export type Role = Schemas['Role']
export type MyWorkspace = Schemas['MyWorkspace']
export type User = Schemas['User']
export type NotificationPrefs = Schemas['NotificationPrefs']
export type NotificationChoice = Schemas['NotificationChoice']
export type Member = Schemas['Member']
export type Invitation = Schemas['Invitation']
export type InvitationPreview = Schemas['InvitationPreview']
export type EmailCodeSent = Schemas['EmailCodeSent']
export type SignInMethod = Schemas['SignInMethod']
export type PasskeyChallenge = Schemas['PasskeyChallenge'] & { options: Record<string, unknown> }
export type Collection = Schemas['Collection']
export type CollectionInput = Schemas['CollectionInput']
export type Post = Schemas['Post']
export type PostInput = Schemas['PostInput']
export type PostPatch = Schemas['PostPatch']
export type PostTarget = Schemas['PostTarget']
export type PostPage = Schemas['PostPage']
export type PostStatus = Schemas['PostStatus']
export type TargetStatus = Schemas['TargetStatus']
export type TargetMetrics = Schemas['TargetMetrics']
export type PreviewLink = Schemas['PreviewLink']
export type PostComment = Schemas['PostComment']
export type PublicPreview = Schemas['PublicPreview']
/** A passage a comment marks: the channel's version ('' the shared text), the part (0 the post, n its n-th
 * comment), the words and where they are (UTF-16 offsets, as the browser counts). */
export type CommentAnchor = {
  channel_id: string
  part: number
  quote: string
  anchor_start: number
  anchor_end: number
}
/** What testing a channel's connection says (`POST /v1/channels/{id}/test`). */
export interface ChannelTestResult {
  ok: boolean
  error?: string
  profile?: { handle?: string; name?: string; avatar_url?: string; profile_url?: string }
}

export type Channel = Schemas['Channel']
export type ChannelInput = Schemas['ChannelInput']
export type ChannelPatch = Schemas['ChannelPatch']
export type ChannelGroup = Schemas['ChannelGroup']
export type ChannelLink = Schemas['ChannelLink']
export type ChannelLinkPublic = Schemas['ChannelLinkPublic']
export type PendingConnection = Schemas['PendingConnection']
export type PlatformAccount = Schemas['PlatformAccount']
export type ChannelGroupInput = Schemas['ChannelGroupInput']
export type ChannelStatus = Schemas['ChannelStatus']
export type Provider = Schemas['Provider']
export type ProviderField = Schemas['ProviderField']
export type Schedule = Schemas['Schedule']
export type ScheduleInput = Schemas['ScheduleInput']
export type Job = Schemas['Job']
export type JobPage = Schemas['JobPage']
export type JobStats = Schemas['JobStats']
export type Media = Schemas['Media']
export type MediaProject = Schemas['MediaProject']
export type AIStatus = Schemas['AIStatus']
export type AIOperation = Schemas['AIOperation']
export type AIJob = Schemas['AIJob']
export type AIOperationId = AIOperation['id']
export type AssistantMessage = Schemas['AssistantMessage']
export type TelegramConnect = Schemas['TelegramConnect']
/** A kept conversation with the assistant (the list leaves out messages and pending). */
export type AssistantChat = Schemas['AssistantChat']
export type AIPostRequest = operations['aiPost']['requestBody']['content']['application/json']
export type AIPostDraft = operations['aiPost']['responses']['200']['content']['application/json']
export type AssistantRequest = operations['assistantChat']['requestBody']['content']['application/json']

/** What the assistant streams (POST /v1/assistant/chat). */
export interface AssistantEvent {
  type: 'text' | 'tool' | 'confirm' | 'message' | 'done' | 'error'
  text?: string
  tool?: string
  ok?: boolean
  tool_call?: NonNullable<AssistantMessage['tool_calls']>[number]
  message?: AssistantMessage
  error?: ErrorBody
}
export type StockPhoto = Schemas['StockPhoto']
export type DesignBrand = Schemas['DesignBrand']
export type StockPage = Schemas['StockPage']
export type ImageRules = Schemas['ImageRules']
export type MediaLibraryItem = Schemas['MediaLibraryItem']
export type MediaPage = Schemas['MediaPage']
export type MediaUse = Schemas['MediaUse']
export type MediaFolder = Schemas['MediaFolder']
export type MediaFolders = Schemas['MediaFolders']
export type MediaKind = 'image' | 'video' | 'audio'
export type MediaSort = 'created_desc' | 'created_asc' | 'size_desc' | 'name_asc'
export interface MediaQuery {
  q?: string
  kind?: MediaKind
  used?: 'used' | 'unused'
  /** A folder id (files directly in it) or 'root' (the top level). */
  folder?: string
  tag?: string
  /** The files in Deleted instead of the library. */
  deleted?: boolean
  /** Only files with an identical twin, grouped by content. */
  duplicates?: boolean
  sort?: MediaSort
  offset?: number
  limit?: number
}
export interface MediaPatch {
  name?: string
  alt_text?: string
  width?: number
  height?: number
  duration_ms?: number
  /** Moves the file: a folder id, or '' for the top level. */
  folder_id?: string
  /** Replaces the file's tags. */
  tags?: string[]
}
export interface MediaBulk {
  ids: string[]
  folder_id?: string
  add_tags?: string[]
  remove_tags?: string[]
}
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
export type Agent = Schemas['Agent']
export type SessionInfo = Omit<JSONOf<'/v1/session', 'get', 200>, 'mode'> & { mode?: Mode }
type RawUsage = JSONOf<'/v1/workspace/usage', 'get', 200>

export type WorkspaceUsage = Omit<RawUsage, 'plan' | 'usage'> & {
  plan: Plan
  usage: RawUsage['usage'] & { notification_channels?: number }
}
export type DeviceLink = JSONOf<'/v1/device-links', 'post', 201>
export type Billing = JSONOf<'/v1/billing', 'get', 200>
export type PromotionCode = JSONOf<'/v1/billing/promotion-code', 'post', 200>
export type BillingDetails = JSONOf<'/v1/billing/details', 'get', 200>
export type PendingPayment = JSONOf<'/v1/billing/pending-payment', 'get', 200>
export type XPosts = JSONOf<'/v1/billing/x-posts', 'get', 200>
/** How a plan is billed: `year` is twelve whole-dollar months at 20 % off or more. */
export type BillingInterval = 'month' | 'year'

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

/** The AI job's result for its preview (same origin: the session cookie authenticates it). */
export function aiResultUrl(jobId: string) {
  return `${API_PREFIX}/v1/ai/jobs/${encodeURIComponent(jobId)}/result`
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
  /** Answer the body as a Blob (files and the design editor's projects). */
  blob?: boolean
  /** Extra request headers (Idempotency-Key). */
  headers?: Record<string, string>
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

/** One turn of the assistant: its events, as they stream in, go to onEvent. */
export async function assistantChat(
  body: AssistantRequest,
  onEvent: (e: AssistantEvent) => void,
  signal?: AbortSignal,
) {
  const token = csrfToken !== null ? csrfToken : await fetchCsrf().catch(() => null)
  const res = await fetch(`${API_PREFIX}/v1/assistant/chat`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      ...(token ? { 'X-CSRF-Token': token } : {}),
    },
    body: JSON.stringify(body),
    signal,
  })

  if (!res.ok || !res.body) {
    const err = (await parseBody(res)) as { error?: ErrorBody } | undefined

    throw new ApiError(res.status, err?.error)
  }

  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  let buf = ''

  for (;;) {
    const { done, value } = await reader.read()

    if (done) {
      break
    }

    buf += value

    let at = buf.indexOf('\n\n')

    while (at >= 0) {
      const line = buf.slice(0, at)

      buf = buf.slice(at + 2)
      at = buf.indexOf('\n\n')

      if (line.startsWith('data: ')) {
        onEvent(JSON.parse(line.slice(6)) as AssistantEvent)
      }
    }
  }
}

/**
 * Uploads one file to the media library and reports progress (0 to 1): XMLHttpRequest, because fetch can't
 * tell how much of a request body went out.
 */
export async function uploadMediaWithProgress(
  file: File,
  onProgress: (share: number) => void,
  signal?: AbortSignal,
): Promise<Media> {
  const token = csrfToken !== null ? csrfToken : await fetchCsrf().catch(() => null)
  const form = new FormData()

  form.append('file', file, file.name)

  return new Promise<Media>((resolve, reject) => {
    const xhr = new XMLHttpRequest()

    xhr.open('POST', `${API_PREFIX}/v1/media`)
    xhr.setRequestHeader('Accept', 'application/json')

    if (token) {
      xhr.setRequestHeader('X-CSRF-Token', token)
    }

    const abort = () => xhr.abort()

    if (signal?.aborted) {
      reject(new DOMException('Upload canceled', 'AbortError'))

      return
    }

    xhr.upload.addEventListener('progress', (e) => e.lengthComputable && onProgress(e.loaded / e.total))
    signal?.addEventListener('abort', abort, { once: true })
    xhr.addEventListener('loadend', () => signal?.removeEventListener('abort', abort))
    xhr.addEventListener('abort', () => reject(new DOMException('Upload canceled', 'AbortError')))
    xhr.addEventListener('error', () =>
      reject(new ApiError(0, undefined, 'Cannot reach the staqly API. Is the server running?')),
    )
    xhr.addEventListener('load', () => {
      let data: unknown = xhr.responseText

      try {
        data = JSON.parse(xhr.responseText)
      } catch {
        // a plain-text error from a proxy
      }

      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(data as Media)

        return
      }

      const errBody =
        data && typeof data === 'object' && 'error' in data ? (data as { error: ErrorBody }).error : undefined
      const err = new ApiError(xhr.status, errBody, typeof data === 'string' && data.length < 200 ? data : undefined)

      // Signed out meanwhile (session expired, another tab signed out): the same sign-in path as every request.
      if (err.status === 401) {
        setCsrfToken(null)
        unauthorizedHandler?.(err)
      }

      reject(err)
    })
    xhr.send(form)
  })
}

/**
 * How long a read may take before it fails as unreachable (a hung proxy or a stalled connection would otherwise
 * leave a page loading forever; queries retry it). Writes, uploads and file downloads have no limit: they may
 * legitimately take long, and a retried write could apply twice.
 */
export const READ_TIMEOUT_MS = 30_000

function readSignal(signal: AbortSignal | undefined): AbortSignal | undefined {
  if (typeof AbortSignal.timeout !== 'function') {
    return signal
  }

  const timeout = AbortSignal.timeout(READ_TIMEOUT_MS)

  if (!signal) {
    return timeout
  }

  return typeof AbortSignal.any === 'function' ? AbortSignal.any([signal, timeout]) : signal
}

export async function request<T>(path: string, opts: RequestOptions = {}, retried = false): Promise<T> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers }
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
  let data: unknown

  try {
    res = await fetch(`${API_PREFIX}${path}${buildQuery(opts.query)}`, {
      method,
      headers,
      body,
      credentials: 'same-origin',
      signal: method === 'GET' && !opts.blob ? readSignal(opts.signal) : opts.signal,
      keepalive: opts.keepalive,
    })

    if (res.ok && opts.blob) {
      return (await res.blob()) as T
    }

    data = await parseBody(res)
  } catch (e) {
    // By name: a signal's reason may be a DOMException of another realm (AbortSignal.any, workers, jsdom).
    const name = (e as { name?: unknown } | null)?.name

    if (name === 'AbortError') {
      throw e
    }

    if (name === 'TimeoutError') {
      throw new ApiError(0, undefined, 'The staqly API did not answer in time. Try again in a moment.')
    }

    throw new ApiError(0, undefined, 'Cannot reach the staqly API. Is the server running?')
  }

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

// A post's comments come 500 a page; the threads on screen need every one.
async function allPostComments(id: string): Promise<{ data: PostComment[] }> {
  const data: PostComment[] = []
  let cursor: string | undefined

  do {
    const page: { data: PostComment[]; next_cursor?: string } = await request(
      `/v1/posts/${encodeURIComponent(id)}/comments`,
      { query: cursor ? { cursor } : {} },
    )

    data.push(...page.data)
    cursor = page.next_cursor
  } while (cursor)

  return { data }
}

export const api = {
  health: () => request<Health>('/health', { public: true }),
  setupStatus: () => request<SetupStatus>('/v1/setup', { public: true }),
  // The dashboard never creates workspaces with a setup token nor signs in with an API key or a recovery key:
  // people sign in with Google, an email code or a passkey; API keys are for agents. A local install signs a
  // browser in with its access token (pasted, or from the link printed at start).
  session: (opts?: { public?: boolean }) => request<SessionInfo>('/v1/session', { public: opts?.public }),
  signInWithAccessToken: (token: string) =>
    request<{ csrf_token?: string }>('/v1/sessions/api-key', {
      method: 'POST',
      json: { api_key: token },
      public: true,
    }),
  accessToken: () => request<{ token: string }>('/v1/access-token'),
  rotateAccessToken: () =>
    request<{ token: string; sessions_revoked: number }>('/v1/access-token/rotate', { method: 'POST' }),
  logout: () => request<void>('/v1/session', { method: 'DELETE' }),
  finishOnboarding: (name?: string) => request<void>('/v1/session/onboarded', { method: 'POST', json: { name } }),
  sessions: () => request<{ data?: Session[] }>('/v1/sessions'),
  revokeSession: (id: string) => request<void>(`/v1/sessions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  workspace: () => request<Workspace>('/v1/workspace'),
  plans: () => request<{ data?: Plan[] }>('/v1/plans', { public: true }),
  usage: () => request<WorkspaceUsage>('/v1/workspace/usage'),
  createDeviceLink: () => request<DeviceLink>('/v1/device-links', { method: 'POST' }),
  redeemDeviceLink: (token: string) =>
    request<{ csrf_token?: string }>('/v1/device-links/redeem', { method: 'POST', json: { token }, public: true }),
  renameWorkspace: (name: string) => request<Workspace>('/v1/workspace', { method: 'PATCH', json: { name } }),
  updateWorkspace: (body: { name?: string; timezone?: string; logo?: string }) =>
    request<Workspace>('/v1/workspace', { method: 'PATCH', json: body }),
  rotateRecoveryKey: () => request<{ recovery_key?: string }>('/v1/recovery-key/rotate', { method: 'POST' }),

  // People in cloud mode (sign-in, workspaces, members, invitations).
  createWorkspace: (name: string, timezone?: string) =>
    request<MyWorkspace>('/v1/account/workspaces', { method: 'POST', json: { name, timezone } }),
  switchWorkspace: (workspace_id: string) =>
    request<void>('/v1/session/workspace', { method: 'POST', json: { workspace_id } }),
  updateMe: (body: { name?: string; avatar?: string }) => request<User>('/v1/account', { method: 'PATCH', json: body }),
  notificationPrefs: () => request<NotificationPrefs>('/v1/account/notifications'),
  setNotificationPrefs: (body: { scope?: NotificationPrefs['scope']; events?: NotificationChoice[] }) =>
    request<NotificationPrefs>('/v1/account/notifications', { method: 'PUT', json: body }),
  unsubscribe: (token: string) =>
    request<{ event: string }>('/v1/notifications/unsubscribe', { method: 'POST', json: { token }, public: true }),
  members: () => request<{ data: Member[] }>('/v1/members'),
  updateMember: (userId: string, role: Role) =>
    request<Member>(`/v1/members/${encodeURIComponent(userId)}`, { method: 'PATCH', json: { role } }),
  removeMember: (userId: string) => request<void>(`/v1/members/${encodeURIComponent(userId)}`, { method: 'DELETE' }),
  invitations: () => request<{ data: Invitation[] }>('/v1/invitations'),
  invite: (email: string, role: Role) =>
    request<Invitation>('/v1/invitations', { method: 'POST', json: { email, role } }),
  resendInvitation: (id: string) =>
    request<Invitation>(`/v1/invitations/${encodeURIComponent(id)}/resend`, { method: 'POST' }),
  revokeInvitation: (id: string) => request<void>(`/v1/invitations/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  lookupInvitation: (token: string) =>
    request<InvitationPreview>(`/v1/invitations/lookup?token=${encodeURIComponent(token)}`, { public: true }),

  // Sign-in with an email code, and the signed-in person's sign-in methods.
  startEmailSignIn: (email: string) =>
    request<EmailCodeSent>('/v1/auth/email/start', { method: 'POST', json: { email }, public: true }),
  // timezone: a new person's first workspace starts in the browser's zone.
  verifyEmailSignIn: (body: { email: string; code: string; next?: string; invite?: string; setup_token?: string }) =>
    request<{ csrf_token: string; next: string }>('/v1/auth/email/verify', {
      method: 'POST',
      json: { ...body, timezone: browserTimeZone() },
      public: true,
    }),
  signInMethods: () => request<{ data: SignInMethod[] }>('/v1/account/sign-in-methods'),
  startEmailLink: (email: string) =>
    request<EmailCodeSent>('/v1/account/sign-in-methods/email/start', { method: 'POST', json: { email } }),
  verifyEmailLink: (email: string, code: string) =>
    request<{ data: SignInMethod[] }>('/v1/account/sign-in-methods/email/verify', {
      method: 'POST',
      json: { email, code },
    }),
  startEmailChange: (email: string) =>
    request<EmailCodeSent>('/v1/account/sign-in-methods/email/change/current/start', {
      method: 'POST',
      json: { email },
    }),
  verifyEmailChangeCurrent: (email: string, code: string) =>
    request<void>('/v1/account/sign-in-methods/email/change/current/verify', { method: 'POST', json: { email, code } }),
  startEmailChangeNew: (email: string, newEmail: string) =>
    request<EmailCodeSent>('/v1/account/sign-in-methods/email/change/new/start', {
      method: 'POST',
      json: { email, new_email: newEmail },
    }),
  verifyEmailChangeNew: (email: string, newEmail: string, code: string) =>
    request<{ data: SignInMethod[] }>('/v1/account/sign-in-methods/email/change/new/verify', {
      method: 'POST',
      json: { email, new_email: newEmail, code },
    }),
  startPasskeyRegistration: () =>
    request<PasskeyChallenge>('/v1/account/sign-in-methods/passkey/start', { method: 'POST' }),
  finishPasskeyRegistration: (body: { challenge_id: string; name: string; response: Record<string, unknown> }) =>
    request<{ data: SignInMethod[] }>('/v1/account/sign-in-methods/passkey/finish', { method: 'POST', json: body }),
  renamePasskey: (id: string, name: string) =>
    request<{ data: SignInMethod[] }>(`/v1/account/sign-in-methods/passkey/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      json: { name },
    }),
  startPasskeySignIn: () => request<PasskeyChallenge>('/v1/auth/passkey/start', { method: 'POST', public: true }),
  finishPasskeySignIn: (body: { challenge_id: string; response: Record<string, unknown>; next?: string }) =>
    request<{ csrf_token: string; next: string }>('/v1/auth/passkey/finish', {
      method: 'POST',
      json: body,
      public: true,
    }),
  startGoogleLink: () => request<{ url: string }>('/v1/account/sign-in-methods/google', { method: 'POST' }),
  removeSignInMethod: (m: Pick<SignInMethod, 'provider' | 'subject'>) =>
    request<{ data: SignInMethod[] }>(
      `/v1/account/sign-in-methods/${encodeURIComponent(m.provider)}/${encodeURIComponent(m.subject)}`,
      { method: 'DELETE' },
    ),

  apiKeys: () => request<APIKeyList>('/v1/api-keys'),
  createApiKey: (body: { name: string; scopes: Scope[]; expires_at?: string }) =>
    request<APIKey>('/v1/api-keys', { method: 'POST', json: body }),
  revokeApiKey: (id: string) => request<void>(`/v1/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  rotateApiKey: (id: string) => request<APIKey>(`/v1/api-keys/${encodeURIComponent(id)}/rotate`, { method: 'POST' }),

  collections: (opts?: { includeArchived?: boolean }) =>
    request<{ data?: Collection[] }>('/v1/collections', {
      query: { include_archived: opts?.includeArchived ? 'true' : undefined },
    }),
  createCollection: (body: CollectionInput) => request<Collection>('/v1/collections', { method: 'POST', json: body }),
  updateCollection: (id: string, body: CollectionInput) =>
    request<Collection>(`/v1/collections/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteCollection: (id: string, confirm: string) =>
    request<void>(`/v1/collections/${encodeURIComponent(id)}`, { method: 'DELETE', query: { confirm } }),
  agents: () => request<{ data?: Agent[] }>('/v1/agents'),
  forgetAgent: (id: string) => request<void>(`/v1/agents/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  tags: () => request<{ data: { name: string; count: number }[] }>('/v1/tags'),

  // Media
  uploadMedia: (file: File | Blob, fileName?: string) => {
    const form = new FormData()

    form.append('file', file, fileName ?? (file instanceof File ? file.name : 'upload'))

    return request<Media>('/v1/media', { method: 'POST', form })
  },
  media: (id: string) => request<Media>(`/v1/media/${encodeURIComponent(id)}/info`),
  /** Moves a file to Deleted; `permanent` deletes it for good. */
  deleteMedia: (id: string, opts: { permanent?: boolean } = {}) =>
    request<void>(`/v1/media/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      query: opts.permanent ? { permanent: 'true' } : undefined,
    }),
  restoreMedia: (id: string) => request<Media>(`/v1/media/${encodeURIComponent(id)}/restore`, { method: 'POST' }),
  keepMedia: (id: string) =>
    request<{ deleted: string[] }>(`/v1/media/${encodeURIComponent(id)}/keep`, { method: 'POST' }),
  mediaLibrary: (query: MediaQuery, signal?: AbortSignal) =>
    request<MediaPage>('/v1/media', { query: { ...query }, signal }),
  updateMedia: (id: string, body: MediaPatch) =>
    request<Media>(`/v1/media/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  mediaUsage: (id: string) => request<{ data: MediaUse[] }>(`/v1/media/${encodeURIComponent(id)}/usage`),
  deleteMediaMany: (ids: string[], opts: { permanent?: boolean } = {}) =>
    request<{ deleted: string[]; missing: string[] }>('/v1/media/delete', {
      method: 'POST',
      json: { ids, permanent: opts.permanent || undefined },
    }),
  organizeMedia: (body: MediaBulk) =>
    request<{ updated: string[]; missing: string[] }>('/v1/media/bulk', { method: 'POST', json: body }),
  mediaFile: (id: string, signal?: AbortSignal) =>
    request<Blob>(`/v1/media/${encodeURIComponent(id)}`, { blob: true, signal }),
  /** The design editor's "Replace the original": answers the edited file (a new id; drafts follow it). */
  replaceMedia: (id: string, file: Blob, fileName: string) => {
    const form = new FormData()

    form.append('file', file, fileName)

    return request<Media>(`/v1/media/${encodeURIComponent(id)}/replace`, { method: 'POST', form })
  },
  /** Saves an editor's file in place of the one it saved before, or as a new file when that one is gone (Deleted). */
  saveEdited: (before: string | undefined, file: Blob, fileName: string) =>
    before
      ? api.replaceMedia(before, file, fileName).catch((e: unknown) => {
          if (e instanceof ApiError && e.status === 404) {
            return api.uploadMedia(file, fileName)
          }

          throw e
        })
      : api.uploadMedia(file, fileName),
  mediaProject: (id: string) => request<MediaProject>(`/v1/media-projects/${encodeURIComponent(id)}`),
  mediaProjectFile: (id: string) => request<Blob>(`/v1/media-projects/${encodeURIComponent(id)}/file`, { blob: true }),
  mediaProjects: (media?: string) =>
    request<{ data: MediaProject[] }>('/v1/media-projects', { query: { media: media || undefined } }),
  saveMediaProject: (
    body: { id?: string; name: string; source_ids: string[]; output_id?: string; page_ids?: string[] },
    file: Blob,
  ) => {
    // Fields before the file: the server reads them first.
    const form = new FormData()

    form.append('name', body.name)
    form.append('source_ids', body.source_ids.join(','))
    form.append('output_id', body.output_id ?? '')
    form.append('page_ids', (body.page_ids ?? []).join(','))
    form.append('file', file, 'design.json')

    return body.id
      ? request<MediaProject>(`/v1/media-projects/${encodeURIComponent(body.id)}`, { method: 'PUT', form })
      : request<MediaProject>('/v1/media-projects', { method: 'POST', form })
  },
  stockPhotos: (query: string, page = 1, signal?: AbortSignal) =>
    request<StockPage>('/v1/stock/photos', { query: { query: query || undefined, page }, signal }),
  /** Copies a stock photo into the library (Pixabay allows no hotlinking). */
  keepStockPhoto: (id: string) => request<Media>(`/v1/stock/photos/${encodeURIComponent(id)}/use`, { method: 'POST' }),
  designBrand: () => request<DesignBrand>('/v1/design/brand'),
  setDesignBrand: (b: DesignBrand) => request<DesignBrand>('/v1/design/brand', { method: 'PUT', json: b }),
  ai: () => request<AIStatus>('/v1/ai'),
  /**
   * Starts an AI request: the settings first, then the files (the server reads them in that order). The key makes a
   * repeated send (a retry after a lost answer) answer the same job instead of paying twice.
   */
  startAiJob: (
    body: {
      operation: AIOperationId
      prompt?: string
      fields?: Record<string, number | string>
      image?: Blob
      mask?: Blob
    },
    idempotencyKey: string,
  ) => {
    const form = new FormData()

    form.append('operation', body.operation)

    if (body.prompt) {
      form.append('prompt', body.prompt)
    }

    for (const [k, v] of Object.entries(body.fields ?? {})) {
      form.append(k, String(v))
    }

    if (body.image) {
      form.append('image', body.image, 'image.png')
    }

    if (body.mask) {
      form.append('mask', body.mask, 'mask.png')
    }

    return request<AIJob>('/v1/ai/jobs', { method: 'POST', form, headers: { 'Idempotency-Key': idempotencyKey } })
  },
  aiJob: (id: string) => request<AIJob>(`/v1/ai/jobs/${encodeURIComponent(id)}`),
  acceptAiJob: (id: string) => request<Media>(`/v1/ai/jobs/${encodeURIComponent(id)}/accept`, { method: 'POST' }),
  discardAiJob: (id: string) => request<void>(`/v1/ai/jobs/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  mediaFolders: () => request<MediaFolders>('/v1/media-folders'),
  createMediaFolder: (body: { name: string; parent_id?: string }) =>
    request<MediaFolder>('/v1/media-folders', { method: 'POST', json: body }),
  updateMediaFolder: (id: string, body: { name?: string; parent_id?: string }) =>
    request<MediaFolder>(`/v1/media-folders/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteMediaFolder: (id: string) => request<void>(`/v1/media-folders/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // Posts
  posts: (query: PostQuery, signal?: AbortSignal) => request<PostPage>('/v1/posts', { query, signal }),
  post: (id: string) => request<Post>(`/v1/posts/${encodeURIComponent(id)}`),
  createPost: (body: PostInput) => request<Post>('/v1/posts', { method: 'POST', json: body }),
  updatePost: (id: string, body: PostPatch) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deletePost: (id: string) => request<void>(`/v1/posts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  deletePosts: (ids: string[]) =>
    request<{ deleted: string[]; missing: string[]; kept: string[] }>('/v1/posts/delete', {
      method: 'POST',
      json: { ids },
    }),
  schedulePost: (id: string, publish_at: string) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}/schedule`, { method: 'POST', json: { publish_at } }),
  cancelPost: (id: string) => request<Post>(`/v1/posts/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),
  unpublishPost: (id: string, channelIds?: string[]) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}/unpublish`, {
      method: 'POST',
      json: channelIds ? { channel_ids: channelIds } : {},
    }),
  duplicatePost: (id: string) => request<Post>(`/v1/posts/${encodeURIComponent(id)}/duplicate`, { method: 'POST' }),
  postMetrics: (id: string, refresh = false) =>
    request<{ data: TargetMetrics[] }>(`/v1/posts/${encodeURIComponent(id)}/metrics${refresh ? '?refresh=true' : ''}`),
  previewLinks: (id: string) => request<{ data: PreviewLink[] }>(`/v1/posts/${encodeURIComponent(id)}/previews`),
  createPreviewLink: (id: string, expires_in_days = 0) =>
    request<PreviewLink>(`/v1/posts/${encodeURIComponent(id)}/previews`, { method: 'POST', json: { expires_in_days } }),
  revokePreviewLink: (id: string, previewId: string) =>
    request<void>(`/v1/posts/${encodeURIComponent(id)}/previews/${encodeURIComponent(previewId)}`, {
      method: 'DELETE',
    }),
  /** Every page of the post's comments (threads need all of them). */
  postComments: (id: string) => allPostComments(id),
  deletePostComment: (id: string, commentId: string) =>
    request<void>(`/v1/posts/${encodeURIComponent(id)}/comments/${encodeURIComponent(commentId)}`, {
      method: 'DELETE',
    }),
  addPostComment: (id: string, body: { body: string; parent_id?: string } & Partial<CommentAnchor>) =>
    request<PostComment>(`/v1/posts/${encodeURIComponent(id)}/comments`, { method: 'POST', json: body }),
  setPostCommentResolved: (id: string, commentId: string, resolved: boolean) =>
    request<PostComment>(
      `/v1/posts/${encodeURIComponent(id)}/comments/${encodeURIComponent(commentId)}/${resolved ? 'resolve' : 'reopen'}`,
      { method: 'POST' },
    ),
  // The public preview page: the token is the only credential.
  openPreview: (token: string) => request<PublicPreview>(`/v1/previews/${encodeURIComponent(token)}`, { public: true }),
  addPreviewComment: (
    token: string,
    body: { name: string; email?: string; body: string; parent_id?: string } & Partial<CommentAnchor>,
  ) =>
    request<PostComment>(`/v1/previews/${encodeURIComponent(token)}/comments`, {
      method: 'POST',
      json: body,
      public: true,
    }),
  setPreviewCommentResolved: (token: string, commentId: string, name: string, resolved: boolean) =>
    request<PostComment>(
      `/v1/previews/${encodeURIComponent(token)}/comments/${encodeURIComponent(commentId)}/${resolved ? 'resolve' : 'reopen'}`,
      { method: 'POST', json: resolved ? { name } : {}, public: true },
    ),
  retryTarget: (id: string, channelId: string) =>
    request<Post>(`/v1/posts/${encodeURIComponent(id)}/targets/${encodeURIComponent(channelId)}/retry`, {
      method: 'POST',
    }),

  // Channels and providers
  providers: () => request<{ data?: Provider[] }>('/v1/providers'),
  channels: () => request<{ data?: Channel[] }>('/v1/channels'),
  channelGroups: () => request<{ data?: ChannelGroup[] }>('/v1/channel-groups'),
  createChannelGroup: (body: ChannelGroupInput) =>
    request<ChannelGroup>('/v1/channel-groups', { method: 'POST', json: body }),
  updateChannelGroup: (id: string, body: ChannelGroupInput) =>
    request<ChannelGroup>(`/v1/channel-groups/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteChannelGroup: (id: string) =>
    request<void>(`/v1/channel-groups/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  connectChannel: (body: ChannelInput) => request<Channel>('/v1/channels', { method: 'POST', json: body }),
  channelLinks: () => request<{ data: ChannelLink[] }>('/v1/channel-links'),
  createChannelLink: (body: {
    note?: string
    providers?: string[]
    group_id?: string
    expires_in_hours?: number
    max_uses?: number
  }) => request<ChannelLink>('/v1/channel-links', { method: 'POST', json: body }),
  revokeChannelLink: (id: string) => request<void>(`/v1/channel-links/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  // The connect link's page: the token is the only credential (someone without access to Staqly).
  openConnectLink: (token: string) =>
    request<ChannelLinkPublic>(`/v1/connect-links/${encodeURIComponent(token)}`, { public: true }),
  connectViaLink: (token: string, body: { provider: string; fields?: Record<string, string> }) =>
    request<{ name: string; handle: string; provider: string }>(
      `/v1/connect-links/${encodeURIComponent(token)}/channels`,
      { method: 'POST', json: body, public: true },
    ),
  /** fields: what the platform asks before its sign-in (Mastodon: the server), sent as query parameters. */
  connectLinkOAuthStart: (token: string, provider: string, fields?: Record<string, string>) =>
    request<{ url: string }>(
      `/v1/connect-links/${encodeURIComponent(token)}/oauth/${encodeURIComponent(provider)}/start${startQuery(fields)}`,
      { public: true },
    ),
  oauthStart: (provider: string, fields?: Record<string, string>) =>
    request<{ url: string }>(`/v1/channels/oauth/${encodeURIComponent(provider)}/start${startQuery(fields)}`),
  /** A sign-in that may publish as several accounts, waiting for the person to pick them. */
  pendingConnection: (id: string) => request<PendingConnection>(`/v1/pending-connections/${encodeURIComponent(id)}`),
  finishPendingConnection: (id: string, accountIds: string[]) =>
    request<{ data: Channel[] }>(`/v1/pending-connections/${encodeURIComponent(id)}`, {
      method: 'POST',
      json: { account_ids: accountIds },
    }),
  pendingConnectionViaLink: (token: string, id: string) =>
    request<PendingConnection>(`/v1/connect-links/${encodeURIComponent(token)}/pending/${encodeURIComponent(id)}`, {
      public: true,
    }),
  finishPendingConnectionViaLink: (token: string, id: string, accountIds: string[]) =>
    request<{ data: { name: string; handle: string; provider: string }[] }>(
      `/v1/connect-links/${encodeURIComponent(token)}/pending/${encodeURIComponent(id)}`,
      { method: 'POST', json: { account_ids: accountIds }, public: true },
    ),
  updateChannel: (id: string, body: ChannelPatch) =>
    request<Channel>(`/v1/channels/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteChannel: (id: string) => request<void>(`/v1/channels/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  /** Asks the platform who the account is: `ok` false with `error` when the sign-in no longer works. */
  testChannel: (id: string) =>
    request<ChannelTestResult>(`/v1/channels/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  /** A short text rewritten by AI (the design editor's text tools). */
  aiText: (text: string, instruction: string) =>
    request<{ text: string }>('/v1/ai/text', { method: 'POST', json: { text, instruction } }),
  /** A post drafted by AI from a brief (the composer's Write with AI); nothing is saved. */
  aiPost: (body: AIPostRequest) => request<AIPostDraft>('/v1/ai/post', { method: 'POST', json: body }),
  /** The person's assistant chats, newest first, a page at a time; q finds words in titles. */
  assistantChats: (q: string, cursor?: string) =>
    request<{ data: AssistantChat[]; next_cursor?: string }>(
      `/v1/assistant/chats?${new URLSearchParams({ q, limit: '30', ...(cursor ? { cursor } : {}) })}`,
    ),
  assistantChatById: (id: string) => request<AssistantChat>(`/v1/assistant/chats/${encodeURIComponent(id)}`),
  deleteAssistantChat: (id: string) =>
    request<void>(`/v1/assistant/chats/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  /** A /connect code for the server's Telegram bot, and whether its chat is connected yet. */
  startTelegramConnect: () => request<TelegramConnect>('/v1/channels/telegram/connect', { method: 'POST' }),
  telegramConnect: (code: string) =>
    request<TelegramConnect>(`/v1/channels/telegram/connect/${encodeURIComponent(code)}`),
  /** The choices of a channel's remote setting, from its platform (Slack's channels). */
  channelOptions: (id: string, key: string) =>
    request<{ data: { value: string; label: string }[] }>(
      `/v1/channels/${encodeURIComponent(id)}/options/${encodeURIComponent(key)}`,
    ),
  /** This server's setup for a platform (app keys, return address, media), independent of any account. */
  checkProvider: (id: string) =>
    request<ProviderCheck>(`/v1/providers/${encodeURIComponent(id)}/check`, { method: 'POST' }),

  // Schedules (a collection's publishing rules) and the work agents are asked for
  schedules: (collectionId?: string) =>
    request<{ data?: Schedule[] }>('/v1/schedules', { query: { collection_id: collectionId || undefined } }),
  createSchedule: (body: ScheduleInput) => request<Schedule>('/v1/schedules', { method: 'POST', json: body }),
  updateSchedule: (id: string, body: Partial<ScheduleInput>) =>
    request<Schedule>(`/v1/schedules/${encodeURIComponent(id)}`, { method: 'PATCH', json: body }),
  deleteSchedule: (id: string) => request<void>(`/v1/schedules/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  runSchedule: (id: string) => request<Schedule>(`/v1/schedules/${encodeURIComponent(id)}/run`, { method: 'POST' }),

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
  billing: () => request<Billing>('/v1/billing'),
  billingDetails: (startingAfter?: string) =>
    request<BillingDetails>(
      startingAfter ? `/v1/billing/details?starting_after=${encodeURIComponent(startingAfter)}` : '/v1/billing/details',
    ),
  /** Pays for a plan (Stripe Checkout URL), or switches a paying workspace to it (billing page URL). */
  billingCheckout: (plan: string, interval: BillingInterval, returnTo?: 'welcome', promotionCode?: string) =>
    request<{ url: string }>('/v1/billing/checkout', {
      method: 'POST',
      json: {
        plan,
        interval,
        ...(returnTo ? { return_to: returnTo } : {}),
        ...(promotionCode ? { promotion_code: promotionCode } : {}),
      },
    }),
  billingPortal: () => request<{ url: string }>('/v1/billing/portal', { method: 'POST' }),
  /** Checks a promotion code: what it takes off and the plans it applies to (422 when it isn't valid). */
  checkPromotionCode: (code: string) =>
    request<PromotionCode>('/v1/billing/promotion-code', { method: 'POST', json: { promotion_code: code } }),
  /** A plan change waiting for its payment (leaving the trial) and why it isn't paid, read from Stripe. */
  pendingPayment: () => request<PendingPayment>('/v1/billing/pending-payment'),
  /** Reads the subscription from Stripe now (back from Checkout, before or without its webhook). */
  syncBilling: () => request<Billing>('/v1/billing/sync', { method: 'POST' }),
  /** Ends the free trial now and pays for the plan (the card on file is charged at once). */
  startPaidPlan: () => request<Billing>('/v1/billing/start-paid-plan', { method: 'POST' }),
  /** Posts to X this period: included, used and bought ones left. */
  xPosts: () => request<XPosts>('/v1/billing/x-posts'),
  /** Buys a bundle of extra posts to X (Stripe Checkout URL). */
  creditsCheckout: (posts: number) =>
    request<{ url: string }>('/v1/billing/credits/checkout', { method: 'POST', json: { provider: 'x', posts } }),
  /** Add-ons on sale and which the workspace has. */
  addons: () => request<{ data: Addon[] }>('/v1/billing/addons'),
  addAddon: (id: string) => request<void>(`/v1/billing/addons/${encodeURIComponent(id)}`, { method: 'POST' }),
  removeAddon: (id: string) => request<void>(`/v1/billing/addons/${encodeURIComponent(id)}`, { method: 'DELETE' }),
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

function startQuery(fields?: Record<string, string>) {
  const q = new URLSearchParams(fields).toString()

  return q ? `?${q}` : ''
}
