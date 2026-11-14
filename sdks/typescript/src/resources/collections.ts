import type { Collection, CollectionInput, CollectionPostListParams, PostPage, RequestOptions } from '../types.js'
import { APIResource, enc } from './base.js'

export interface CollectionCreateInput extends CollectionInput {
  name: string
}

/** Collections group posts (e.g. "Blog posts") and carry the schedules that publish them. */
export class Collections extends APIResource {
  /** Lists collections (archived ones only with `include_archived`). */
  async list(params: { include_archived?: boolean } = {}, options?: RequestOptions): Promise<Collection[]> {
    const res = await this.http.request<{ data: Collection[] }>({
      method: 'GET',
      path: '/v1/collections',
      query: { include_archived: params.include_archived },
      options,
    })

    return res.data
  }

  /** Gets a collection by ID or exact (case-insensitive) name. */
  get(idOrName: string, options?: RequestOptions): Promise<Collection> {
    return this.http.request({ method: 'GET', path: `/v1/collections/${enc(idOrName)}`, options })
  }

  /**
   * Creates a collection. `processing_timeout_seconds` (60 to 86400) is how long a target on
   * your own automation may wait for a result before it fails; null = the server default.
   */
  create(input: CollectionCreateInput, options?: RequestOptions): Promise<Collection> {
    return this.http.request({ method: 'POST', path: '/v1/collections', body: input, options })
  }

  /** Renames, re-describes, archives or changes the processing timeout of a collection. */
  update(id: string, input: CollectionInput, options?: RequestOptions): Promise<Collection> {
    return this.http.request({ method: 'PATCH', path: `/v1/collections/${enc(id)}`, body: input, options })
  }

  archive(id: string, options?: RequestOptions): Promise<Collection> {
    return this.update(id, { archived: true }, options)
  }

  unarchive(id: string, options?: RequestOptions): Promise<Collection> {
    return this.update(id, { archived: false }, options)
  }

  /**
   * Permanently deletes a collection and all its posts. `confirmName` must
   * equal the collection's name (a guard against deleting the wrong one).
   * Needs the `collections:delete` scope (`collections:write` is not enough).
   */
  async delete(id: string, confirmName: string, options?: RequestOptions): Promise<void> {
    await this.http.request({
      method: 'DELETE',
      path: `/v1/collections/${enc(id)}`,
      query: { confirm: confirmName },
      options,
    })
  }

  /** Lists one page of a collection's posts (ID or exact name). Same filters as `posts.list`. */
  listPosts(idOrName: string, params: CollectionPostListParams = {}, options?: RequestOptions): Promise<PostPage> {
    return this.http.request({
      method: 'GET',
      path: `/v1/collections/${enc(idOrName)}/posts`,
      query: { ...params },
      options,
    })
  }

  /** Returns the collection ID for an ID or name (no request when given an ID). */
  async resolveId(idOrName: string, options?: RequestOptions): Promise<string> {
    if (idOrName.startsWith('col_')) {
      return idOrName
    }

    return (await this.get(idOrName, options)).id
  }
}
