// MCP, keyless from this machine: Streamable HTTP (POST /mcp) and STDIO
// (`mcp-stdio` inside the api container).
import type { Item } from '@staqly/sdk'
import { beforeAll, describe, expect, it } from 'vitest'

import { sdk, unique } from '../lib/clients.js'
import { callTool, INITIALIZE, INITIALIZED, mcpHttp, mcpStdio, toolCall } from '../lib/mcp.js'
import { sleep } from '../lib/wait.js'

// Update when tools are added or removed (apps/api/internal/mcp).
const TOOL_COUNT = 71

describe('MCP', () => {
  const agent = sdk()
  let collection: string

  beforeAll(async () => {
    collection = (await agent.collections.create({ name: unique('MCP') })).name
  })

  it('initializes over HTTP', async () => {
    const { status, answer } = await mcpHttp(INITIALIZE)

    expect(status).toBe(200)
    expect(answer?.result).toMatchObject({ serverInfo: { name: 'staqly' } })
  })

  it(`lists ${TOOL_COUNT} tools`, async () => {
    const { answer } = await mcpHttp({ jsonrpc: '2.0', id: 2, method: 'tools/list' })
    const result = answer?.result as { tools: { name: string }[] } | undefined

    expect(result?.tools).toHaveLength(TOOL_COUNT)
  })

  it('creates an item over HTTP', async () => {
    const result = await callTool<Item>('create_item', { collection, title: 'via MCP HTTP', content: 'hello' })

    expect(result.isError).toBeFalsy()
    expect(result.structuredContent).toMatchObject({ status: 'pending', title: 'via MCP HTTP' })
  })

  it('creates an item over STDIO', async () => {
    const answers = await mcpStdio(
      [INITIALIZE, INITIALIZED, toolCall(2, 'create_item', { collection, title: 'via MCP stdio' })],
      2,
    )
    const init = answers.find((a) => a.id === 1)
    const created = answers.find((a) => a.id === 2)?.result as { structuredContent: Item } | undefined

    expect(init?.result).toMatchObject({ serverInfo: { name: 'staqly' } })
    expect(created?.structuredContent.status).toBe('pending')
    expect((await agent.items.get(created!.structuredContent.id)).title).toBe('via MCP stdio')
  })

  it('await_decision returns when a human approves', async () => {
    const gate = (await callTool<Item>('create_item', { collection, title: 'gate me' })).structuredContent
    const waiting = callTool<{ decided: boolean; item: Item }>('await_decision', {
      item_id: gate.id,
      timeout_seconds: 20,
    })

    await sleep(1000)
    await agent.items.approve(gate.id)
    const { structuredContent } = await waiting

    expect(structuredContent.decided).toBe(true)
    expect(structuredContent.item.status).toBe('approved')
  })
})
