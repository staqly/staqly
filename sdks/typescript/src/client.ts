import { type ClientOptions, HTTPClient } from './core.js'
import { Alerts } from './resources/alerts.js'
import { AnalyticsResource } from './resources/analytics.js'
import { BillingResource } from './resources/billing.js'
import { Channels, Providers } from './resources/channels.js'
import { Collections } from './resources/collections.js'
import { Events } from './resources/events.js'
import { Exports } from './resources/exports.js'
import { Jobs } from './resources/jobs.js'
import { MediaResource } from './resources/media.js'
import { Posts } from './resources/posts.js'
import { Schedules } from './resources/schedules.js'
import { DeviceLinks, Sessions } from './resources/sessions.js'
import { Webhooks } from './resources/webhooks.js'
import { Work } from './resources/work.js'
import { APIKeys, Plans, WorkspaceResource, Workspaces } from './resources/workspace.js'
import type { Liveness, Readiness, RequestOptions, SetupStatus, Whoami } from './types.js'

/**
 * staqly API client.
 *
 * ```ts
 * const staqly = new Staqly();                // local mode: the access token from STAQLY_API_KEY
 * const cloud = new Staqly({ baseUrl: 'https://staqly.example.com', apiKey: 'stk_...' })
 * ```
 */
export class Staqly {
  readonly collections: Collections
  readonly posts: Posts
  readonly channels: Channels
  readonly providers: Providers
  readonly schedules: Schedules
  readonly jobs: Jobs
  readonly work: Work
  readonly media: MediaResource
  readonly events: Events
  readonly webhooks: Webhooks
  readonly alerts: Alerts
  readonly analytics: AnalyticsResource
  readonly workspace: WorkspaceResource
  readonly workspaces: Workspaces
  readonly plans: Plans
  readonly apiKeys: APIKeys
  readonly billing: BillingResource
  readonly exports: Exports
  readonly sessions: Sessions
  readonly deviceLinks: DeviceLinks
  /** Low-level HTTP client (retries, auth, error mapping); use `request` for endpoints without a helper. */
  readonly http: HTTPClient

  constructor(options: ClientOptions = {}) {
    this.http = new HTTPClient(options)
    this.collections = new Collections(this.http)
    this.posts = new Posts(this.http)
    this.channels = new Channels(this.http)
    this.providers = new Providers(this.http)
    this.schedules = new Schedules(this.http)
    this.jobs = new Jobs(this.http)
    this.work = new Work(this.http)
    this.media = new MediaResource(this.http)
    this.events = new Events(this.http)
    this.webhooks = new Webhooks(this.http)
    this.alerts = new Alerts(this.http)
    this.analytics = new AnalyticsResource(this.http)
    this.workspace = new WorkspaceResource(this.http)
    this.workspaces = new Workspaces(this.http)
    this.plans = new Plans(this.http)
    this.apiKeys = new APIKeys(this.http)
    this.billing = new BillingResource(this.http)
    this.exports = new Exports(this.http)
    this.sessions = new Sessions(this.http)
    this.deviceLinks = new DeviceLinks(this.http)
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
}
