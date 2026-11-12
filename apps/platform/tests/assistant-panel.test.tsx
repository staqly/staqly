import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AssistantPanel } from '@/components/assistant/assistant-panel'
import { qk, usePostsInfinite } from '@/lib/queries'

import { post } from './fixtures'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

const CHAT = 'ach_aaaaaaaaaaaaaaaa'

function openChat(answer: string) {
  localStorage.setItem('staqly.assistant.ws_1.current', CHAT)
  mockApi({
    [`GET /v1/assistant/chats/${CHAT}`]: {
      id: CHAT,
      title: 'Replies',
      messages: [
        { role: 'user', content: 'Summarise the replies' },
        { role: 'assistant', content: answer },
      ],
      pending: null,
    },
    'GET /v1/*': { data: [] },
  })
  renderWithProviders(<AssistantPanel open onOpenChange={() => {}} />, { mode: 'cloud' })
}

describe("links in the assistant's answer", () => {
  // The answer can quote text from outside (a reply, a reviewer's comment): a link styled as one of the app's own
  // must not leave it.
  it.each(['//evil.example/login', '/\\evil.example/login'])('%s is text, not an in-app link', async (href) => {
    openChat(`Your session expired: [Sign in again](${href})`)

    await screen.findByText(/Your session expired/)
    expect(screen.queryByRole('link', { name: 'Sign in again' })).not.toBeInTheDocument()
  })

  it('a path in the app opens in this tab, another site in a new one', async () => {
    openChat('See [your drafts](/posts?status=draft) and [the guide](https://docs.staqly.ai/posts)')

    const inApp = await screen.findByRole('link', { name: 'your drafts' })
    const outside = screen.getByRole('link', { name: 'the guide' })

    expect(inApp).toHaveAttribute('href', '/posts?status=draft')
    expect(inApp).not.toHaveAttribute('target')
    expect(outside).toHaveAttribute('target', '_blank')
    expect(outside).toHaveAttribute('rel', 'noreferrer')
  })
})

/** A posts list on screen with three pages loaded, like the posts table after scrolling. */
function PostsOnScreen() {
  usePostsInfinite({})

  return null
}

const page = (cursor: string) => ({ data: [post({ id: `pst_${cursor}` })], next_cursor: 'more' })

/** Sends one message with a posts list on screen; the server answers the chat request with `answer`. */
async function ask(answer: () => Response) {
  const api = mockApi({
    'GET /v1/posts': { data: [post()], next_cursor: 'more' },
    'GET /v1/session': sessionFixture('cloud'),
    'GET /v1/setup': { initialized: true, mode: 'cloud' },
    'GET /v1/*': { data: [] },
  })
  const json = api.fetchMock.getMockImplementation()!

  vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) =>
    String(input).endsWith('/v1/assistant/chat') ? Promise.resolve(answer()) : json(input, init),
  )

  const client = makeClient()

  client.setQueryData(qk.posts({}), { pages: [page('a'), page('b'), page('c')], pageParams: ['', 'b', 'c'] })
  localStorage.removeItem('staqly.assistant.ws_1.current')
  renderWithProviders(
    <>
      <PostsOnScreen />
      <AssistantPanel open onOpenChange={() => {}} />
    </>,
    { mode: 'cloud', client },
  )

  const user = userEvent.setup()

  await user.type(await screen.findByLabelText('Message to the assistant'), 'Do it{Enter}')

  return api
}

/** The assistant's answer is a stream: each tool that ran, then its last message. */
async function turn(tools: string[]) {
  const events = [
    ...tools.map((tool) => ({ type: 'tool', tool, ok: true })),
    { type: 'message', message: { role: 'assistant', content: 'All done.' } },
  ]
  const api = await ask(
    () =>
      new Response(events.map((e) => `data: ${JSON.stringify(e)}\n\n`).join(''), {
        headers: { 'Content-Type': 'text/event-stream' },
      }),
  )

  await screen.findByText('All done.')
  await new Promise((r) => setTimeout(r, 100))

  return api
}

describe('after the assistant changed something', () => {
  it('a change to something else leaves the loaded pages of posts alone', async () => {
    const api = await turn(['update_webhook', 'create_alert', 'rename_workspace'])

    expect(api.find('GET', '/v1/posts')).toHaveLength(0)
    // What is one request is read again (the workspace's name lives in the session).
    await waitFor(() => expect(api.find('GET', '/v1/session').length).toBeGreaterThan(0))
  })

  it('new or changed posts reload each list once, from its first page', async () => {
    const api = await turn(['create_post', 'update_post', 'schedule_post'])

    await waitFor(() => expect(api.find('GET', '/v1/posts')).toHaveLength(1))
    expect(api.find('GET', '/v1/posts')[0]!.query.get('cursor')).toBeNull()
  })
})

describe('a chat the server refuses', () => {
  it("shows the server's reason (a conversation too long: start a new chat), not a generic failure", async () => {
    await ask(
      () =>
        new Response(
          JSON.stringify({
            error: {
              code: 'validation_failed',
              message: 'a conversation takes at most 1000 messages; start a new chat',
            },
          }),
          { status: 422, headers: { 'Content-Type': 'application/json' } },
        ),
    )

    expect(await screen.findByRole('alert')).toHaveTextContent(
      /a conversation takes at most 1000 messages; start a new chat/i,
    )
    // Nothing to run on this device instead: the limit is the conversation's, not the model's.
    expect(screen.queryByRole('button', { name: /on this device/i })).not.toBeInTheDocument()
  })
})
