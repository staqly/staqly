// Public model types. Everything here is derived from the generated
// `schema.ts` (openapi-typescript over apps/api/openapi.yaml) so the SDK
// cannot drift from the contract. Request bodies and query parameters use the
// API's snake_case names; SDK-only options (timeouts, idempotency) are camelCase.
import type { components, paths } from './schema.js'

export type { components, paths } from './schema.js'

type Schemas = components['schemas']
type JSONResponse<P extends keyof paths, M extends keyof paths[P], S extends number = 200> = paths[P][M] extends {
  responses: infer R
}
  ? R extends Record<S, { content: { 'application/json': infer B } }>
    ? B
    : never
  : never

export type Scope = Schemas['Scope']
export type ErrorBodySchema = Schemas['ErrorBody']

export type Collection = Schemas['Collection']
export type CollectionInput = Schemas['CollectionInput']

export type PostStatus = Schemas['PostStatus']
export type TargetStatus = Schemas['TargetStatus']
export type Post = Schemas['Post']
export type PostTarget = Schemas['PostTarget']
export type PostTargetInput = Schemas['PostTargetInput']
export type PostInput = Schemas['PostInput']
/** '' (no repeat), 1d to 6d, 1w, 2w or 1mo: once published, a copy is scheduled that much later. */
export type RepeatEvery = Schemas['RepeatEvery']
/** A reply published under the post (a thread part): content and/or media, delay_seconds after the part before. */
export type ThreadPart = Schemas['ThreadPart']
/** A thread part as returned, with its media resolved. */
export type ThreadPartOut = Schemas['ThreadPartOut']
/** How one thread part went on one channel: pending, publishing, published, failed or skipped. */
export type Reply = Schemas['Reply']
export type PostPatch = Schemas['PostPatch']
export type PostPage = Schemas['PostPage']
export type PostPreview = Schemas['PostPreview']
export type PostMetrics = Schemas['Metrics']
export type TargetMetrics = Schemas['TargetMetrics']
/** Replies to a post per channel, with an analysis (counts, summary, themes, questions). */
export type PostResponses = Schemas['PostResponses']
export type ChannelResponses = Schemas['ChannelResponses']
export type ResponseInsight = Schemas['ResponseInsight']
export type PostResponse = Schemas['PostResponse']
/** The analytics report (`analytics.get`). */
export type Analytics = Schemas['Analytics']
export type AnalyticsTotals = Schemas['AnalyticsTotals']
/** Platform numbers in one vocabulary; engagements = likes + replies + reposts + quotes + bookmarks. */
export type AnalyticsMetrics = Schemas['AnalyticsMetrics']
export type ChannelAnalytics = Schemas['ChannelAnalytics']
export type AnalyticsTimeSlot = Schemas['AnalyticsTimeSlot']
/** Filters for `analytics.get`; days are YYYY-MM-DD in the workspace's time zone. */
export interface AnalyticsParams {
  /** First day (default: 29 days before `to`). */
  from?: string
  /** Last day, included (default: today). At most 366 days in all. */
  to?: string
  /** Instead of `from`: the last n days up to `to` (1 to 366; 7, 30, 90...). Sending both is a 422. */
  days?: number
  /** Only this channel's targets. */
  channel?: string
  /** Only posts of this collection. */
  collection?: string
  /** Only posts from this source, as `sources[].source` names it (`dashboard`, an agent's name, `schedule:<name>`, `repeat`). */
  source?: string
}
export type PreviewLink = Schemas['PreviewLink']
export type ChannelLink = Schemas['ChannelLink']
export type ChannelLinkPublic = Schemas['ChannelLinkPublic']
export type PostComment = Schemas['PostComment']
export type PostCommentInput = NonNullable<
  paths['/v1/posts/{id}/comments']['post']['requestBody']
>['content']['application/json']
export type PublicPreview = Schemas['PublicPreview']
export type Tag = Schemas['Tag']
export type WorkItem = Schemas['WorkItem']
export type Media = Schemas['Media']
export type MediaLibraryItem = Schemas['MediaLibraryItem']
export type MediaPage = Schemas['MediaPage']
export type MediaUse = Schemas['MediaUse']
export type MediaFolder = Schemas['MediaFolder']
export type MediaFolders = Schemas['MediaFolders']
export type MediaProject = Schemas['MediaProject']
export type AIStatus = Schemas['AIStatus']
export type AIJob = Schemas['AIJob']
export type DesignBrand = Schemas['DesignBrand']
export type StockPhoto = Schemas['StockPhoto']
export type StockPage = Schemas['StockPage']
export interface MediaListParams {
  /** Part of the file name (case-insensitive). */
  q?: string
  kind?: 'image' | 'video' | 'audio'
  used?: 'used' | 'unused'
  /** A folder id (files directly in it) or `root` (the top level). */
  folder?: string
  /** Files carrying this tag. */
  tag?: string
  /** List the files in Deleted (with `deleted_at` and `purge_at`) instead of the library. */
  deleted?: boolean
  /** Only files with an identical twin (same sha256), grouped by content. */
  duplicates?: boolean
  sort?: 'created_desc' | 'created_asc' | 'size_desc' | 'name_asc'
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
export interface MediaFolderInput {
  name?: string
  /** The folder it sits in; '' for the top level. */
  parent_id?: string
}
export interface MediaBulkInput {
  ids: string[]
  /** Moves the files: a folder id, or '' for the top level; omit to leave folders alone. */
  folder_id?: string
  add_tags?: string[]
  remove_tags?: string[]
}
/** Body of `posts.reportResult`: what your own automation did with a target. */
export type TargetResultInput = NonNullable<
  paths['/v1/posts/{id}/targets/{channel}/result']['post']['requestBody']
>['content']['application/json']

export type ChannelStatus = Schemas['ChannelStatus']
/** What a channel does with the links in a post's text at publish time: keep, remove or defang (example[.]com). */
export type LinksMode = Schemas['LinksMode']
export type Channel = Schemas['Channel']
export type ChannelActivity = Schemas['ChannelActivity']
export type ChannelGroup = Schemas['ChannelGroup']
export type ChannelGroupInput = Schemas['ChannelGroupInput']
export type ChannelInput = Schemas['ChannelInput']
export type ChannelPatch = Schemas['ChannelPatch']
export type ChannelTestResult = JSONResponse<'/v1/channels/{id}/test', 'post'>
/** One choice of a channel's remote setting (a Pinterest board, a Discord channel...). */
export type ChannelOption = JSONResponse<'/v1/channels/{id}/options/{key}', 'get'>['data'][number]
export type TelegramConnect = Schemas['TelegramConnect']
export type ProviderCheck = Schemas['ProviderCheck']
export type DeveloperAppInput = Schemas['DeveloperAppInput']
export type Provider = Schemas['Provider']
export type ProviderField = Schemas['ProviderField']
export type ProviderLimits = Schemas['ProviderLimits']

export type Schedule = Schemas['Schedule']
export type ScheduleInput = Schemas['ScheduleInput']
export type ScheduleRule = Schemas['ScheduleRule']

export type Job = Schemas['Job']
export type JobPage = Schemas['JobPage']
export type JobStats = Schemas['JobStats']

export type Alert = Schemas['Alert']
export type AlertInput = Schemas['AlertInput']
export type AlertConfig = Schemas['AlertConfig']
export type AlertDelivery = Schemas['AlertDelivery']
export type AlertTestResult = JSONResponse<'/v1/alerts/{id}/test', 'post'>

export type Event = Schemas['Event']
export type EventPage = Schemas['EventPage']

export type Webhook = Schemas['Webhook']
export type WebhookInput = Schemas['WebhookInput']
export type Delivery = Schemas['Delivery']

export type WorkspaceExport = Schemas['WorkspaceExport']
export type Agent = Schemas['Agent']
export type PurgeResult = Schemas['PurgeResult']
export type Readiness = Schemas['Readiness']

/** Filters for `exports.events` (query parameters of GET /v1/events/export). */
export interface EventExportParams {
  /** created_at >= (RFC3339 or YYYY-MM-DD). */
  from?: string
  /** created_at < (RFC3339 or YYYY-MM-DD). */
  to?: string
  /** Exact event type. */
  type?: string
  /** Literal prefix, e.g. `post.`. */
  type_prefix?: string
  actor_type?: 'session' | 'api_key' | 'local' | 'system'
  actor_id?: string
  collection_id?: string
  post_id?: string
}

export type Plan = Schemas['Plan']
/** The plans on sale (cloud). */
export type PlanID = 'starter' | 'plus' | 'pro' | 'business'
/** How a plan is billed: `year` is twelve whole-dollar months at 20 % off or more. */
export type BillingInterval = 'month' | 'year'
export type Billing = Schemas['Billing']
export type BillingDetails = Schemas['BillingDetails']
export type BillingSubscription = Schemas['BillingSubscription']
export type PaymentMethod = Schemas['PaymentMethod']
export type Invoice = Schemas['Invoice']
export type BillingURL = Schemas['BillingURL']
export type XPosts = Schemas['XPosts']
export type Workspace = Schemas['Workspace']
/** A person in the workspace and their role (cloud accounts). */
export type Member = Schemas['Member']
export type Role = Schemas['Role']
export type User = Schemas['User']
export type MyWorkspace = Schemas['MyWorkspace']
export type WorkspaceUsage = JSONResponse<'/v1/workspace/usage', 'get'>
export type WorkspaceCreated = JSONResponse<'/v1/workspaces', 'post', 201>
export type APIKey = Schemas['APIKey']
export type Session = Schemas['Session']
export type Whoami = JSONResponse<'/v1/session', 'get'>
export type SetupStatus = JSONResponse<'/v1/setup', 'get'>
export type DeviceLink = JSONResponse<'/v1/device-links', 'post', 201>
/** Response of `sessions.signInWithApiKey` / `deviceLinks.redeem`; the session itself is a cookie. */
export type SessionCreated = JSONResponse<'/v1/sessions/api-key', 'post', 201>
/**
 * Response of `sessions.recover`: the CSRF token of the new session (a cookie), a new admin
 * API key (`api_key.key`) and the next recovery key. Both secrets are shown only once.
 */
export type SessionRecovered = JSONResponse<'/v1/sessions/recover', 'post', 201>
export type RecoveryKey = JSONResponse<'/v1/recovery-key/rotate', 'post'>
/** A local-mode server's access token (`workspace.accessToken`), `stk_local_...`. */
export type AccessToken = JSONResponse<'/v1/access-token', 'get'>
/** Response of `workspace.rotateAccessToken`: the new token and how many other browsers were signed out. */
export type AccessTokenRotated = JSONResponse<'/v1/access-token/rotate', 'post'>
export type Liveness = JSONResponse<'/livez', 'get'>

/** Every API key scope, in the server's order. */
export const SCOPES: readonly Scope[] = [
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
  'integrations:manage',
  'admin',
]

/** A page of results with an opaque cursor for the next one. */
export interface Page<T> {
  data: T[]
  next_cursor?: string
}

export type PostSort = 'publish_asc' | 'publish_desc' | 'created_desc' | 'created_asc' | 'updated_desc'

/** Filters for `posts.list` / `posts.iterate` (query parameters of GET /v1/posts). */
export interface PostListParams {
  /** Collection id or exact name. */
  collection_id?: string
  status?: PostStatus
  /** Posts with a target on this channel. */
  channel_id?: string
  /** Posts carrying this tag. */
  tag?: string
  /** Words of the content (case-insensitive substring match). */
  q?: string
  /** `publish_at >=` (RFC3339 or YYYY-MM-DD); posts without a time never match. */
  publish_after?: string
  /** `publish_at <` (RFC3339 or YYYY-MM-DD). */
  publish_before?: string
  /** Default `publish_asc`: scheduled posts first, soonest first, posts without a time last. */
  sort?: PostSort
  /** Page size, 1–200 (server default 50). */
  limit?: number
  cursor?: string
}

/** Filters for `collections.listPosts` (the same as `posts.list` without the collection). */
export type CollectionPostListParams = Omit<PostListParams, 'collection_id'>

export interface EventListParams {
  post_id?: string
  collection_id?: string
  channel_id?: string
  schedule_id?: string
  /** Exact event type, e.g. `post.published`. */
  type?: string
  actor_type?: 'local' | 'session' | 'api_key' | 'system'
  /** One actor, e.g. an API key id (`key_...`) or a session id. */
  actor_id?: string
  /** Agent name as listed by `workspace.agents()` (e.g. `claude-code`). */
  agent?: string
  /** Literal type prefix, e.g. `post.` or `webhook.`. */
  type_prefix?: string
  /** `created_at >=` (RFC 3339 or YYYY-MM-DD). */
  from?: string
  /** `created_at <` (RFC 3339 or YYYY-MM-DD). */
  to?: string
  limit?: number
  cursor?: string
}

export interface DeliveryListParams {
  webhook_id?: string
  status?: 'pending' | 'succeeded' | 'failed'
  /** Only deliveries of events about this post. */
  post_id?: string
  limit?: number
}

export interface JobListParams {
  status?: Job['status']
  kind?: Job['kind']
  limit?: number
  cursor?: string
}

export interface APIKeyCreateInput {
  name: string
  scopes: Scope[]
  /** RFC3339 timestamp or YYYY-MM-DD. */
  expires_at?: string
}

/** Per-request overrides accepted by every method as the last argument. */
export interface RequestOptions {
  /** Timeout for this request in milliseconds. */
  timeout?: number
  /** Retries for this request (overrides the client's `maxRetries`). */
  maxRetries?: number
  /** Idempotency-Key for unsafe requests (one is generated automatically when retries are enabled). */
  idempotencyKey?: string
  /** Extra headers. */
  headers?: Record<string, string>
  /** Abort the request (and any retries / polling). */
  signal?: AbortSignal
}

/** A webhook delivery body (`POST` to your endpoint), returned by `verifyWebhook`. */
export interface WebhookEvent<D = Record<string, unknown>> {
  /** Event ID (also in `X-Staqly-Event-Id`); use it to de-duplicate at-least-once deliveries. */
  id: string
  /** Event type, e.g. `post.published`, `post.publish_requested`, `webhook.test`. */
  type: string
  /** When the event happened. Deliveries can arrive out of order: order by this, not by arrival. */
  created_at: string
  workspace_id: string
  delivery_id: string
  attempt: number
  actor: { type: string; id: string; name: string }
  /** Event data. Post events carry `post` and `collection`; target events add `channel`. */
  data: D
}

/** `data` of post events (`post.*`). */
export interface PostEventData {
  post: Post
  collection: { id: string; name: string }
  /** Target events (`post.publish_requested`, `post.target_published`): the channel. */
  channel?: { id: string; name: string; provider: string }
  previous_status?: PostStatus
  error?: string
  [key: string]: unknown
}
