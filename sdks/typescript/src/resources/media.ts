import type { Media, RequestOptions } from '../types.js'
import { APIResource, enc } from './base.js'

/** Anything that can be uploaded. Use `fileFromPath` from `@staqly/sdk/node` for paths. */
export type Uploadable = Blob | ArrayBuffer | ArrayBufferView

export interface UploadOptions {
  /** File name stored with the media (default: the File's name, else "upload"). */
  filename?: string
  /** MIME type hint; the server detects the type from the content anyway. */
  contentType?: string
}

export function toBlob(file: Uploadable, contentType?: string): Blob {
  if (file instanceof Blob) {
    return contentType && !file.type ? new Blob([file], { type: contentType }) : file
  }

  const part = ArrayBuffer.isView(file)
    ? new Uint8Array(file.buffer, file.byteOffset, file.byteLength)
    : new Uint8Array(file)

  return new Blob([part as Uint8Array<ArrayBuffer>], contentType ? { type: contentType } : {})
}

export function filenameOf(file: Uploadable, explicit?: string): string {
  if (explicit) {
    return explicit
  }

  const name = (file as { name?: unknown }).name

  return typeof name === 'string' && name ? name : 'upload'
}

/** Uploaded files (images, videos). Posts reference them by id in `media`. */
export class MediaResource extends APIResource {
  /** Uploads a file. The type is detected from its content; SVG/HTML are rejected (415). */
  upload(file: Uploadable, opts: UploadOptions = {}, options?: RequestOptions): Promise<Media> {
    const form = new FormData()

    form.set('file', toBlob(file, opts.contentType), filenameOf(file, opts.filename))

    return this.http.request({ method: 'POST', path: '/v1/media', form, options })
  }

  /** Downloads a file's bytes. */
  async download(id: string, options?: RequestOptions): Promise<Uint8Array> {
    const res = await this.downloadResponse(id, options)

    return new Uint8Array(await res.arrayBuffer())
  }

  /** Returns the raw `Response` (stream the body, read headers such as Content-Type). */
  downloadResponse(id: string, options?: RequestOptions): Promise<Response> {
    return this.http.request({ method: 'GET', path: `/v1/media/${enc(id)}`, raw: true, options })
  }

  /** Absolute URL of a file (it needs the same authentication as the API). */
  url(id: string): string {
    return this.http.buildURL(`/v1/media/${enc(id)}`)
  }

  /** Deletes a file; posts referencing it lose it. Needs `posts:delete`. */
  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/media/${enc(id)}`, options })
  }
}
