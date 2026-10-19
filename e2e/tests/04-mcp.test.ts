// MCP with local mode's access token: Streamable HTTP (POST /mcp, bearer) and STDIO
// (`mcp-stdio` inside the api container, which opens the database itself and needs no key). The agent's round trip: connect a
// channel on its own automation, write and schedule a post, publish it, see
// the requested work and report the result.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, type Channel, type Post, unique } from '../lib/clients.js'
import { waitForRequested } from '../lib/flow.js'
import { callTool, INITIALIZE, INITIALIZED, mcpHttp, mcpStdio, toolCall } from '../lib/mcp.js'
import { LOCAL_TOKEN } from '../lib/stack.js'

// Update when tools are added or removed (apps/api/internal/mcpserver).
const TOOL_COUNT = 100

describe('MCP', () => {
  const agent = api()
  let collection: string

  beforeAll(async () => {
    collection = (await agent.collection({ name: unique('MCP') })).name
  })

  it('initializes over HTTP', async () => {
    const { status, answer } = await mcpHttp(INITIALIZE)

    expect(status).toBe(200)
    expect(answer?.result).toMatchObject({ serverInfo: { name: 'staqly' } })
  })

  it(`lists ${TOOL_COUNT} tools, with the posts vocabulary and none of the review era`, async () => {
    const { answer } = await mcpHttp({ jsonrpc: '2.0', id: 2, method: 'tools/list' })
    const result = answer?.result as { tools: { name: string }[] } | undefined
    const names = result?.tools.map((t) => t.name) ?? []

    expect(names).toHaveLength(TOOL_COUNT)

    for (const name of [
      'list_providers',
      'connect_channel',
      'create_post',
      'schedule_post',
      'publish_post',
      'report_publish_result',
      'retry_target',
      'list_requested_work',
      'create_schedule',
      'run_schedule',
      'list_jobs',
      'retry_job',
      'create_alert',
      'preview_post',
    ]) {
      expect(names).toContain(name)
    }

    expect(names.filter((n) => /item|watcher|approve|reject|decision/.test(n))).toEqual([])
  })

  it('creates a post over HTTP', async () => {
    const result = await callTool<Post>('create_post', { collection, content: 'via MCP HTTP', tags: ['mcp'] })

    expect(result.isError).toBeFalsy()
    expect(result.structuredContent).toMatchObject({ status: 'draft', content: 'via MCP HTTP', tags: ['mcp'] })
  })

  it('creates a post over STDIO', async () => {
    const answers = await mcpStdio(
      [INITIALIZE, INITIALIZED, toolCall(2, 'create_post', { collection, content: 'via MCP stdio' })],
      2,
    )
    const init = answers.find((a) => a.id === 1)
    const created = answers.find((a) => a.id === 2)?.result as { structuredContent: Post } | undefined

    expect(init?.result).toMatchObject({ serverInfo: { name: 'staqly' } })
    expect(created?.structuredContent.status).toBe('draft')
    expect((await agent.getPost(created!.structuredContent.id)).content).toBe('via MCP stdio')
  })

  it('STDIO needs no key, and takes the access token an agent may pass along as none', async () => {
    const answers = await mcpStdio(
      [INITIALIZE, INITIALIZED, { jsonrpc: '2.0', id: 2, method: 'tools/list' }],
      2,
      30_000,
      {
        STAQLY_API_KEY: LOCAL_TOKEN,
      },
    )

    expect(answers.find((a) => a.id === 2)?.result).toBeDefined()
  })

  it('connect a channel, schedule and publish a post, see the requested work, report the result', async () => {
    const channel = (await callTool<Channel>('connect_channel', { provider: 'custom', name: unique('Agent channel') }))
      .structuredContent

    expect(channel).toMatchObject({ provider: 'custom', status: 'connected' })
    const providers = (await callTool<{ providers: { id: string; status: string }[] }>('list_providers', {}))
      .structuredContent

    expect(providers.providers.find((p) => p.id === 'custom')?.status).toBe('available')
    // A platform that is not wired up yet refuses to connect.
    const soon = providers.providers.find((p) => p.status === 'soon')

    expect(soon).toBeDefined()
    expect((await callTool('connect_channel', { provider: soon!.id, fields: { token: 'x' } })).isError).toBe(true)

    const post = (
      await callTool<Post>('create_post', {
        collection,
        content: 'Round trip\nvia MCP',
        targets: [{ channel_id: channel.id }],
      })
    ).structuredContent
    const at = new Date(Date.now() + 3600_000).toISOString()
    const scheduled = (await callTool<Post>('schedule_post', { post_id: post.id, publish_at: at })).structuredContent

    expect(scheduled.status).toBe('scheduled')
    expect(Date.parse(scheduled.publish_at!)).toBe(Date.parse(at))
    const preview = (
      await callTool<{ targets: { channel_id: string; text: string; ok: boolean }[] }>('preview_post', {
        post_id: post.id,
      })
    ).structuredContent

    expect(preview.targets).toEqual([
      expect.objectContaining({ channel_id: channel.id, text: 'Round trip\nvia MCP', warnings: [], ok: true }),
    ])
    const publishing = (await callTool<Post>('publish_post', { post_id: post.id })).structuredContent

    expect(publishing.status).toBe('publishing')
    await waitForRequested(agent, post.id)
    const work = (
      await callTool<{ work: { kind: string; post?: { id: string }; channel?: { id: string } }[] }>(
        'list_requested_work',
        {},
      )
    ).structuredContent

    expect(work.work.find((w) => w.kind === 'target' && w.post?.id === post.id)?.channel?.id).toBe(channel.id)
    const reported = (
      await callTool<Post>('report_publish_result', {
        post_id: post.id,
        channel_id: channel.id,
        status: 'published',
        url: 'https://example.com/mcp/1',
        external_id: 'mcp-1',
      })
    ).structuredContent

    expect(reported.status).toBe('published')
    expect(reported.targets[0]).toMatchObject({
      status: 'published',
      external_url: 'https://example.com/mcp/1',
      external_id: 'mcp-1',
    })
    expect(
      (await callTool<{ work: unknown[] }>('list_requested_work', {})).structuredContent.work.find(
        (w) => (w as { post?: { id: string } }).post?.id === post.id,
      ),
    ).toBeUndefined()
    const history = (await callTool<{ data: { type: string }[] }>('get_post_history', { post_id: post.id }))
      .structuredContent

    expect(history.data[0]!.type).toBe('post.published')
    // Reporting again is an error the agent can read.
    const again = await callTool('report_publish_result', {
      post_id: post.id,
      channel_id: channel.id,
      status: 'published',
    })

    expect(again.isError).toBe(true)
  })

  it('a schedule asks the agent for posts when nothing waits', async () => {
    const empty = (
      await callTool<{ id: string; name: string }>('create_collection', { name: unique('MCP agent source') })
    ).structuredContent
    const channel = (await callTool<Channel>('connect_channel', { provider: 'custom' })).structuredContent
    const schedule = (
      await callTool<{ id: string; next_run_at: string | null }>('create_schedule', {
        collection: empty.id,
        name: 'Ask the agent',
        kind: 'recurring',
        rule: { times: ['09:00'], weekdays: [1, 2, 3, 4, 5], tz: 'Europe/Warsaw' },
        channels: [channel.id],
        source: 'agent',
      })
    ).structuredContent

    expect(schedule.next_run_at).not.toBeNull()
    const ran = (await callTool<{ last_result: string }>('run_schedule', { schedule_id: schedule.id }))
      .structuredContent

    expect(ran.last_result).toBe('asked for posts')
    const work = (
      await callTool<{ work: { kind: string; schedule?: { id: string }; collection: { id: string } }[] }>(
        'list_requested_work',
        {},
      )
    ).structuredContent

    expect(work.work.find((w) => w.kind === 'posts' && w.schedule?.id === schedule.id)?.collection.id).toBe(empty.id)
    await callTool('create_post', { collection: empty.id, content: 'the agent answers' })
    expect(
      (await callTool<{ last_result: string }>('run_schedule', { schedule_id: schedule.id })).structuredContent
        .last_result,
    ).toBe('published 1')
    const jobs = (await callTool<{ data: { kind: string }[] }>('list_jobs', { kind: 'publish_target', limit: 5 }))
      .structuredContent

    expect(jobs.data.length).toBeGreaterThan(0)
  })
})
