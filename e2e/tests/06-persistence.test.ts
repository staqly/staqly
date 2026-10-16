// Data lives in the staqly-data volume: it survives a restart and the
// re-creation of the containers.
import { readFile } from 'node:fs/promises'

import type { Item } from '@staqly/sdk'
import { fileFromPath } from '@staqly/sdk/node'
import { beforeAll, describe, expect, it } from 'vitest'

import { sdk, unique } from '../lib/clients.js'
import { API, compose, waitForHttp, waitForStack } from '../lib/stack.js'

const AVATAR = new URL('../fixtures/avatar1.png', import.meta.url)

async function allItemIds(): Promise<string[]> {
  const ids: string[] = []

  for await (const item of sdk().items.iterate({ limit: 100 })) {
    ids.push(item.id)
  }

  return ids.toSorted()
}

describe('persistence', () => {
  const agent = sdk()
  let before: string[]
  let image: Item

  beforeAll(async () => {
    const collection = await agent.collections.create({ name: unique('Persistence') })

    image = await agent.items.upload(collection.id, await fileFromPath(AVATAR.pathname), { title: 'Keep me' })
    before = await allItemIds()
  })

  it('keeps items and media across a restart', async () => {
    await compose(['restart', 'staqly'])
    await waitForHttp(`${API}/health`)

    expect(await allItemIds()).toEqual(before)
    expect(await agent.media.download(image.media!.id)).toEqual(new Uint8Array(await readFile(AVATAR)))
  })

  it('keeps items and media when the containers are re-created', async () => {
    await compose(['up', '-d', '--wait', '--no-build', '--force-recreate', 'staqly'])
    await waitForStack()

    expect(await allItemIds()).toEqual(before)
    expect(before).toContain(image.id)
    expect(await agent.media.download(image.media!.id)).toEqual(new Uint8Array(await readFile(AVATAR)))
  })
})
