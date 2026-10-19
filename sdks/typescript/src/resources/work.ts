import type { RequestOptions, WorkItem } from '../types.js'
import { APIResource } from './base.js'

/**
 * What the workspace asks your automation for: targets on your own channels (provider
 * `custom`) that wait for `posts.reportResult`, and schedules of `source: 'agent'` whose last
 * run found nothing to publish and want new posts.
 */
export class Work extends APIResource {
  async list(options?: RequestOptions): Promise<WorkItem[]> {
    return (await this.http.request<{ data: WorkItem[] }>({ method: 'GET', path: '/v1/work', options })).data
  }
}
