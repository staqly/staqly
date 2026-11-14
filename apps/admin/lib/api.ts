export class AdminApiError extends Error {
  status: number
  code: string
  fields: Record<string, string>

  constructor(status: number, code: string, message: string, fields: Record<string, string> = {}) {
    super(message)
    this.name = 'AdminApiError'
    this.status = status
    this.code = code
    this.fields = fields
  }
}

// errorText: the per-field messages of a 422 validation_failed ("plan: unknown plan"), else the error's message.
export function errorText(e: Error): string {
  const f = e instanceof AdminApiError ? Object.entries(e.fields) : []

  return f.length > 0 ? f.map(([k, v]) => `${k}: ${v}`).join('; ') : e.message
}

let csrf: string | null = null

async function csrfToken(): Promise<string> {
  if (csrf === null) {
    const me = await request<{ csrf_token: string }>('GET', '/v1/admin/me')

    csrf = me.csrf_token
  }

  return csrf
}

async function request<T>(method: string, path: string, body?: unknown, token?: string): Promise<T> {
  const headers: Record<string, string> = {}

  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }

  if (token) {
    headers['X-CSRF-Token'] = token
  }

  const res = await fetch(`/api${path}`, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (res.ok) {
    return res.status === 204 ? (undefined as T) : ((await res.json()) as T)
  }

  const env = (await res.json().catch(() => null)) as {
    error?: { code?: string; message?: string; fields?: Record<string, string> }
  } | null
  const err = new AdminApiError(
    res.status,
    env?.error?.code ?? (res.status === 401 ? 'unauthorized' : 'error'),
    env?.error?.message ?? res.statusText,
    env?.error?.fields,
  )

  if (res.status === 401) {
    csrf = null

    if (typeof window !== 'undefined' && window.location.pathname !== '/sign-in') {
      window.location.assign('/sign-in')
    }
  }

  throw err
}

async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  if (method === 'GET') {
    return request<T>(method, path, body)
  }

  try {
    return await request<T>(method, path, body, await csrfToken())
  } catch (e) {
    if (e instanceof AdminApiError && e.status === 403 && e.code === 'csrf_failed') {
      csrf = null

      return request<T>(method, path, body, await csrfToken())
    }

    throw e
  }
}

export const adminApi = {
  get: <T>(path: string) => send<T>('GET', path),
  send,
  // The sign-in calls have no session yet, so no CSRF token (and /me would answer 401).
  anon: <T>(path: string, body: unknown) => request<T>('POST', path, body),
}
