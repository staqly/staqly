import { mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { expect, it } from 'vitest'

import { saveResponse } from '../src/node.js'

it('preserves an existing download when the response stream fails', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'staqly-sdk-'))
  const path = join(dir, 'workspace.zip')
  let reads = 0
  const body = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (reads++ === 0) {
        controller.enqueue(new Uint8Array([1, 2, 3]))
      } else {
        controller.error(new Error('connection interrupted'))
      }
    },
  })

  try {
    await writeFile(path, 'previous backup')
    await expect(saveResponse(new Response(body), path)).rejects.toThrow('connection interrupted')
    expect(await readFile(path, 'utf8')).toBe('previous backup')
    expect(await readdir(dir)).toEqual(['workspace.zip'])
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

it('replaces an existing download only after the complete response is saved', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'staqly-sdk-'))
  const path = join(dir, 'workspace.zip')

  try {
    await writeFile(path, 'previous backup')
    expect(await saveResponse(new Response('new backup'), path)).toBe(10)
    expect(await readFile(path, 'utf8')).toBe('new backup')
    expect(await readdir(dir)).toEqual(['workspace.zip'])
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

it('cancels the response if the target directory cannot be opened', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'staqly-sdk-'))
  let cancelled = false
  const body = new ReadableStream<Uint8Array>({
    cancel() {
      cancelled = true
    },
  })

  try {
    await expect(saveResponse(new Response(body), join(dir, 'missing', 'workspace.zip'))).rejects.toThrow()
    expect(cancelled).toBe(true)
    expect(await readdir(dir)).toEqual([])
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})
