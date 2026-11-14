// A loopback HTTP server the seeded webhooks and alerts talk to: /ok answers
// 200, /fail 500, /slow never answers in time, /posts answers an empty posts
// list for a schedule's endpoint source, anything else (ntfy pushes) 200.
// /fal/... plays fal.ai's queue API for the local server's AI tools (AI_FAL_BASE_URL): every request completes
// at once with a small picture (nothing billable is ever called).
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'

export interface Receiver {
  url: string
  hits: { path: string; body: string }[]
  close: () => Promise<void>
}

// A 4 x 3 PNG: what the fake AI makes.
const AI_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAQAAAADCAIAAAA7ljmRAAAAEElEQVR4nGM4YVMBRww4OQBfThHR5zkcVQAAAABJRU5ErkJggg==',
  'base64',
)

export const FAL_E2E_KEY = 'fal-e2e-key-0123456789'

function json(v: unknown, status = 200) {
  return { status, type: 'application/json', body: JSON.stringify(v) }
}

function fakeFal(base: string, method: string, path: string): { status: number; type: string; body: string | Buffer } {
  if (path === '/fal/files/out.png') {
    return { status: 200, type: 'image/png', body: AI_PNG }
  }

  const parts = path.split('/').filter(Boolean) // fal, owner, app, ...
  const at = parts.indexOf('requests')

  if (at < 0 && method === 'POST') {
    const app = `${base}/fal/${parts[1]}/${parts[2]}/requests/req-1`

    return json({ request_id: 'req-1', status_url: `${app}/status`, response_url: app, cancel_url: `${app}/cancel` })
  }

  if (path.endsWith('/status')) {
    return json({ status: 'COMPLETED' })
  }

  if (path.endsWith('/cancel')) {
    return json({ status: 'OK' })
  }

  return json({ images: [{ url: `${base}/fal/files/out.png`, width: 4, height: 3 }], has_nsfw_concepts: [false] })
}

/**
 * The assistant's language model (fal.ai's OpenAI-compatible router, streamed): asked by a person it looks up the
 * collections, then creates a draft in the first one (which waits for Allow), then says it is done.
 */
function fakeAssistant(body: string): { status: number; type: string; body: string } {
  const req = JSON.parse(body) as {
    messages: { role: string; content: string; tool_calls?: { function: { name: string } }[] }[]
  }
  const last = req.messages.at(-1)!
  const prev = req.messages.findLast((m) => m.role === 'assistant' && m.tool_calls?.length)?.tool_calls?.[0]?.function
    .name
  const chunks: unknown[] = []
  const call = (id: string, name: string, args: unknown) =>
    chunks.push({
      choices: [{ delta: { tool_calls: [{ index: 0, id, function: { name, arguments: JSON.stringify(args) } }] } }],
    })

  if (last.role === 'user') {
    chunks.push({ choices: [{ delta: { content: 'Let me check your collections.' } }] })
    call('c1', 'list_collections', {})
  } else if (prev === 'list_collections') {
    const { collections } = JSON.parse(last.content) as { collections: { id: string }[] }

    call('c2', 'create_post', {
      collection: collections[0]!.id,
      title: 'Post',
      content: 'Fresh coffee, fresh week. See you at the cafe!',
    })
  } else if (last.content.startsWith('The person declined')) {
    chunks.push({ choices: [{ delta: { content: 'Okay, I left it.' } }] })
  } else {
    chunks.push({ choices: [{ delta: { content: 'Done: the **draft** is in your collection.' } }] })
  }

  chunks.push({ choices: [], usage: { prompt_tokens: 900, completion_tokens: 40, cost: 0.0003 } })

  return {
    status: 200,
    type: 'text/event-stream',
    body: chunks.map((c) => `data: ${JSON.stringify(c)}\n\n`).join('') + 'data: [DONE]\n\n',
  }
}

export async function startReceiver(port = 0): Promise<Receiver> {
  const hits: Receiver['hits'] = []
  let base = ''
  const server: Server = createServer((req, res) => {
    let body = ''

    req.on('data', (d: Buffer) => (body += d.toString()))
    req.on('end', () => {
      const path = req.url ?? '/'

      hits.push({ path, body })

      if (path.startsWith('/fal/')) {
        const out =
          req.headers.authorization === `Key ${FAL_E2E_KEY}` || path.startsWith('/fal/files/')
            ? path.endsWith('/chat/completions')
              ? fakeAssistant(body)
              : fakeFal(base, req.method ?? 'GET', path)
            : { status: 401, type: 'application/json', body: '{"detail":"Invalid key"}' }

        res.writeHead(out.status, { 'Content-Type': out.type }).end(out.body)
      } else if (path.startsWith('/fail')) {
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
  base = `http://127.0.0.1:${port}`

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
