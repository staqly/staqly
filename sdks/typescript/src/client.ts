import { type ClientOptions, HTTPClient } from './core.js'
import { RejectedError } from './errors.js'
import { BillingResource } from './resources/billing.js'
import { Collections } from './resources/collections.js'
import { Events } from './resources/events.js'
import { Exports } from './resources/exports.js'
import { Items, type WaitOptions } from './resources/items.js'
import { MediaResource } from './resources/media.js'
import { NotificationChannels } from './resources/notifications.js'
import { DecisionLinks, DeviceLinks, Sessions } from './resources/sessions.js'
import { Watchers } from './resources/watchers.js'
import { Webhooks } from './resources/webhooks.js'
import { APIKeys, Plans, WorkspaceResource, Workspaces } from './resources/workspace.js'
import type {
  CreateItemInput,
  Item,
  ItemStatus,
  Liveness,
  Readiness,
  RequestOptions,
  SetupStatus,
  Whoami,
} from './types.js'

/** Outcome of `requestApproval`. */
export interface Decision {
  /** The item after the decision (reviewer edits included). */
  item: Item
  /** `item.status`. */
  status: ItemStatus
  /** True when a reviewer approved it. */
  approved: boolean
  /** True when nobody decided before `expires_in` passed (not approved). */
  expired: boolean
  /** Reviewer feedback ("" when none). */
  feedback: string
}

export interface ApprovalOptions<A extends unknown[]> extends WaitOptions {
  /** Collection ID or name that holds approval requests. */
  collection: string
  /** Item title, or a function of the wrapped function's arguments. */
  title: string | ((...args: A) => string)
  /** Item content (string or JSON); defaults to the arguments as a JSON array. */
  content?: unknown | ((...args: A) => unknown)
  /** Item metadata. */
  metadata?: Record<string, unknown> | ((...args: A) => Record<string, unknown>)
  /** Expire the request if nobody decides in time: seconds or a duration like "24h" (60 s – 90 days). */
  expires_in?: number | string
  /** Fields the reviewer may edit before approving, e.g. ["content", "metadata.caption"]. */
  editable?: string[]
  /** Report the item `completed` (with the return value as `result`) or `failed` after the function runs (default true). */
  report?: boolean
}

/**
 * staqly API client.
 *
 * ```ts
 * const staqly = new Staqly();                                              // local mode
 * const cloud = new Staqly({ baseUrl: 'https://staqly.example.com', apiKey: 'stk_...' })
 * ```
 */
export class Staqly {
  readonly collections: Collections
  readonly items: Items
  readonly media: MediaResource
  readonly events: Events
  readonly webhooks: Webhooks
  readonly watchers: Watchers
  readonly workspace: WorkspaceResource
  readonly workspaces: Workspaces
  readonly plans: Plans
  readonly apiKeys: APIKeys
  readonly billing: BillingResource
  readonly notificationChannels: NotificationChannels
  readonly exports: Exports
  readonly sessions: Sessions
  readonly deviceLinks: DeviceLinks
  readonly decisionLinks: DecisionLinks
  /** Low-level HTTP client (retries, auth, error mapping); use `request` for endpoints without a helper. */
  readonly http: HTTPClient

  constructor(options: ClientOptions = {}) {
    this.http = new HTTPClient(options)
    this.collections = new Collections(this.http)
    this.items = new Items(this.http, this.collections)
    this.media = new MediaResource(this.http)
    this.events = new Events(this.http)
    this.webhooks = new Webhooks(this.http)
    this.watchers = new Watchers(this.http)
    this.workspace = new WorkspaceResource(this.http)
    this.workspaces = new Workspaces(this.http)
    this.plans = new Plans(this.http)
    this.apiKeys = new APIKeys(this.http)
    this.billing = new BillingResource(this.http)
    this.notificationChannels = new NotificationChannels(this.http)
    this.exports = new Exports(this.http)
    this.sessions = new Sessions(this.http)
    this.deviceLinks = new DeviceLinks(this.http)
    this.decisionLinks = new DecisionLinks(this.http)
  }

  get baseUrl(): string {
    return this.http.baseUrl
  }

  /** Liveness and database check (no auth). */
  health(options?: RequestOptions): Promise<{ status?: string; version?: string }> {
    return this.http.request({ method: 'GET', path: '/health', noAuth: true, options })
  }

  /**
   * Readiness probe (database, migrations, background workers). Resolves with
   * the report when ready; a not-ready server throws `InternalServerError`
   * (503) whose `body` is the same report. Not retried.
   */
  ready(options?: RequestOptions): Promise<Readiness> {
    return this.http.request({ method: 'GET', path: '/readyz', noAuth: true, options: { maxRetries: 0, ...options } })
  }

  /** Liveness probe: resolves while the process serves HTTP (does not touch the database; no auth). */
  live(options?: RequestOptions): Promise<Liveness> {
    return this.http.request({ method: 'GET', path: '/livez', noAuth: true, options })
  }

  /** Server mode and whether the workspace exists / signup is open (no auth). */
  setup(options?: RequestOptions): Promise<SetupStatus> {
    return this.http.request({ method: 'GET', path: '/v1/setup', noAuth: true, options })
  }

  /** The server's OpenAPI document as YAML text (no auth). */
  async openapi(options?: RequestOptions): Promise<string> {
    const res = await this.http.request<Response>({
      method: 'GET',
      path: '/openapi.yaml',
      noAuth: true,
      raw: true,
      options: { ...options, headers: { Accept: 'application/yaml', ...options?.headers } },
    })

    return res.text()
  }

  /**
   * Prometheus metrics (text exposition format). Allowed for direct requests
   * from the server's machine, or with the operator's `METRICS_TOKEN` (not an
   * API key, which is never sent here); otherwise `NotFoundError`.
   */
  async metrics(params: { token?: string } = {}, options?: RequestOptions): Promise<string> {
    const headers: Record<string, string> = { Accept: 'text/plain', ...options?.headers }

    if (params.token) {
      headers.Authorization = `Bearer ${params.token}`
    }

    const res = await this.http.request<Response>({
      method: 'GET',
      path: '/metrics',
      noAuth: true,
      raw: true,
      options: { ...options, headers },
    })

    return res.text()
  }

  /** Server mode, workspace and the calling principal with its scopes. */
  whoami(options?: RequestOptions): Promise<Whoami> {
    return this.http.request({ method: 'GET', path: '/v1/session', options })
  }

  /**
   * Human approval gate: creates an item and waits until a reviewer decides
   * it. Resolves with the decision (never throws on rejection).
   *
   * ```ts
   * const d = await staqly.requestApproval('Outbox', { title: 'Send newsletter?', content: draft })
   * if (d.approved) send(d.item.content)
   * ```
   */
  async requestApproval(
    collection: string,
    input: CreateItemInput & { title: string },
    wait: WaitOptions = {},
    options?: RequestOptions,
  ): Promise<Decision> {
    const created = await this.items.create(collection, input, options)
    const item = created.status === 'pending' ? await this.items.waitForDecision(created.id, wait, options) : created

    return {
      item,
      status: item.status,
      approved: item.status === 'approved',
      expired: item.status === 'expired',
      feedback: item.feedback,
    }
  }

  /**
   * Wraps a function so every call first asks a human. The wrapped function
   * runs only after approval and then reports the item `completed` (or
   * `failed`, re-throwing the error) via `items.reportResult`. A rejection or
   * expiry throws `RejectedError`.
   *
   * ```ts
   * const publish = staqly.requiresApproval(postToInstagram, {
   *   collection: 'Instagram', title: (post) => `Publish '${post.caption}'?`,
   * })
   * await publish(post)
   * ```
   */
  requiresApproval<A extends unknown[], R>(
    fn: (...args: A) => R | Promise<R>,
    opts: ApprovalOptions<A>,
  ): (...args: A) => Promise<R> {
    const value = <T>(v: T | ((...args: A) => T), args: A): T =>
      typeof v === 'function' ? (v as (...args: A) => T)(...args) : v

    return async (...args: A): Promise<R> => {
      const content = opts.content === undefined ? jsonable(args) : value(opts.content, args)
      const decision = await this.requestApproval(
        opts.collection,
        {
          title: value(opts.title, args),
          content,
          ...(opts.metadata ? { metadata: value(opts.metadata, args) } : {}),
          ...(opts.expires_in !== undefined ? { expires_in: opts.expires_in } : {}),
          ...(opts.editable ? { editable: opts.editable } : {}),
        },
        opts,
      )

      if (!decision.approved) {
        throw new RejectedError(decision.item)
      }

      const report = opts.report ?? true

      try {
        const result = await fn(...args)

        if (report) {
          await this.items.reportResult(decision.item.id, {
            status: 'completed',
            ...(result === undefined ? {} : { result: jsonable(result) }),
          })
        }

        return result
      } catch (err) {
        if (report) {
          await this.items
            .reportResult(decision.item.id, {
              status: 'failed',
              error: err instanceof Error ? err.message : String(err),
            })
            .catch(() => undefined)
        }

        throw err
      }
    }
  }
}

function jsonable(v: unknown): unknown {
  try {
    JSON.stringify(v)

    return v
  } catch {
    return String(v)
  }
}
