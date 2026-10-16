// An agent submits an image over REST; a signed-in browser views and approves
// it through the dashboard's /api proxy (session + CSRF).
import { readFile } from 'node:fs/promises'

import { ConflictError, type Item, type Media } from '@staqly/sdk'
import { fileFromPath } from '@staqly/sdk/node'
import { beforeAll, describe, expect, it } from 'vitest'

import { sdk, signInBrowser, unique } from '../lib/clients.js'
import { http, type Session } from '../lib/http.js'
import { DASHBOARD } from '../lib/stack.js'

const AVATAR = new URL('../fixtures/avatar0.png', import.meta.url)

describe('items and the dashboard', () => {
  const agent = sdk()
  let browser: Session
  let collection: string
  let item: Item

  beforeAll(async () => {
    browser = await signInBrowser()
    collection = (await agent.collections.create({ name: unique('Instagram Avatar') })).id
  })

  it('an agent uploads an image item over REST', async () => {
    item = await agent.items.upload(collection, await fileFromPath(AVATAR.pathname), {
      title: 'E2E avatar',
      content: 'caption',
      metadata: { prompt: 'e2e' },
      source: 'e2e',
    })

    expect(item.status).toBe('pending')
    expect(item.type).toBe('image')
    expect(item.media?.mime_type).toBe('image/png')
    expect(await agent.media.download(item.media!.id)).toEqual(new Uint8Array(await readFile(AVATAR)))
  })

  it('renders the item and inbox pages', async () => {
    expect((await http(`${DASHBOARD}/items/${item.id}`)).status).toBe(200)
    expect((await http(`${DASHBOARD}/inbox`)).status).toBe(200)
  })

  it('serves the item and its image to the browser through the proxy', async () => {
    const res = await browser.request(`/api/v1/items/${item.id}`)

    expect(res.status).toBe(200)
    expect(res.json<Item>().title).toBe('E2E avatar')

    const media = await browser.request(`/api/v1/media/${item.media!.id}`)

    expect(media.status).toBe(200)
    expect(media.headers['content-type']).toBe('image/png')
  })

  it('streams a 12 MB upload through the proxy without truncating it', async () => {
    // A valid PNG header followed by zero padding.
    const big = new Uint8Array(8 + (12 << 20))

    big.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
    const form = new FormData()

    form.set('file', new Blob([big], { type: 'image/png' }), 'big.png')
    const res = await browser.request('/api/v1/media', { method: 'POST', form })

    expect(res.status).toBe(201)
    expect(res.json<Media>().size_bytes).toBe(big.length)
  })

  it('the browser approves it with session + CSRF', async () => {
    const res = await browser.request(`/api/v1/items/${item.id}/actions/approve`, {
      method: 'POST',
      json: { feedback: 'ship it' },
    })

    expect(res.status).toBe(200)
    expect(res.json<Item>()).toMatchObject({ status: 'approved', feedback: 'ship it' })
  })

  it('a second decision conflicts', async () => {
    const err = await agent.items.approve(item.id).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ConflictError)
    expect((err as ConflictError).status).toBe(409)
  })

  it('records the history', async () => {
    const history = await agent.items.history(item.id)

    expect(history.data.map((e) => e.type)).toEqual(['item.approved', 'item.created'])
  })
})
