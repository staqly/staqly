// Full-text search, and connecting a phone on the LAN: it reaches the
// dashboard with a non-local Host and is refused until it redeems a QR link,
// which works exactly once.
import type { Item } from '@staqly/sdk'
import { beforeAll, describe, expect, it } from 'vitest'

import { sdk, signInBrowser, unique } from '../lib/clients.js'
import { Session } from '../lib/http.js'
import { DASHBOARD } from '../lib/stack.js'

describe('full-text search', () => {
  const agent = sdk()

  it('finds an item by a word prefix', async () => {
    const collection = await agent.collections.create({ name: unique('Search') })
    const item = await agent.items.create(collection.id, { title: 'Quokka avatar', content: 'smiling' })
    const found = await agent.items.search('quokk')

    expect(found.data.map((i) => i.id)).toContain(item.id)
    expect((await agent.items.search('avat', { collection_id: collection.id })).data[0]?.title).toBe('Quokka avatar')
  })
})

describe('connect a device', () => {
  const phoneHost = { Host: '192.168.1.20:18080' }
  let browser: Session
  let phone: Session
  let item: Item
  let token: string

  beforeAll(async () => {
    const agent = sdk()
    const collection = await agent.collections.create({ name: unique('Devices') })

    item = await agent.items.create(collection.id, { title: 'Archive me from the phone' })
    browser = await signInBrowser()
    phone = new Session(DASHBOARD, phoneHost)
  })

  it('refuses a phone that is not connected', async () => {
    expect((await phone.request('/api/v1/collections')).status).toBe(403)
  })

  it('the browser creates a QR link and the phone redeems it', async () => {
    const res = await browser.request('/api/v1/device-links', { method: 'POST', json: {} })

    expect(res.status).toBe(201)
    token = res.json<{ url: string }>().url.split('#')[1]!

    expect((await phone.redeemDeviceLink(token)).status).toBe(201)
  })

  it('the connected phone reads and acts', async () => {
    expect((await phone.request('/api/v1/collections')).status).toBe(200)

    const archived = await phone.request(`/api/v1/items/${item.id}/actions/archive`, { method: 'POST', json: {} })

    expect(archived.status).toBe(200)
    expect(archived.json<Item>().status).toBe('archived')
  })

  it('the QR link works only once', async () => {
    const other = new Session(DASHBOARD, phoneHost)

    expect((await other.redeemDeviceLink(token)).status).toBe(410)
  })
})
