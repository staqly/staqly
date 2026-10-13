import { WaitTimeoutError } from '../errors.js'
import type {
  ActionDef,
  ActionInput,
  BulkActionResult,
  CreateItemInput,
  EventPage,
  Item,
  ItemListParams,
  ItemPage,
  ItemVersion,
  ItemWaitResult,
  PurgeResult,
  ReportResultInput,
  RequestOptions,
  UpdateItemInput,
} from '../types.js'
import { APIResource, enc, paginate } from './base.js'
import type { Collections } from './collections.js'
import { filenameOf, toBlob, type Uploadable, type UploadOptions } from './media.js'

export interface WaitOptions {
  /** Give up after this many milliseconds (default 10 minutes). */
  timeout?: number
  /** Stop waiting early. */
  signal?: AbortSignal
  /** Called with the current item after every long-poll round (progress logging). */
  onPoll?: (item: Item) => void
}

export const DEFAULT_WAIT_TIMEOUT_MS = 10 * 60_000
/** Longest single long-poll the server accepts (seconds). */
export const MAX_LONG_POLL_SECONDS = 60

/** Items are the content agents produce and humans review. */
export class Items extends APIResource {
  constructor(
    http: ConstructorParameters<typeof APIResource>[0],
    private readonly collections: Collections,
  ) {
    super(http)
  }

  /** Lists one page of items across collections (or one, via `collection`/`collection_id`). */
  async list(params: ItemListParams = {}, options?: RequestOptions): Promise<ItemPage> {
    const { collection, ...rest } = params
    const query = { ...rest }

    if (collection) {
      query.collection_id = await this.collections.resolveId(collection, options)
    }

    return this.http.request({ method: 'GET', path: '/v1/items', query, options })
  }

  /** Full-text search (prefix match over title, content and metadata); one page, best match first. */
  search(q: string, params: Omit<ItemListParams, 'q'> = {}, options?: RequestOptions): Promise<ItemPage> {
    return this.list({ ...params, q }, options)
  }

  /** Iterates over every matching item across pages (`for await (const item of …)`). */
  async *iterate(params: ItemListParams = {}, options?: RequestOptions): AsyncGenerator<Item, void, undefined> {
    const { collection, ...rest } = params

    if (collection) {
      rest.collection_id = await this.collections.resolveId(collection, options)
    }

    yield* paginate((p) => this.list(p, options), rest)
  }

  get(id: string, options?: RequestOptions): Promise<Item> {
    return this.http.request({ method: 'GET', path: `/v1/items/${enc(id)}`, options })
  }

  /**
   * Creates an item in a collection (ID or exact name). New items are
   * `pending` (awaiting review) unless `status` says otherwise; approval rules
   * may decide them immediately.
   */
  create(collection: string, input: CreateItemInput, options?: RequestOptions): Promise<Item> {
    return this.http.request({
      method: 'POST',
      path: `/v1/collections/${enc(collection)}/items`,
      body: input,
      options,
    })
  }

  /**
   * Uploads a file and creates the item in one multipart request. The type
   * (image/video/file) is inferred from the content.
   */
  upload(
    collection: string,
    file: Uploadable,
    input: CreateItemInput & UploadOptions = {},
    options?: RequestOptions,
  ): Promise<Item> {
    const { filename, contentType, ...fields } = input
    const form = new FormData()

    form.set('item', JSON.stringify(fields))
    form.set('file', toBlob(file, contentType), filenameOf(file, filename))

    return this.http.request({ method: 'POST', path: `/v1/collections/${enc(collection)}/items`, form, options })
  }

  /**
   * Updates fields, moves the item or changes its status (not to
   * approved/rejected — use `approve`/`reject`). `metadata_patch` merges keys.
   */
  update(id: string, input: UpdateItemInput, options?: RequestOptions): Promise<Item> {
    return this.http.request({ method: 'PATCH', path: `/v1/items/${enc(id)}`, body: input, options })
  }

  /** Deletes the item (its history is kept). */
  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/items/${enc(id)}`, options })
  }

  /**
   * Deletes the item **and its history** (events, webhook deliveries, unused
   * media); only a content-free `item.purged` event remains. Needs `admin`.
   */
  purge(id: string, options?: RequestOptions): Promise<PurgeResult> {
    return this.http.request({ method: 'POST', path: `/v1/items/${enc(id)}/purge`, options })
  }

  /** Approves a pending item (needs `items:review`). Optionally edit it in the same step. */
  approve(id: string, input: ActionInput = {}, options?: RequestOptions): Promise<Item> {
    return this.action(id, 'approve', input, options)
  }

  /** Rejects a pending item, usually with `feedback` for the agent. */
  reject(id: string, input: ActionInput = {}, options?: RequestOptions): Promise<Item> {
    return this.action(id, 'reject', input, options)
  }

  /** Runs `approve`, `reject` or a collection-defined action (e.g. `regenerate`, `publish`). */
  action(id: string, name: string, input: ActionInput = {}, options?: RequestOptions): Promise<Item> {
    return this.http.request({
      method: 'POST',
      path: `/v1/items/${enc(id)}/actions/${enc(name)}`,
      body: input,
      options,
    })
  }

  /** Actions the caller may run on the item in its current status. */
  async listActions(id: string, options?: RequestOptions): Promise<ActionDef[]> {
    return (
      await this.http.request<{ data: ActionDef[] }>({ method: 'GET', path: `/v1/items/${enc(id)}/actions`, options })
    ).data
  }

  /** Runs one action on up to 100 items; each succeeds or fails independently. */
  async bulkAction(
    ids: string[],
    action: string,
    input: Omit<ActionInput, 'expected_version'> = {},
    options?: RequestOptions,
  ): Promise<BulkActionResult[]> {
    const res = await this.http.request<{ data: BulkActionResult[] }>({
      method: 'POST',
      path: '/v1/items/bulk-actions',
      body: { ...input, ids, action },
      options,
    })

    return res.data
  }

  /** Activity history of one item, newest first. */
  history(id: string, params: { limit?: number; cursor?: string } = {}, options?: RequestOptions): Promise<EventPage> {
    return this.http.request({ method: 'GET', path: `/v1/items/${enc(id)}/events`, query: { ...params }, options })
  }

  /** Snapshots of the item before each change. */
  async versions(id: string, options?: RequestOptions): Promise<ItemVersion[]> {
    return (
      await this.http.request<{ data: ItemVersion[] }>({
        method: 'GET',
        path: `/v1/items/${enc(id)}/versions`,
        options,
      })
    ).data
  }

  /**
   * One long-poll round: waits up to `timeout` seconds (server max 60) for the
   * item to leave `pending` and returns `{decided, item}`.
   */
  wait(id: string, params: { timeout?: number } = {}, options?: RequestOptions): Promise<ItemWaitResult> {
    const secs = params.timeout

    return this.http.request({
      method: 'GET',
      path: `/v1/items/${enc(id)}/wait`,
      query: { timeout: secs },
      // the HTTP timeout must outlast the server-side wait
      options: { ...options, timeout: Math.max(options?.timeout ?? 0, ((secs ?? 30) + 15) * 1000) },
    })
  }

  /**
   * Waits until the item leaves `pending` — approved, rejected, expired, or
   * moved on by an action or rule — and returns it. Uses the server's
   * long-poll endpoint (no busy polling), re-issuing it until decided.
   * Throws `WaitTimeoutError` after `timeout`. For push instead of waiting,
   * subscribe a webhook to `item.approved` / `item.rejected` / `item.expired`.
   */
  async waitForDecision(id: string, opts: WaitOptions = {}, options?: RequestOptions): Promise<Item> {
    const timeout = opts.timeout ?? DEFAULT_WAIT_TIMEOUT_MS
    const signal = opts.signal ?? options?.signal
    const started = Date.now()
    const deadline = started + timeout

    for (;;) {
      const remaining = deadline - Date.now()
      const secs = Math.min(MAX_LONG_POLL_SECONDS, Math.max(1, Math.ceil(remaining / 1000)))
      const res = await this.wait(id, { timeout: secs }, { ...options, signal })

      opts.onPoll?.(res.item)

      if (res.decided || res.item.status !== 'pending') {
        return res.item
      }

      if (Date.now() >= deadline) {
        throw new WaitTimeoutError(id, Math.round((Date.now() - started) / 1000), res.item)
      }
    }
  }

  /**
   * Reports what happened after acting on an approved (or processing) item:
   * `completed` with an optional JSON `result` (stored in `metadata.result`)
   * or `failed` with an `error`. 409 unless the item is approved/processing.
   */
  reportResult(id: string, input: ReportResultInput, options?: RequestOptions): Promise<Item> {
    return this.http.request({ method: 'POST', path: `/v1/items/${enc(id)}/result`, body: input, options })
  }
}
