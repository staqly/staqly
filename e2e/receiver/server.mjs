// Fake webhook endpoint for the e2e suite (service `receiver` in
// compose.e2e.yml). It answers 500 to the first delivery and 200 afterwards,
// so the retry path is exercised, and records every delivery.
//
//   POST /<anything>  record the request (headers + raw body)
//   GET  /requests    the recorded requests as JSON
//   POST /reset       forget them and fail the next delivery again
import { createServer } from 'node:http'

let requests = []
let failNext = true

const server = createServer((req, res) => {
  const chunks = []

  req.on('data', (chunk) => chunks.push(chunk))
  req.on('end', () => {
    if (req.method === 'GET' && req.url === '/requests') {
      res.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify(requests))

      return
    }

    if (req.method === 'POST' && req.url === '/reset') {
      requests = []
      failNext = true
      res.writeHead(204).end()

      return
    }

    if (req.method !== 'POST') {
      res.writeHead(405).end()

      return
    }

    const status = failNext ? 500 : 200

    failNext = false
    requests.push({ path: req.url, status, headers: req.headers, body: Buffer.concat(chunks).toString('utf8') })
    res.writeHead(status).end()
  })
})

server.listen(9000, '0.0.0.0', () => console.log('receiver listening on :9000'))
process.on('SIGTERM', () => server.close(() => process.exit(0)))
