// A tiny REST client for seeding and for changing data behind the browser's
// back (live updates, 409 conflicts). Local mode wants its access token, cloud mode an API key.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { ROOT } from './stack.js'

export class Api {
  constructor(
    readonly base: string,
    readonly key = '',
  ) {}

  withKey(key: string): Api {
    return new Api(this.base, key)
  }

  async raw(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<Response> {
    const init: RequestInit = { method, headers: { ...headers } }
    const h = init.headers as Record<string, string>

    if (this.key) {
      h.Authorization = `Bearer ${this.key}`
    }

    if (body instanceof FormData) {
      init.body = body
    } else if (body !== undefined) {
      h['Content-Type'] = 'application/json'
      init.body = JSON.stringify(body)
    }

    return fetch(`${this.base}${path}`, init)
  }

  async req<T = Record<string, unknown>>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await this.raw(method, path, body)
    const text = await res.text()

    if (!res.ok) {
      throw new Error(`${method} ${path}: ${res.status} ${text}`)
    }

    return (text ? JSON.parse(text) : {}) as T
  }

  get = <T = Record<string, unknown>>(path: string) => this.req<T>('GET', path)
  post = <T = Record<string, unknown>>(path: string, body?: unknown) => this.req<T>('POST', path, body ?? {})
  patch = <T = Record<string, unknown>>(path: string, body: unknown) => this.req<T>('PATCH', path, body)
  del = (path: string) => this.req('DELETE', path)

  async upload(fixture: string, name = fixture): Promise<string> {
    const form = new FormData()

    form.append('file', new Blob([new Uint8Array(readFileSync(join(ROOT, 'e2e/fixtures', fixture)))]), name)
    const media = await this.req<{ id: string }>('POST', '/v1/media', form)

    return media.id
  }

  collection(body: Record<string, unknown>): Promise<Collection> {
    return this.post<Collection>('/v1/collections', body)
  }

  /** A channel on "your own automation": publishing asks a webhook or an agent and waits for the result. */
  channel(name: string): Promise<Channel> {
    return this.post<Channel>('/v1/channels', { provider: 'custom', name })
  }

  /** Titled with the content's first line unless the body names one, so tests find posts by that line. */
  createPost(collection: string, body: Record<string, unknown>): Promise<Post> {
    const title =
      String(body.content ?? '')
        .split('\n')[0]!
        .trim() || 'Post'

    return this.post<Post>('/v1/posts', { collection, title, ...body })
  }

  getPost(id: string): Promise<Post> {
    return this.get<Post>(`/v1/posts/${id}`)
  }

  publish = (id: string) => this.post<Post>(`/v1/posts/${id}/publish`)
  schedule = (id: string, publishAt: string) => this.post<Post>(`/v1/posts/${id}/schedule`, { publish_at: publishAt })

  /** What your automation reports for a custom target. */
  result = (postId: string, channelId: string, body: Record<string, unknown>) =>
    this.post<Post>(`/v1/posts/${postId}/targets/${channelId}/result`, body)

  /** Waits until every target was handed to the automation (post.publish_requested emitted). */
  async waitRequested(id: string, timeout = 20_000): Promise<Post> {
    const end = Date.now() + timeout

    for (;;) {
      const p = await this.getPost(id)

      if (p.targets.every((t) => t.status === 'published' || (t.status === 'publishing' && t.attempts > 0))) {
        return p
      }

      if (Date.now() > end) {
        throw new Error(`post ${id} was not handed to the automation: ${JSON.stringify(p.targets)}`)
      }

      await new Promise((r) => setTimeout(r, 300))
    }
  }

  /** Publish now and report every target as published, so the post ends published. */
  async publishThrough(id: string, url = 'https://example.com/posts/1'): Promise<Post> {
    await this.publish(id)
    const asked = await this.waitRequested(id)
    let post = asked

    for (const t of asked.targets.filter((x) => x.status === 'publishing')) {
      post = await this.result(id, t.channel_id, { status: 'published', url })
    }

    return post
  }

  /** Publish now and report the (first) target as failed. */
  async publishFailing(id: string, error = 'Mock API: 503 Service Unavailable'): Promise<Post> {
    await this.publish(id)
    const asked = await this.waitRequested(id)

    return this.result(id, asked.targets[0]!.channel_id, { status: 'failed', error })
  }
}

export interface Collection {
  id: string
  name: string
  archived: boolean
}

export interface Channel {
  id: string
  name: string
  provider: string
  status: string
}

export interface Target {
  channel_id: string
  channel_name: string
  status: 'pending' | 'publishing' | 'published' | 'failed'
  attempts: number
  external_url: string
  error: string
}

export interface Post {
  id: string
  collection_id: string
  content: string
  status: 'draft' | 'scheduled' | 'publishing' | 'published' | 'failed'
  publish_at: string | null
  version: number
  tags: string[]
  targets: Target[]
}

export interface Schedule {
  id: string
  name: string
  enabled: boolean
  next_run_at: string | null
}
