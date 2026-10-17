// Fake endpoint for the e2e suite (service `receiver` in compose.e2e.yml):
// webhook and ntfy receiver, and an RSS feed for watchers. By default it
// answers 500 to the first POST and 200 afterwards, so the retry path is
// exercised, and records every POST.
//
//   POST /<anything>   record the request (headers + raw body)
//   GET  /requests     the recorded requests as JSON
//   POST /reset        forget them and fail the next POST again
//   POST /fail?n=3     fail the next 3 POSTs (0: accept everything)
//   PUT  /feed         store the request body as the feed
//   GET  /feed.xml     serve the stored feed (application/rss+xml)
import { createServer } from 'node:http'

let requests = []
let failures = 1
let feed = ''

const server = createServer((req, res) => {
  const chunks = []

  req.on('data', (chunk) => chunks.push(chunk))
  req.on('end', () => {
    const url = new URL(req.url, 'http://receiver')
    const body = Buffer.concat(chunks).toString('utf8')

    if (req.method === 'GET' && url.pathname === '/requests') {
      res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(requests))

      return
    }

    if (req.method === 'GET' && url.pathname === '/feed.xml') {
      res.writeHead(200, { 'Content-Type': 'application/rss+xml; charset=utf-8' }).end(feed)

      return
    }

    if (req.method === 'PUT' && url.pathname === '/feed') {
      feed = body
      res.writeHead(204).end()

      return
    }

    if (req.method === 'POST' && url.pathname === '/reset') {
      requests = []
      failures = 1
      res.writeHead(204).end()

      return
    }

    if (req.method === 'POST' && url.pathname === '/fail') {
      failures = Number(url.searchParams.get('n') ?? '1')
      res.writeHead(204).end()

      return
    }

    if (req.method !== 'POST') {
      res.writeHead(404).end()

      return
    }

    const status = failures > 0 ? 500 : 200

    failures = Math.max(0, failures - 1)
    requests.push({ path: req.url, status, headers: req.headers, body })
    res.writeHead(status).end()
  })
})

server.listen(9000, '0.0.0.0', () => console.log('receiver listening on :9000'))
process.on('SIGTERM', () => server.close(() => process.exit(0)))
