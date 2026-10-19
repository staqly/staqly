import type { RequestOptions, Schedule, ScheduleInput } from '../types.js'
import { APIResource, enc } from './base.js'

export interface ScheduleCreateInput extends ScheduleInput {
  /** Collection id or exact name. */
  collection: string
  name: string
}

/**
 * Publishing schedules: publish a collection's waiting drafts at a time (`kind: 'once'`,
 * `run_at`) or on a cadence (`kind: 'recurring'`, `rule`). When nothing waits, `source: 'agent'`
 * asks an agent (`schedule.posts_requested`, `work.list()`) and `source: 'endpoint'` fetches
 * posts from `endpoint_url`. Needs `schedules:manage` to change.
 */
export class Schedules extends APIResource {
  /** Lists schedules, optionally of one collection (id or exact name). */
  async list(params: { collection_id?: string } = {}, options?: RequestOptions): Promise<Schedule[]> {
    return (
      await this.http.request<{ data: Schedule[] }>({
        method: 'GET',
        path: '/v1/schedules',
        query: { ...params },
        options,
      })
    ).data
  }

  get(id: string, options?: RequestOptions): Promise<Schedule> {
    return this.http.request({ method: 'GET', path: `/v1/schedules/${enc(id)}`, options })
  }

  create(input: ScheduleCreateInput, options?: RequestOptions): Promise<Schedule> {
    return this.http.request({ method: 'POST', path: '/v1/schedules', body: input, options })
  }

  /** Changes a schedule; the next run is recomputed. */
  update(id: string, input: Omit<ScheduleInput, 'collection'>, options?: RequestOptions): Promise<Schedule> {
    return this.http.request({ method: 'PATCH', path: `/v1/schedules/${enc(id)}`, body: input, options })
  }

  enable(id: string, options?: RequestOptions): Promise<Schedule> {
    return this.update(id, { enabled: true }, options)
  }

  disable(id: string, options?: RequestOptions): Promise<Schedule> {
    return this.update(id, { enabled: false }, options)
  }

  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/schedules/${enc(id)}`, options })
  }

  /** Runs the schedule now (a `run_schedule` job); its regular next run is unchanged. */
  run(id: string, options?: RequestOptions): Promise<Schedule> {
    return this.http.request({ method: 'POST', path: `/v1/schedules/${enc(id)}/run`, options })
  }
}
