// Raw HTTP for the checks the SDK hides on purpose: status codes, guard
// headers (Origin, Host), cookies and CSRF. node:http instead of fetch,
// because fetch cannot override the Host header.
import { type IncomingHttpHeaders, request as httpRequest } from 'node:http'

export interface HttpInit {
  method?: string
  headers?: Record<string, string>
  /** Sent as JSON with `Content-Type: application/json`. */
  json?: unknown
  /** Sent as multipart/form-data. */
  form?: FormData
  body?: string | Buffer
}

export interface HttpResponse {
  status: number
  headers: IncomingHttpHeaders
  body: Buffer
  text(): string
  json<T = Record<string, unknown>>(): T
}

async function encodeBody(init: HttpInit): Promise<{ body?: Buffer; contentType?: string }> {
  if (init.json !== undefined) {
    return { body: Buffer.from(JSON.stringify(init.json)), contentType: 'application/json' }
  }

  if (init.form) {
    // Let the Fetch API pick the multipart boundary, then send the bytes.
    const req = new Request('http://localhost/', { method: 'POST', body: init.form })

    return { body: Buffer.from(await req.arrayBuffer()), contentType: req.headers.get('content-type') ?? undefined }
  }

  return { body: init.body === undefined ? undefined : Buffer.from(init.body) }
}

export async function http(url: string, init: HttpInit = {}): Promise<HttpResponse> {
  const { body, contentType } = await encodeBody(init)
  const headers: Record<string, string> = {}

  if (contentType) {
    headers['Content-Type'] = contentType
  }

  if (body) {
    headers['Content-Length'] = String(body.length)
  }

  Object.assign(headers, init.headers)

  return new Promise((resolve, reject) => {
    const req = httpRequest(url, { method: init.method ?? (body ? 'POST' : 'GET'), headers }, (res) => {
      const chunks: Buffer[] = []

      res.on('data', (chunk: Buffer) => chunks.push(chunk))
      res.on('error', reject)
      res.on('end', () => {
        const data = Buffer.concat(chunks)

        resolve({
          status: res.statusCode ?? 0,
          headers: res.headers,
          body: data,
          text: () => data.toString('utf8'),
          json: <T>() => JSON.parse(data.toString('utf8')) as T,
        })
      })
    })

    req.on('error', reject)
    req.end(body)
  })
}

/** Keeps the cookies a server sets (name=value only, which is all these tests need). */
export class CookieJar {
  private readonly cookies = new Map<string, string>()

  store(headers: IncomingHttpHeaders): void {
    for (const line of headers['set-cookie'] ?? []) {
      const [pair = ''] = line.split(';')
      const eq = pair.indexOf('=')

      if (eq > 0) {
        this.cookies.set(pair.slice(0, eq).trim(), pair.slice(eq + 1).trim())
      }
    }
  }

  get size(): number {
    return this.cookies.size
  }

  header(): string {
    return [...this.cookies].map(([name, value]) => `${name}=${value}`).join('; ')
  }
}

/**
 * A signed-in client (browser, phone or operator): sends its cookies, the CSRF
 * token and fixed headers such as Origin or Host with every request.
 */
export class Session {
  readonly jar = new CookieJar()
  csrf = ''

  constructor(
    readonly baseUrl: string,
    readonly headers: Record<string, string> = {},
  ) {}

  async request(path: string, init: HttpInit = {}): Promise<HttpResponse> {
    const headers: Record<string, string> = { ...this.headers }

    if (this.jar.size > 0) {
      headers.Cookie = this.jar.header()
    }

    if (this.csrf) {
      headers['X-CSRF-Token'] = this.csrf
    }

    const res = await http(`${this.baseUrl}${path}`, { ...init, headers: { ...headers, ...init.headers } })

    this.jar.store(res.headers)

    return res
  }

  /** Redeems a one-time device link token (dashboard sign-in). */
  async redeemDeviceLink(token: string): Promise<HttpResponse> {
    const res = await this.request('/api/v1/device-links/redeem', { method: 'POST', json: { token } })

    if (res.status === 201) {
      this.csrf = res.json<{ csrf_token: string }>().csrf_token
    }

    return res
  }
}
