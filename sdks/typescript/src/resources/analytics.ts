import type { Analytics, AnalyticsParams, RequestOptions } from '../types.js'
import { APIResource } from './base.js'

/** How publishing went: totals, channels, sources, posting times, top posts and failures. */
export class AnalyticsResource extends APIResource {
  /**
   * The report for a range of days in the workspace's time zone (`from` and `to` both
   * included; default the last 30 days, at most 366). Needs `posts:read`.
   */
  get(params: AnalyticsParams = {}, options?: RequestOptions): Promise<Analytics> {
    return this.http.request({ method: 'GET', path: '/v1/analytics', query: { ...params }, options })
  }
}
