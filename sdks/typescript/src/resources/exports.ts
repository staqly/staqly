import { sleepWithSignal } from '../core.js'
import { StaqlyError, WaitTimeoutError } from '../errors.js'
import type { EventExportParams, Event as StaqlyEvent, RequestOptions, WorkspaceExport } from '../types.js'
import { APIResource, enc } from './base.js'

export interface ExportWaitOptions {
  /** Give up after this many milliseconds (default 10 minutes). */
  timeout?: number
  /** Delay between status checks in milliseconds (default 1000). */
  pollInterval?: number
  signal?: AbortSignal
}

/** The workspace export job failed or expired before it could be downloaded. */
export class ExportFailedError extends StaqlyError {
  constructor(readonly job: WorkspaceExport) {
    super(`Workspace export ${job.id} is ${job.status}${job.error ? `: ${job.error}` : ''}`)
  }
}

/** Data portability: stream the activity log, or export the whole workspace as a ZIP. */
export class Exports extends APIResource {
  /**
   * Streams every matching event, oldest first (NDJSON, no paging), without
   * buffering the whole log in memory.
   *
   * ```ts
   * for await (const ev of staqly.exports.events({ from: '2026-10-01', type_prefix: 'post.' })) ...
   * ```
   */
  async *events(
    params: EventExportParams = {},
    options?: RequestOptions,
  ): AsyncGenerator<StaqlyEvent, void, undefined> {
    const res = await this.eventsResponse({ ...params, format: 'ndjson' }, options)

    if (!res.body) {
      return
    }

    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
    let buf = ''

    try {
      for (;;) {
        const { value, done } = await reader.read()

        if (value) {
          buf += value
        }

        let nl = buf.indexOf('\n')

        while (nl >= 0) {
          const line = buf.slice(0, nl).trim()

          buf = buf.slice(nl + 1)

          if (line) {
            yield JSON.parse(line) as StaqlyEvent
          }

          nl = buf.indexOf('\n')
        }

        if (done) {
          break
        }
      }

      if (buf.trim()) {
        yield JSON.parse(buf) as StaqlyEvent
      }
    } finally {
      reader.releaseLock()
      await res.body.cancel().catch(() => undefined)
    }
  }

  /**
   * The raw export `Response` (attachment) for streaming to a file or another
   * service. `format: "csv"` gives `created_at,id,type,actor_type,actor_id,actor_name,collection_id,post_id,data`.
   */
  eventsResponse(
    params: EventExportParams & { format?: 'ndjson' | 'csv' } = {},
    options?: RequestOptions,
  ): Promise<Response> {
    return this.http.request({ method: 'GET', path: '/v1/events/export', query: { ...params }, raw: true, options })
  }

  /**
   * Starts a workspace export (admin). On Free it is data only (`includes_media: false`, no
   * media files). Only one can be pending/running at a time (409 with `details.export_id`).
   * Archives are deleted after 24 h.
   */
  createWorkspaceExport(options?: RequestOptions): Promise<WorkspaceExport> {
    return this.http.request({ method: 'POST', path: '/v1/workspace/export', options })
  }

  /** The workspace's latest exports (backups), newest first, at most 20 (admin). */
  async listWorkspaceExports(options?: RequestOptions): Promise<WorkspaceExport[]> {
    return (
      await this.http.request<{ data: WorkspaceExport[] }>({ method: 'GET', path: '/v1/workspace/exports', options })
    ).data
  }

  getWorkspaceExport(id: string, options?: RequestOptions): Promise<WorkspaceExport> {
    return this.http.request({ method: 'GET', path: `/v1/workspace/exports/${enc(id)}`, options })
  }

  /** Polls until the export is `ready`; throws `ExportFailedError` if it fails/expires, `WaitTimeoutError` on timeout. */
  async waitForWorkspaceExport(
    id: string,
    opts: ExportWaitOptions = {},
    options?: RequestOptions,
  ): Promise<WorkspaceExport> {
    const started = Date.now()
    const deadline = started + (opts.timeout ?? 10 * 60_000)
    const interval = Math.max(100, opts.pollInterval ?? 1_000)

    for (;;) {
      const job = await this.getWorkspaceExport(id, { ...options, signal: opts.signal ?? options?.signal })

      if (job.status === 'ready') {
        return job
      }

      if (job.status === 'failed' || job.status === 'expired') {
        throw new ExportFailedError(job)
      }

      const remaining = deadline - Date.now()

      if (remaining <= 0) {
        throw new WaitTimeoutError(id, Math.round((Date.now() - started) / 1000), undefined)
      }

      await sleepWithSignal(Math.min(interval, remaining), opts.signal ?? options?.signal)
    }
  }

  /** The finished archive as a raw `Response` (stream it to disk with `saveResponse` from `@staqly/sdk/node`). */
  workspaceExportResponse(id: string, options?: RequestOptions): Promise<Response> {
    return this.http.request({ method: 'GET', path: `/v1/workspace/exports/${enc(id)}/download`, raw: true, options })
  }

  /** Downloads the finished archive into memory. */
  async downloadWorkspaceExport(id: string, options?: RequestOptions): Promise<Uint8Array> {
    const res = await this.workspaceExportResponse(id, options)

    return new Uint8Array(await res.arrayBuffer())
  }

  /** Starts an export, waits until it is ready and returns the job (then download it). */
  async exportWorkspace(opts: ExportWaitOptions = {}, options?: RequestOptions): Promise<WorkspaceExport> {
    const job = await this.createWorkspaceExport(options)

    return this.waitForWorkspaceExport(job.id, opts, options)
  }
}
