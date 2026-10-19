// A loopback HTTP server the seeded webhooks and alerts talk to: /ok answers
// 200, /fail 500, /slow never answers in time, /posts answers an empty posts
// list for a schedule's endpoint source, anything else (ntfy pushes) 200.
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'

export interface Receiver {
  url: string
  hits: { path: string; body: string }[]
  close: () => Promise<void>
}

export async function startReceiver(port = 0): Promise<Receiver> {
  const hits: Receiver['hits'] = []
  const server: Server = createServer((req, res) => {
    let body = ''

    req.on('data', (d: Buffer) => (body += d.toString()))
    req.on('end', () => {
      const path = req.url ?? '/'

      hits.push({ path, body })

      if (path.startsWith('/fail')) {
        res.writeHead(500, { 'Content-Type': 'text/plain' }).end('receiver says no')
      } else if (path.startsWith('/slow')) {
        setTimeout(() => res.writeHead(200).end('late'), 30_000)
      } else if (path.startsWith('/posts')) {
        res.writeHead(200, { 'Content-Type': 'application/json' }).end('{"posts":[]}')
      } else {
        res.writeHead(200, { 'Content-Type': 'application/json' }).end('{"ok":true}')
      }
    })
  })

  await new Promise<void>((r) => server.listen(port, '127.0.0.1', r))
  port = (server.address() as AddressInfo).port

  return {
    url: `http://127.0.0.1:${port}`,
    hits,
    close: () =>
      new Promise<void>((r) => {
        server.closeAllConnections()
        server.close(() => r())
      }),
  }
}
