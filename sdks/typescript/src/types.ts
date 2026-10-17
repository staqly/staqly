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
export type ItemStatus = Schemas['ItemStatus']
export type ItemType = Schemas['ItemType']
export type ErrorBodySchema = Schemas['ErrorBody']

export type Collection = Schemas['Collection']
export type CollectionInput = Schemas['CollectionInput']
export type ActionDef = Schemas['ActionDef']

export type Item = Schemas['Item']
export type ItemFields = Schemas['ItemFields']
export type CreateItemInput = Schemas['CreateItemInput']
export type UpdateItemInput = Schemas['UpdateItemInput']
export type ItemPage = Schemas['ItemPage']
export type MediaRef = Schemas['MediaRef']
export type Media = Schemas['Media']
export type ItemWaitResult = JSONResponse<'/v1/items/{id}/wait', 'get'>
export type ReportResultInput = NonNullable<
  paths['/v1/items/{id}/result']['post']['requestBody']
>['content']['application/json']
export type ItemVersion = NonNullable<JSONResponse<'/v1/items/{id}/versions', 'get'>['data']>[number]
export type BulkActionResult = NonNullable<JSONResponse<'/v1/items/bulk-actions', 'post'>['data']>[number]

export type Event = Schemas['Event']
export type EventPage = Schemas['EventPage']

export type Webhook = Schemas['Webhook']
export type WebhookInput = Schemas['WebhookInput']
export type Delivery = Schemas['Delivery']

export type Watcher = Schemas['Watcher']
export type WatcherInput = Schemas['WatcherInput']
export type WatcherRunResult = JSONResponse<'/v1/watchers/{id}/run', 'post'>

export type NotificationChannel = Schemas['NotificationChannel']
export type NotificationChannelInput = Schemas['NotificationChannelInput']
export type NotificationChannelConfig = Schemas['NotificationChannelConfig']
export type NotificationDelivery = Schemas['NotificationDelivery']
export type NotificationTestResult = JSONResponse<'/v1/notification-channels/{id}/test', 'post'>

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
  /** Literal prefix, e.g. `item.`. */
  type_prefix?: string
  actor_type?: 'session' | 'api_key' | 'local' | 'system'
  actor_id?: string
  collection_id?: string
  item_id?: string
}

export type Plan = Schemas['Plan']
export type Billing = Schemas['Billing']
export type BillingURL = Schemas['BillingURL']
export type Workspace = Schemas['Workspace']
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
export type DecisionResult = Schemas['DecisionResult']
export type Liveness = JSONResponse<'/livez', 'get'>

/** Filters for `collections.listItems` (query parameters of GET /v1/collections/{id}/items). */
export type CollectionItemListParams = Omit<ItemListParams, 'collection' | 'collection_id'>

/** Every API key scope, in the server's order. */
export const SCOPES: readonly Scope[] = [
  'collections:read',
  'collections:write',
  'items:read',
  'items:write',
  'items:review',
  'actions:execute',
  'webhooks:manage',
  'watchers:manage',
  'channels:manage',
  'admin',
]

/** A page of results with an opaque cursor for the next one. */
export interface Page<T> {
  data: T[]
  next_cursor?: string
}

export type ItemSort =
  | 'relevance'
  | 'created_desc'
  | 'created_asc'
  | 'updated_desc'
  | 'updated_asc'
  | 'decided_desc'
  | 'title_asc'

/** Filters for `items.list` / `items.iterate` (query parameters of GET /v1/items). */
export interface ItemListParams {
  /** Collection ID or exact name (a name is resolved with one extra request). */
  collection?: string
  collection_id?: string
  status?: ItemStatus
  /** One or several (any of): pending, approved, rejected, expired (archived items are left out), or archived. */
  decision?: string | string[]
  /** One or several (any of): none, processing, completed, failed. */
  run?: string | string[]
  /** Items carrying any of these tags. */
  tag?: string | string[]
  /** `false` leaves archived items out, `true` lists only them. */
  archived?: boolean
  /** Runs started by this action (e.g. `publish`), or `none`. */
  run_action?: string
  type?: ItemType
  /** Full-text search (prefix match, diacritics-insensitive) over title, content and metadata. */
  q?: string
  source?: string
  external_ref?: string
  /** RFC3339 timestamp or YYYY-MM-DD. */
  created_after?: string
  created_before?: string
  decided_after?: string
  decided_before?: string
  /** Defaults to `relevance` when `q` is set, otherwise `created_desc`. */
  sort?: ItemSort
  /** Page size, 1–200 (server default 50). */
  limit?: number
  cursor?: string
}

export interface EventListParams {
  item_id?: string
  collection_id?: string
  /** Exact event type, e.g. `item.approved`. */
  type?: string
  actor_type?: 'local' | 'session' | 'api_key' | 'system'
  /** One actor, e.g. an API key id (`key_...`) or a session id. */
  actor_id?: string
  /** Literal type prefix, e.g. `item.` or `webhook.`. */
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
  /** Only deliveries of events about this item. */
  item_id?: string
  limit?: number
}

/** Body of approve/reject/custom action requests. */
export interface ActionInput {
  /** Reviewer feedback; required by `reject`-style actions that set `requires_feedback`. */
  feedback?: string
  /** Edit the item while deciding ("edit before approval"). */
  edits?: ItemFields
  /** Arbitrary JSON forwarded in the event and webhook payload. */
  payload?: unknown
  /** Optimistic concurrency guard: fail with 409 unless the item is at this version. */
  expected_version?: number
  /** After a reject only: run this action next with the same feedback, e.g. `regenerate`. */
  follow_up?: string
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
  /** Event type, e.g. `item.approved`, `item.created`, `webhook.test`. */
  type: string
  created_at: string
  workspace_id: string
  delivery_id: string
  attempt: number
  actor: { type: string; id: string; name: string }
  /** Event data. Item events carry `item`, `collection`, `previous_status` and, for actions, `action`, `feedback`, `payload`. */
  data: D
}

/** `data` of item events (`item.*`). */
export interface ItemEventData {
  item: Item
  collection: { id: string; name: string }
  previous_status?: ItemStatus
  /** On approvals with edits: `{field: {before, after}}`. */
  diff?: Record<string, { before: unknown; after: unknown }>
  action?: string
  feedback?: string
  edited?: boolean
  payload?: unknown
  [key: string]: unknown
}
