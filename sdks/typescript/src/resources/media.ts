import type {
  Media,
  MediaBulkInput,
  MediaFolder,
  MediaFolderInput,
  MediaFolders,
  MediaListParams,
  MediaPage,
  MediaPatch,
  MediaUse,
  RequestOptions,
} from '../types.js'
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

  /** The media library, newest first; `next_offset` is where the next page starts. */
  list(params: MediaListParams = {}, options?: RequestOptions): Promise<MediaPage> {
    return this.http.request({ method: 'GET', path: '/v1/media', query: { ...params }, options })
  }

  /** A file's details: name, type, size, alt text, pixels and length. */
  info(id: string, options?: RequestOptions): Promise<Media> {
    return this.http.request({ method: 'GET', path: `/v1/media/${enc(id)}/info`, options })
  }

  /** Renames a file, sets its alt text, or records a video's size and length. Needs `media:write`. */
  update(id: string, patch: MediaPatch, options?: RequestOptions): Promise<Media> {
    return this.http.request({ method: 'PATCH', path: `/v1/media/${enc(id)}`, body: patch, options })
  }

  /** The posts that use a file, newest first (at most 50). */
  async usage(id: string, options?: RequestOptions): Promise<MediaUse[]> {
    const out = await this.http.request<{ data: MediaUse[] }>({
      method: 'GET',
      path: `/v1/media/${enc(id)}/usage`,
      options,
    })

    return out.data
  }

  /**
   * Moves several files to Deleted (or deletes them for good with `permanent`); reports which ids were not
   * found. A file a scheduled post uses refuses the whole call (409). Needs `posts:delete`.
   */
  deleteMany(
    ids: string[],
    opts: { permanent?: boolean } = {},
    options?: RequestOptions,
  ): Promise<{ deleted: string[]; missing: string[] }> {
    const body = opts.permanent ? { ids, permanent: true } : { ids }

    return this.http.request({ method: 'POST', path: '/v1/media/delete', body, options })
  }

  /** Takes a file out of Deleted, back where it was. Needs `posts:delete`. */
  restore(id: string, options?: RequestOptions): Promise<Media> {
    return this.http.request({ method: 'POST', path: `/v1/media/${enc(id)}/restore`, options })
  }

  /**
   * Keeps this file of a set of identical ones: posts that use a copy use it instead, the copies move to Deleted.
   * Needs `posts:delete` and `posts:write`.
   */
  keep(id: string, options?: RequestOptions): Promise<{ deleted: string[] }> {
    return this.http.request({ method: 'POST', path: `/v1/media/${enc(id)}/keep`, options })
  }

  /** Moves files to a folder and/or adds and removes tags, up to 200 at a time. Needs `media:write`. */
  organize(input: MediaBulkInput, options?: RequestOptions): Promise<{ updated: string[]; missing: string[] }> {
    return this.http.request({ method: 'POST', path: '/v1/media/bulk', body: input, options })
  }

  /** The library's folders (a flat list with `parent_id`), file counts and tags with counts. */
  folders(options?: RequestOptions): Promise<MediaFolders> {
    return this.http.request({ method: 'GET', path: '/v1/media-folders', options })
  }

  /** Creates a folder at the top level or inside `parent_id`. Needs `media:write`. */
  createFolder(input: MediaFolderInput & { name: string }, options?: RequestOptions): Promise<MediaFolder> {
    return this.http.request({ method: 'POST', path: '/v1/media-folders', body: input, options })
  }

  /** Renames a folder or moves it under another one (`parent_id: ''` = the top level). */
  updateFolder(id: string, input: MediaFolderInput, options?: RequestOptions): Promise<MediaFolder> {
    return this.http.request({ method: 'PATCH', path: `/v1/media-folders/${enc(id)}`, body: input, options })
  }

  /** Deletes a folder; its files and subfolders move up to its parent (no file is deleted). */
  async deleteFolder(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/media-folders/${enc(id)}`, options })
  }

  /**
   * Moves a file to Deleted, restorable for 30 days (409 while a scheduled post uses it); with `permanent` deletes
   * it for good now and posts referencing it lose it. Needs `posts:delete`.
   */
  async delete(id: string, opts: { permanent?: boolean } = {}, options?: RequestOptions): Promise<void> {
    await this.http.request({
      method: 'DELETE',
      path: `/v1/media/${enc(id)}`,
      query: opts.permanent ? { permanent: 'true' } : undefined,
      options,
    })
  }
}
