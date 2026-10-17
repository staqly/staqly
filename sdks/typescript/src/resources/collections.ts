import type {
  ActionDef,
  ActionFlowInput,
  Collection,
  CollectionInput,
  CollectionItemListParams,
  ItemPage,
  RequestOptions,
} from '../types.js'
import { APIResource, enc } from './base.js'
import { itemQuery } from './items.js'

export interface CollectionCreateInput extends CollectionInput {
  name: string
}

/** Collections group items (e.g. "Instagram posts") and configure their review flow from the action catalog. */
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

  create(input: CollectionCreateInput, options?: RequestOptions): Promise<Collection> {
    return this.http.request({ method: 'POST', path: '/v1/collections', body: input, options })
  }

  /** Renames, re-describes, archives or changes the action flow of a collection. */
  update(id: string, input: CollectionInput, options?: RequestOptions): Promise<Collection> {
    return this.http.request({ method: 'PATCH', path: `/v1/collections/${enc(id)}`, body: input, options })
  }

  /**
   * Sets the collection's flow from the fixed catalog (`regenerate`, `publish`, `retry`,
   * `archive`; approve/reject are built in), e.g.
   * `[{ name: 'regenerate' }, { name: 'publish', auto_run: true }]`. publish and archive are
   * always kept; an unknown name or a status outside an action's options is a 422.
   */
  setActions(id: string, actions: Array<ActionFlowInput | ActionDef>, options?: RequestOptions): Promise<Collection> {
    return this.update(id, { actions }, options)
  }

  archive(id: string, options?: RequestOptions): Promise<Collection> {
    return this.update(id, { archived: true }, options)
  }

  unarchive(id: string, options?: RequestOptions): Promise<Collection> {
    return this.update(id, { archived: false }, options)
  }

  /**
   * Permanently deletes a collection and all its items. `confirmName` must
   * equal the collection's name (a guard against deleting the wrong one).
   */
  async delete(id: string, confirmName: string, options?: RequestOptions): Promise<void> {
    await this.http.request({
      method: 'DELETE',
      path: `/v1/collections/${enc(id)}`,
      query: { confirm: confirmName },
      options,
    })
  }

  /**
   * Lists one page of items in a collection (ID or exact name; a name is
   * resolved with one extra request) via GET /v1/collections/{id}/items.
   * Same filters as `items.list`.
   */
  async listItems(
    idOrName: string,
    params: CollectionItemListParams = {},
    options?: RequestOptions,
  ): Promise<ItemPage> {
    const id = await this.resolveId(idOrName, options)

    return this.http.request({
      method: 'GET',
      path: `/v1/collections/${enc(id)}/items`,
      query: itemQuery(params),
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
