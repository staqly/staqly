import type { HTTPClient } from '../core.js'
import type { AIJob, AIStatus, DesignBrand, Media, MediaProject, RequestOptions, StockPage } from '../types.js'
import { APIResource, enc } from './base.js'
import { type Uploadable, toBlob } from './media.js'

export interface MediaProjectInput {
  /** The editor file: a design (JSON, version 1, width, height and pages) or a video/sound timeline. */
  file: Uploadable | Record<string, unknown>
  name?: string
  /** The library files it was made from. */
  source_ids?: string[]
  /** The file it last saved. */
  output_id?: string
  /** The files its pages saved as, in page order (a carousel). */
  page_ids?: string[]
}

export interface AIJobInput {
  operation:
    | 'generate'
    | 'edit'
    | 'inpaint'
    | 'erase'
    | 'expand'
    | 'remove_background'
    | 'replace_background'
    | 'upscale'
    | 'enhance'
    | 'text_to_video'
    | 'image_to_video'
  prompt?: string
  /** PNG, JPEG or WebP, at most 20 MB and 4096 px a side (PNG for upscale). */
  image?: Uploadable
  /** inpaint, erase: a PNG of the picture's size; white marks the area to change. */
  mask?: Uploadable
  width?: number
  height?: number
  /** Videos: seconds, 5 (default) or 10. */
  duration?: 5 | 10
  /** text_to_video (default 16:9). */
  aspect_ratio?: '16:9' | '9:16' | '1:1'
  canvas_width?: number
  canvas_height?: number
  image_x?: number
  image_y?: number
  place_width?: number
  place_height?: number
}

function isUploadable(v: unknown): v is Uploadable {
  return v instanceof Blob || v instanceof ArrayBuffer || ArrayBuffer.isView(v)
}

/** Editable projects of the design editor (and the video and sound editor). */
export class MediaProjects extends APIResource {
  /** The projects, newest first; `media` keeps those made from that file or saved as it. */
  async list(params: { media?: string } = {}, options?: RequestOptions): Promise<MediaProject[]> {
    return (
      await this.http.request<{ data: MediaProject[] }>({
        method: 'GET',
        path: '/v1/media-projects',
        query: { ...params },
        options,
      })
    ).data
  }

  /** Saves a new project. Needs `media:write`. */
  create(input: MediaProjectInput, options?: RequestOptions): Promise<MediaProject> {
    return this.http.request({ method: 'POST', path: '/v1/media-projects', form: projectForm(input), options })
  }

  get(id: string, options?: RequestOptions): Promise<MediaProject> {
    return this.http.request({ method: 'GET', path: `/v1/media-projects/${enc(id)}`, options })
  }

  /** Saves a new version of a project. Needs `media:write`. */
  update(id: string, input: MediaProjectInput, options?: RequestOptions): Promise<MediaProject> {
    return this.http.request({
      method: 'PUT',
      path: `/v1/media-projects/${enc(id)}`,
      form: projectForm(input),
      options,
    })
  }

  /** Deletes a project and its file; the files it was made from stay. Needs `posts:delete`. */
  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/media-projects/${enc(id)}`, options })
  }

  /** The project's editor file (the design as JSON). */
  async file(id: string, options?: RequestOptions): Promise<Uint8Array> {
    // Bytes: JSON today, a binary .gpex for projects saved before 2026-10-25.
    const res = await this.http.request<Response>({
      method: 'GET',
      path: `/v1/media-projects/${enc(id)}/file`,
      raw: true,
      options,
    })

    return new Uint8Array(await res.arrayBuffer())
  }
}

function projectForm(input: MediaProjectInput): FormData {
  const form = new FormData()

  // The server reads the fields before the file, so they go first.
  for (const [key, value] of [
    ['name', input.name],
    ['source_ids', input.source_ids?.join(',')],
    ['output_id', input.output_id],
    ['page_ids', input.page_ids?.join(',')],
  ] as const) {
    if (value !== undefined) {
      form.set(key, value)
    }
  }

  const file = isUploadable(input.file)
    ? toBlob(input.file, 'application/json')
    : new Blob([JSON.stringify(input.file)], { type: 'application/json' })

  form.set('file', file, 'project.json')

  return form
}

/** AI requests that make or change a picture or video; the result is kept in the media library with `accept`. */
export class AIJobs extends APIResource {
  /**
   * Starts an AI request (202 with the queued job). `idempotencyKey` in the options makes a repeated request answer
   * with the earlier job. Needs `media:write`.
   */
  create(input: AIJobInput, options?: RequestOptions): Promise<AIJob> {
    const form = new FormData()

    for (const [key, value] of Object.entries(input)) {
      if (value !== undefined && key !== 'image' && key !== 'mask') {
        form.set(key, String(value))
      }
    }

    for (const key of ['image', 'mask'] as const) {
      const file = input[key]

      if (file !== undefined) {
        form.set(key, toBlob(file), key)
      }
    }

    // Multipart bodies skip the server's idempotency middleware; the AI service matches the key itself.
    const headers = options?.idempotencyKey
      ? { 'Idempotency-Key': options.idempotencyKey, ...options.headers }
      : options?.headers

    return this.http.request({ method: 'POST', path: '/v1/ai/jobs', form, options: { ...options, headers } })
  }

  /** An AI request's state; poll until `status` is succeeded, failed or canceled. */
  get(id: string, options?: RequestOptions): Promise<AIJob> {
    return this.http.request({ method: 'GET', path: `/v1/ai/jobs/${enc(id)}`, options })
  }

  /** Discards an AI request (cancels it while it runs). */
  async discard(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/ai/jobs/${enc(id)}`, options })
  }

  /** The made picture or video's bytes, until it is kept or discarded. */
  async result(id: string, options?: RequestOptions): Promise<Uint8Array> {
    const res = await this.http.request<Response>({
      method: 'GET',
      path: `/v1/ai/jobs/${enc(id)}/result`,
      raw: true,
      options,
    })

    return new Uint8Array(await res.arrayBuffer())
  }

  /** Keeps the result in the media library. */
  accept(id: string, options?: RequestOptions): Promise<Media> {
    return this.http.request({ method: 'POST', path: `/v1/ai/jobs/${enc(id)}/accept`, options })
  }
}

/** The AI tools on the workspace's own provider keys. */
export class AI extends APIResource {
  readonly jobs: AIJobs

  constructor(http: HTTPClient) {
    super(http)
    this.jobs = new AIJobs(http)
  }

  /** Which AI tools work on this server, and the workspace's AI usage this month. */
  status(options?: RequestOptions): Promise<AIStatus> {
    return this.http.request({ method: 'GET', path: '/v1/ai', options })
  }

  /** Rewrites a short text, e.g. `{ text, instruction: 'Make it shorter' }`. Needs `media:write`. */
  async text(body: { text: string; instruction: string }, options?: RequestOptions): Promise<string> {
    const out = await this.http.request<{ text: string }>({ method: 'POST', path: '/v1/ai/text', body, options })

    return out.text
  }
}

/** The workspace's brand kit for the design editor. */
export class Design extends APIResource {
  brand(options?: RequestOptions): Promise<DesignBrand> {
    return this.http.request({ method: 'GET', path: '/v1/design/brand', options })
  }

  /** Replaces the brand kit (colours, fonts, logo media ids). Needs `media:write`. */
  setBrand(brand: DesignBrand, options?: RequestOptions): Promise<DesignBrand> {
    return this.http.request({ method: 'PUT', path: '/v1/design/brand', body: brand, options })
  }
}

/** Stock photos from Pixabay (when the server has a key). */
export class Stock extends APIResource {
  /** Photos, 30 a page (`page` 1 to 16); `configured` false: this server has no Pixabay key. */
  photos(params: { query?: string; page?: number } = {}, options?: RequestOptions): Promise<StockPage> {
    return this.http.request({ method: 'GET', path: '/v1/stock/photos', query: { ...params }, options })
  }

  /** Copies a photo into the media library. Needs `media:write`. */
  use(id: string, options?: RequestOptions): Promise<Media> {
    return this.http.request({ method: 'POST', path: `/v1/stock/photos/${enc(id)}/use`, options })
  }
}
