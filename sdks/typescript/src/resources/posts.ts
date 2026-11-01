import { sleepWithSignal } from '../core.js'
import { WaitTimeoutError } from '../errors.js'
import type {
  EventPage,
  Post,
  PostInput,
  PostListParams,
  PostPage,
  PostPatch,
  PostComment,
  PostCommentInput,
  PostMetrics,
  PostPreview,
  PreviewLink,
  PurgeResult,
  RequestOptions,
  Tag,
  TargetMetrics,
  TargetResultInput,
} from '../types.js'
import { APIResource, enc, paginate } from './base.js'

export interface WaitOptions {
  /** Give up after this many milliseconds (default 10 minutes). */
  timeout?: number
  /** Delay between checks in milliseconds (default 2000). */
  pollInterval?: number
  /** Stop waiting early. */
  signal?: AbortSignal
  /** Called with the post after every check (progress logging). */
  onPoll?: (post: Post) => void
}

export const DEFAULT_WAIT_TIMEOUT_MS = 10 * 60_000

export interface PostCreateInput extends PostInput {
  /** Collection id or exact name. */
  collection: string
}

/** Posts are what goes out: text, media and the channels they go to. */
export class Posts extends APIResource {
  /** Lists one page of posts across collections (`collection_id` takes an id or an exact name). */
  list(params: PostListParams = {}, options?: RequestOptions): Promise<PostPage> {
    return this.http.request({ method: 'GET', path: '/v1/posts', query: { ...params }, options })
  }

  /** Posts whose content contains these words; one page. */
  search(q: string, params: Omit<PostListParams, 'q'> = {}, options?: RequestOptions): Promise<PostPage> {
    return this.list({ ...params, q }, options)
  }

  /** Iterates over every matching post across pages (`for await (const post of ...)`). */
  iterate(params: PostListParams = {}, options?: RequestOptions): AsyncGenerator<Post, void, undefined> {
    return paginate((p) => this.list(p, options), params)
  }

  get(id: string, options?: RequestOptions): Promise<Post> {
    return this.http.request({ method: 'GET', path: `/v1/posts/${enc(id)}`, options })
  }

  /**
   * Creates a post: a draft unless `status: 'scheduled'` (needs `publish_at` and a target) or
   * `publish_now: true`. Media ids come from `media.upload`; each target is a channel the post
   * goes to. Needs `posts:write`, plus `posts:publish` to schedule or publish.
   */
  create(input: PostCreateInput, options?: RequestOptions): Promise<Post> {
    return this.http.request({ method: 'POST', path: '/v1/posts', body: input, options })
  }

  /**
   * Edits a draft, scheduled or failed post. Fields left out stay as they are; `targets`
   * replaces the whole list; `publish_at: null` clears the time (a scheduled post becomes a
   * draft). `expected_version` makes the edit conditional (409 `post_changed`).
   */
  update(id: string, input: PostPatch, options?: RequestOptions): Promise<Post> {
    return this.http.request({ method: 'PATCH', path: `/v1/posts/${enc(id)}`, body: input, options })
  }

  /**
   * Deletes a draft, scheduled or failed post (its events stay as history). A post that went out (published,
   * publishing, or published on any channel) is history: 409 `ConflictError`. Needs `posts:delete`
   * (`posts:write` is not enough).
   */
  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/posts/${enc(id)}`, options })
  }

  /**
   * Deletes several posts at once (up to 200). Unknown ids, and ids of another workspace, come back in
   * `missing`; posts that went out are history and come back in `kept`. Needs `posts:delete`.
   */
  deleteMany(
    ids: string[],
    options?: RequestOptions,
  ): Promise<{ deleted: string[]; missing: string[]; kept: string[] }> {
    return this.http.request({ method: 'POST', path: '/v1/posts/delete', body: { ids }, options })
  }

  /**
   * Deletes the post **and its history** (events, webhook deliveries, media no other post
   * uses); only a content-free `post.purged` event remains. Needs `admin`.
   */
  purge(id: string, options?: RequestOptions): Promise<PurgeResult> {
    return this.http.request({ method: 'POST', path: `/v1/posts/${enc(id)}/purge`, options })
  }

  /** Sets the publish time and makes the post scheduled (from draft, scheduled or failed). */
  schedule(id: string, publishAt: string | Date, options?: RequestOptions): Promise<Post> {
    return this.http.request({
      method: 'POST',
      path: `/v1/posts/${enc(id)}/schedule`,
      body: { publish_at: typeof publishAt === 'string' ? publishAt : publishAt.toISOString() },
      options,
    })
  }

  /**
   * Publishes now: the post becomes `publishing` and a job publishes each target. It ends
   * `published` when every target is, `failed` when one is not. Needs `posts:publish`.
   */
  publish(id: string, options?: RequestOptions): Promise<Post> {
    return this.http.request({ method: 'POST', path: `/v1/posts/${enc(id)}/publish`, options })
  }

  /** Takes a scheduled post off the schedule; it becomes a draft. */
  cancel(id: string, options?: RequestOptions): Promise<Post> {
    return this.http.request({ method: 'POST', path: `/v1/posts/${enc(id)}/cancel`, options })
  }

  /** Copies content, media, tags, metadata and targets into a new draft. */
  duplicate(id: string, options?: RequestOptions): Promise<Post> {
    return this.http.request({ method: 'POST', path: `/v1/posts/${enc(id)}/duplicate`, options })
  }

  /** Tries a failed target once more, now. */
  retryTarget(id: string, channelId: string, options?: RequestOptions): Promise<Post> {
    return this.http.request({
      method: 'POST',
      path: `/v1/posts/${enc(id)}/targets/${enc(channelId)}/retry`,
      options,
    })
  }

  /** Takes a published post down from its channels (all, or only `channelIds`). */
  unpublish(id: string, input?: { channelIds?: string[] }, options?: RequestOptions): Promise<Post> {
    return this.http.request({
      method: 'POST',
      path: `/v1/posts/${enc(id)}/unpublish`,
      body: input?.channelIds !== undefined ? { channel_ids: input.channelIds } : undefined,
      options,
    })
  }

  /**
   * Closes a target on your own automation (a channel of provider `custom`) that waits for a
   * result: `published` (with `url` / `external_id`) or `failed` (with `error`). 409 unless the
   * target is `publishing`.
   */
  reportResult(id: string, channelId: string, input: TargetResultInput, options?: RequestOptions): Promise<Post> {
    return this.http.request({
      method: 'POST',
      path: `/v1/posts/${enc(id)}/targets/${enc(channelId)}/result`,
      body: input,
      options,
    })
  }

  /** The text each target will carry and what its provider objects to. */
  preview(id: string, options?: RequestOptions): Promise<PostPreview> {
    return this.http.request({ method: 'GET', path: `/v1/posts/${enc(id)}/preview`, options })
  }

  /**
   * The post's numbers per channel, read from each platform that reports them when older than ten minutes
   * (`refresh: true` reads them now). `available: false`: the platform gives none.
   */
  async metrics(id: string, params: { refresh?: boolean } = {}, options?: RequestOptions): Promise<TargetMetrics[]> {
    const res = await this.http.request<{ data: TargetMetrics[] }>({
      method: 'GET',
      path: `/v1/posts/${enc(id)}/metrics`,
      query: params.refresh ? { refresh: true } : {},
      options,
    })

    return res.data
  }

  /** Your own automation sends the numbers of a post it published on a custom channel. */
  reportMetrics(id: string, channelId: string, metrics: PostMetrics, options?: RequestOptions): Promise<TargetMetrics> {
    return this.http.request({
      method: 'PUT',
      path: `/v1/posts/${enc(id)}/targets/${enc(channelId)}/metrics`,
      body: metrics,
      options,
    })
  }

  /** A link to a read-only preview page where anyone with it can comment; `url` is returned only now. */
  createPreviewLink(
    id: string,
    params: { expires_in_days?: number } = {},
    options?: RequestOptions,
  ): Promise<PreviewLink> {
    return this.http.request({ method: 'POST', path: `/v1/posts/${enc(id)}/previews`, body: params, options })
  }

  /** The post's preview links (without their tokens). */
  async previewLinks(id: string, options?: RequestOptions): Promise<PreviewLink[]> {
    const res = await this.http.request<{ data: PreviewLink[] }>({
      method: 'GET',
      path: `/v1/posts/${enc(id)}/previews`,
      options,
    })

    return res.data
  }

  /** Stops a preview link from working. */
  revokePreviewLink(id: string, previewId: string, options?: RequestOptions): Promise<void> {
    return this.http.request({ method: 'DELETE', path: `/v1/posts/${enc(id)}/previews/${enc(previewId)}`, options })
  }

  /** Comments as the team: a reply to a thread (`parent_id`) or a new thread, optionally on a passage. */
  addComment(id: string, input: PostCommentInput, options?: RequestOptions): Promise<PostComment> {
    return this.http.request({ method: 'POST', path: `/v1/posts/${enc(id)}/comments`, body: input, options })
  }

  /** Resolves a feedback thread by its first comment. */
  resolveComment(id: string, commentId: string, options?: RequestOptions): Promise<PostComment> {
    return this.http.request({
      method: 'POST',
      path: `/v1/posts/${enc(id)}/comments/${enc(commentId)}/resolve`,
      options,
    })
  }

  /** Reopens a resolved feedback thread. */
  reopenComment(id: string, commentId: string, options?: RequestOptions): Promise<PostComment> {
    return this.http.request({
      method: 'POST',
      path: `/v1/posts/${enc(id)}/comments/${enc(commentId)}/reopen`,
      options,
    })
  }

  /** The post's feedback threads (reviewers' comments and the team's replies), oldest first: every page. */
  async comments(id: string, options?: RequestOptions): Promise<PostComment[]> {
    const out: PostComment[] = []
    let cursor: string | undefined

    do {
      const res = await this.http.request<{ data: PostComment[]; next_cursor?: string }>({
        method: 'GET',
        path: `/v1/posts/${enc(id)}/comments`,
        query: cursor ? { cursor } : {},
        options,
      })

      out.push(...res.data)
      cursor = res.next_cursor
    } while (cursor)

    return out
  }

  /** Removes a comment; removing a thread's first comment removes its replies. */
  deleteComment(id: string, commentId: string, options?: RequestOptions): Promise<void> {
    return this.http.request({ method: 'DELETE', path: `/v1/posts/${enc(id)}/comments/${enc(commentId)}`, options })
  }

  /** The post's history, newest first. */
  history(id: string, params: { limit?: number; cursor?: string } = {}, options?: RequestOptions): Promise<EventPage> {
    return this.http.request({ method: 'GET', path: `/v1/posts/${enc(id)}/events`, query: { ...params }, options })
  }

  /** The workspace's tags, most used first. */
  async tags(options?: RequestOptions): Promise<Tag[]> {
    return (await this.http.request<{ data: Tag[] }>({ method: 'GET', path: '/v1/tags', options })).data
  }

  /**
   * Polls the post until it is `published` or `failed` and returns it (check `status` and the
   * targets' `error`). Throws `WaitTimeoutError` after `timeout`. For push instead of waiting,
   * subscribe a webhook to `post.published` / `post.failed`.
   */
  async waitForPublished(id: string, opts: WaitOptions = {}, options?: RequestOptions): Promise<Post> {
    const timeout = opts.timeout ?? DEFAULT_WAIT_TIMEOUT_MS
    const interval = Math.max(100, opts.pollInterval ?? 2_000)
    const signal = opts.signal ?? options?.signal
    const started = Date.now()
    const deadline = started + timeout

    for (;;) {
      const post = await this.get(id, { ...options, signal })

      opts.onPoll?.(post)

      if (post.status === 'published' || post.status === 'failed') {
        return post
      }

      const remaining = deadline - Date.now()

      if (remaining <= 0) {
        throw new WaitTimeoutError(id, Math.round((Date.now() - started) / 1000), post)
      }

      await sleepWithSignal(Math.min(interval, remaining), signal)
    }
  }
}
