import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Composer } from '@/components/posts/composer'

import { channel, collection, post, provider } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

// ProseMirror does not type in jsdom: the editor is a plain textarea here (the text conversion has its own
// test in post-text.test.ts, the real editor runs in e2e/ui).
vi.mock('@/components/posts/post-editor', () => ({
  PostEditor: (p: {
    id: string
    value: string
    onChange: (t: string) => void
    placeholder?: string
    labelledBy?: string
    testId?: string
    below?: React.ReactNode
    start?: React.ReactNode
    end?: React.ReactNode
  }) => (
    <>
      <textarea
        id={p.id}
        value={p.value}
        onChange={(e) => p.onChange(e.target.value)}
        placeholder={p.placeholder}
        aria-labelledby={p.labelledBy}
        data-testid={p.testId}
      />
      {p.below}
      {p.start}
      {p.end}
    </>
  ),
}))

const routes = {
  'GET /v1/collections': { data: [collection(), collection({ id: 'col_2', name: 'Blog' })] },
  'GET /v1/channels': {
    data: [channel(), channel({ id: 'ch_2', name: 'Brand on Bluesky', provider: 'bluesky' })],
  },
  'GET /v1/providers': {
    data: [
      provider(),
      provider({
        id: 'bluesky',
        name: 'Bluesky',
        limits: {
          max_chars: 300,
          max_media: 4,
          media_kinds: ['image'],
          needs_title: false,
          needs_link: false,
          needs_media: false,
        },
        settings_fields: [],
      }),
    ],
  },
}

/** Opens the Channels multi-select, picks these channels and closes it. */
async function pickChannels(user: ReturnType<typeof userEvent.setup>, ...ids: string[]) {
  await user.click(await screen.findByRole('button', { name: 'Channels' }))

  for (const id of ids) {
    await user.click(await screen.findByTestId(`pick-channel-${id}`))
  }

  await user.keyboard('{Escape}')
}

describe('composer', () => {
  it('counts characters per picked channel and publishes now with the picked channels', async () => {
    const api = mockApi({
      ...routes,
      'POST /v1/posts': post({ id: 'pst_9', status: 'draft' }),
      'POST /v1/posts/pst_9/publish': post({ id: 'pst_9', status: 'publishing' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    await user.type(screen.getByTestId('post-content'), 'Hello world')
    // The counter shows the tightest limit; its popover lists every channel against its platform's limit.
    expect(screen.getByRole('button', { name: 'Characters per channel' })).toHaveTextContent('11/300')
    await user.click(screen.getByRole('button', { name: 'Characters per channel' }))
    expect(await screen.findByTestId('counters')).toHaveTextContent('11/500')
    expect(screen.getByTestId('counters')).toHaveTextContent('11/300')
    await user.click(screen.getByTestId('publish-now'))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts')[0].body).toMatchObject({
      collection: 'col_1',
      content: 'Hello world',
      status: 'draft',
      publish_at: null,
      targets: [
        { channel_id: 'ch_1', content: null, settings: {} },
        { channel_id: 'ch_2', content: null, settings: {} },
      ],
    })
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_9/publish')).toHaveLength(1))
    expect(await screen.findAllByText(/is being published/)).not.toHaveLength(0)
  })

  it('a per-channel text override and a platform setting travel with that target', async () => {
    const api = mockApi({ ...routes, 'POST /v1/posts': post({ id: 'pst_9' }) })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    // Per-channel text shows from two channels on; with one, the post's text is that channel's.
    expect(screen.queryByTestId('tab-channel-ch_1')).not.toBeInTheDocument()
    await pickChannels(user, 'ch_2')
    await user.type(screen.getByTestId('post-content'), 'Base text')
    await user.click(screen.getByTestId('tab-channel-ch_1'))
    await user.clear(screen.getByTestId('override-ch_1'))
    await user.type(screen.getByTestId('override-ch_1'), 'Fediverse text')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts')[0].body).toMatchObject({
      targets: [
        { channel_id: 'ch_1', content: 'Fediverse text' },
        { channel_id: 'ch_2', content: null },
      ],
    })
  })

  it('adds tags, existing or new, and sends them with the post', async () => {
    const api = mockApi({
      ...routes,
      'GET /v1/tags': { data: [{ tag: 'launch', count: 3 }] },
      'POST /v1/posts': post({ id: 'pst_9' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'Tagged')
    await user.click(screen.getByRole('button', { name: 'Tags' }))
    await user.click(await screen.findByRole('option', { name: '#launch' }))
    await user.type(screen.getByPlaceholderText('Search or add...'), '#Spring Sale')
    await user.click(await screen.findByRole('option', { name: 'Add “spring-sale”' }))
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts')[0].body).toMatchObject({ tags: ['launch', 'spring-sale'] })
  })

  it('adds comments under the post with their delay and sends them as its thread', async () => {
    const api = mockApi({ ...routes, 'POST /v1/posts': post({ id: 'pst_9' }) })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'Launch day')
    await user.click(screen.getByTestId('add-comment'))
    await user.type(screen.getByTestId('thread-content-0'), 'Details in the docs')
    await user.click(screen.getByTestId('add-comment'))
    await user.type(screen.getByTestId('thread-content-1'), 'Ask us anything')
    await user.click(screen.getByRole('button', { name: 'Move comment 2 up' }))
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts')[0].body).toMatchObject({
      thread: [
        { content: 'Ask us anything', media: [], delay_seconds: 0 },
        { content: 'Details in the docs', media: [], delay_seconds: 0 },
      ],
    })
  })

  it('shows field errors from the API under the fields', async () => {
    mockApi({
      ...routes,
      'POST /v1/posts': {
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'validation failed',
            fields: { content: 'too long for Bluesky' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'x')
    await user.click(screen.getByTestId('publish-now'))
    expect(await screen.findByText('Too long for Bluesky')).toHaveAttribute('data-slot', 'field-error')
  })
})
