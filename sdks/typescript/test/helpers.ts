import { Staqly } from '../src/index.js'

export interface Call {
  method: string
  url: URL
  path: string
  headers: Headers
  body: unknown
  form?: FormData
}

type Reply = Response | ((call: Call) => Response | Promise<Response>)

export function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  })
}

export function apiError(status: number, code: string, message = code, extra: Record<string, unknown> = {}) {
  return json({ error: { code, message, ...extra } }, status)
}

/** A fetch double that records calls and answers from a queue (the last reply repeats). */
export function mockFetch(...replies: Reply[]) {
  const calls: Call[] = []
  const fetch = async (input: string | URL | Request, init: RequestInit = {}) => {
    const url = new URL(String(input))
    const headers = new Headers(init.headers)
    let body: unknown
    let form: FormData | undefined

    if (init.body instanceof FormData) {
      form = init.body
    } else if (typeof init.body === 'string') {
      body = JSON.parse(init.body)
    }

    const call: Call = { method: init.method ?? 'GET', url, path: url.pathname, headers, body, form }

    calls.push(call)

    if (init.signal?.aborted) {
      throw new DOMException('aborted', 'AbortError')
    }

    const reply = replies.length > 1 ? replies.shift()! : replies[0]

    if (!reply) {
      throw new Error('no reply queued')
    }

    return typeof reply === 'function' ? reply(call) : reply.clone()
  }

  return { fetch, calls }
}

export function client(...replies: Reply[]) {
  const m = mockFetch(...replies)
  const c = new Staqly({ baseUrl: 'http://api.test', apiKey: 'stk_test', fetch: m.fetch, maxRetries: 2 })

  return { c, calls: m.calls }
}

export function target(overrides: Record<string, unknown> = {}) {
  return {
    channel_id: 'ch_1',
    channel_name: 'Brand',
    provider: 'mastodon',
    avatar_url: '',
    content: null,
    settings: {},
    status: 'pending',
    external_id: '',
    external_url: '',
    error: '',
    attempts: 0,
    published_at: null,
    replies: [],
    ...overrides,
  }
}

export function post(overrides: Record<string, unknown> = {}) {
  return {
    id: 'pst_1',
    collection_id: 'col_1',
    collection_name: 'Posts',
    content: 'Launch day',
    media: [],
    media_items: [],
    status: 'draft',
    publish_at: null,
    published_at: null,
    tags: [],
    metadata: {},
    source: 'sdk',
    external_ref: '',
    repeat_every: '',
    thread: [],
    version: 1,
    targets: [target()],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}
