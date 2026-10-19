import type { Job, JobListParams, JobPage, JobStats, RequestOptions } from '../types.js'
import { APIResource, enc, paginate } from './base.js'

/** The background queue: publishing, schedule runs and sign-in refreshes, with retries. */
export class Jobs extends APIResource {
  /** One page of jobs, newest first. */
  list(params: JobListParams = {}, options?: RequestOptions): Promise<JobPage> {
    return this.http.request({ method: 'GET', path: '/v1/jobs', query: { ...params }, options })
  }

  iterate(params: JobListParams = {}, options?: RequestOptions): AsyncGenerator<Job, void, undefined> {
    return paginate((p) => this.list(p, options), params)
  }

  stats(options?: RequestOptions): Promise<JobStats> {
    return this.http.request({ method: 'GET', path: '/v1/jobs/stats', options })
  }

  get(id: string, options?: RequestOptions): Promise<Job> {
    return this.http.request({ method: 'GET', path: `/v1/jobs/${enc(id)}`, options })
  }

  /** Queues a failed job again, now (needs `posts:publish`). */
  retry(id: string, options?: RequestOptions): Promise<Job> {
    return this.http.request({ method: 'POST', path: `/v1/jobs/${enc(id)}/retry`, options })
  }

  /** Cancels a queued job. */
  cancel(id: string, options?: RequestOptions): Promise<Job> {
    return this.http.request({ method: 'POST', path: `/v1/jobs/${enc(id)}/cancel`, options })
  }
}
