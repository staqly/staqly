import { screen, waitFor, within, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Composer } from '@/components/posts/composer'

import { channel, collection, post, provider, target } from './fixtures'
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
          links: 'keep',
          link_modes: ['keep', 'remove', 'defang'],
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
  it('opened outside a collection, a new post starts in the Default collection', async () => {
    mockApi({
      ...routes,
      'GET /v1/collections': {
        data: [collection(), collection({ id: 'col_default', name: 'Default', is_default: true })],
      },
    })

    renderWithProviders(<Composer open onOpenChange={() => {}} />, { mode: 'local' })
    const select = await screen.findByRole('combobox', { name: /Collection/ })

    await waitFor(() => expect(select).toHaveTextContent('Default'))
  })

  it('Schedule opens the calendar: a quick pick and Schedule send the post for that time', async () => {
    const api = mockApi({ ...routes, 'POST /v1/posts': post({ id: 'pst_9', status: 'scheduled' }) })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-title'), 'Launch day')
    await user.type(screen.getByTestId('post-content'), 'Later')
    // A new post offers both ways out side by side.
    expect(screen.getByTestId('publish-now')).toBeEnabled()
    await user.click(screen.getByRole('button', { name: 'Schedule for later' }))
    const picker = await screen.findByRole('dialog', { name: 'Pick a date and time' })

    await user.click(within(picker).getByRole('button', { name: 'Tomorrow 09:00' }))
    await user.click(within(picker).getByRole('button', { name: 'Schedule' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    const body = api.find('POST', '/v1/posts')[0].body as { status: string; publish_at: string; title: string }
    const at = new Date(body.publish_at)

    expect(body.title).toBe('Launch day')
    expect(body.status).toBe('scheduled')
    expect([at.getHours(), at.getMinutes()]).toEqual([9, 0])
  })

  it('a picked time shows as a chip next to Publish now and the calendar; the chip clears it', async () => {
    mockApi(routes)
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    // No channel and no text yet: Schedule only sets the time.
    await user.click(await screen.findByRole('button', { name: 'Schedule for later' }))
    const picker = await screen.findByRole('dialog', { name: 'Pick a date and time' })

    await user.click(within(picker).getByRole('button', { name: 'In 1 hour' }))
    await user.click(within(picker).getByRole('button', { name: 'Schedule' }))
    expect(await screen.findByTestId('publish-at-chip')).toBeInTheDocument()
    expect(screen.getByTestId('publish-now')).toBeInTheDocument()
    expect(screen.getByTestId('date-time-picker')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Clear schedule' }))
    expect(screen.queryByTestId('publish-at-chip')).not.toBeInTheDocument()
    expect(screen.getByTestId('publish-now')).toBeInTheDocument()
  })

  it('counts characters per picked channel and publishes now with the picked channels', async () => {
    const api = mockApi({
      ...routes,
      'POST /v1/posts': post({ id: 'pst_9', status: 'draft' }),
      'POST /v1/posts/pst_9/schedule': post({ id: 'pst_9', status: 'scheduled' }),
      'POST /v1/posts/pst_9/cancel': post({ id: 'pst_9', status: 'draft' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    await user.type(screen.getByTestId('post-content'), 'Hello world')
    // The counter shows the tightest limit; its popover lists every channel against its platform's limit.
    expect(screen.getByRole('button', { name: /characters per channel$/ })).toHaveTextContent('11/300')
    await user.click(screen.getByRole('button', { name: /characters per channel$/ }))
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
    // Publish now waits 7 s with an Undo: it schedules the post for that moment instead of publishing at once.
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_9/schedule')).toHaveLength(1))
    const at = new Date(
      (api.find('POST', '/v1/posts/pst_9/schedule')[0].body as { publish_at: string }).publish_at,
    ).getTime()

    expect(at - Date.now()).toBeGreaterThan(5000)
    expect(at - Date.now()).toBeLessThanOrEqual(7000)
    expect(api.find('POST', '/v1/posts/pst_9/publish')).toHaveLength(0)
    expect(await screen.findByText(/Publishing in \d s/)).toBeInTheDocument()
    // The toast sits outside the open sheet (which blocks pointer events in the test DOM): click it directly.
    fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_9/cancel')).toHaveLength(1))
    expect(await screen.findByText('Publishing canceled')).toBeInTheDocument()
  })

  it('a channel that removes links counts the text without them and says so, in red', async () => {
    mockApi({
      ...routes,
      'GET /v1/channels': {
        data: [channel(), channel({ id: 'ch_x', name: 'Brand on X', provider: 'x', links: 'remove' })],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_x')
    await user.type(screen.getByTestId('post-content'), 'New post: https://example.com/a')
    expect(screen.getByTestId('links-notices')).toHaveTextContent(
      'Links will be removed from: Brand on X (https://example.com/a)',
    )
    expect(screen.getByTestId('links-notices')).not.toHaveTextContent('Brand on Mastodon')
    await user.click(screen.getByRole('button', { name: /characters per channel$/ }))
    // Mastodon keeps the link (31 characters), X receives "New post:" (9).
    expect(await screen.findByTestId('counters')).toHaveTextContent('31/500')
    expect(screen.getByTestId('counters')).toHaveTextContent('9')
    expect(screen.getByRole('button', { name: /characters per channel$/ })).toHaveClass('border-destructive')
  })

  it('a past time says so next to its chip and holds Publish now back until it is cleared', async () => {
    const draft = post({
      id: 'pst_9',
      status: 'draft',
      content: 'Late',
      publish_at: '2020-01-05T09:00:00Z',
      targets: [target()],
    })
    const api = mockApi({
      ...routes,
      'PATCH /v1/posts/pst_9': { ...draft, publish_at: null },
      'POST /v1/posts/pst_9/schedule': { ...draft, status: 'scheduled' },
      'POST /v1/posts/pst_9/cancel': draft,
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} post={draft} />, { mode: 'local' })
    // The notice sits with the time, the chip points at it.
    const notice = await screen.findByTestId('publish-at-error')
    const chip = screen.getByTestId('publish-at-chip')

    expect(notice).toHaveAttribute('role', 'alert')
    expect(notice).toHaveTextContent('This time is in the past. Pick a future time or clear it.')
    expect(chip).toHaveClass('border-destructive')
    expect(chip).toHaveAttribute('aria-describedby', notice.id)
    // Publish now stays focusable, says why on focus, and does nothing.
    const now = screen.getByTestId('publish-now')

    expect(now).toHaveAttribute('aria-disabled', 'true')
    expect(now).toHaveAttribute('aria-describedby', notice.id)
    await user.click(now)
    expect(api.find('PATCH', '/v1/posts/pst_9')).toHaveLength(0)
    expect(api.find('POST', '/v1/posts/pst_9/schedule')).toHaveLength(0)
    now.blur()
    now.focus()
    expect(await screen.findByRole('tooltip')).toHaveTextContent(
      'The date is in the past: pick a future time or clear the date to publish now',
    )
    // Cleared, it publishes now with the usual Undo.
    await user.click(screen.getByRole('button', { name: 'Clear schedule' }))
    expect(screen.queryByTestId('publish-at-error')).not.toBeInTheDocument()
    expect(screen.getByTestId('publish-now')).not.toHaveAttribute('aria-disabled')
    await user.click(screen.getByTestId('publish-now'))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_9/schedule')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/posts/pst_9')[0].body).toMatchObject({ publish_at: null })
    expect(await screen.findByText(/Publishing in \d s/)).toBeInTheDocument()
  })

  it('the picked channel stands out in the heading over its text, and its tab ring circles the avatar', async () => {
    mockApi(routes)
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    await user.click(screen.getByTestId('tab-channel-ch_2'))
    const heading = screen.getByRole('heading', { name: 'Text for Brand on Bluesky' })
    const name = within(heading).getByTestId('text-for-channel')

    expect(name).toHaveAttribute('title', 'Brand on Bluesky')
    expect(name).toHaveClass('bg-muted')
    expect(
      screen.getByText('Brand on Bluesky', { selector: '.font-medium.text-foreground' }).parentElement,
    ).toHaveTextContent('Brand on Bluesky uses the text for all channels. Change it here to give it its own.')
    // The selection ring is its own circle around the avatar, not the button around avatar and badge.
    const tab = screen.getByTestId('tab-channel-ch_2')

    expect(tab).toHaveAttribute('aria-selected', 'true')
    expect(within(tab).getByTestId('tab-channel-ring')).toHaveClass('ring-brand', 'rounded-full')
    expect(within(screen.getByTestId('tab-channel-ch_1')).getByTestId('tab-channel-ring')).toHaveClass(
      'ring-transparent',
    )
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

  it("the Settings tab holds each picked channel's settings and counts the channels with changes", async () => {
    const api = mockApi({
      ...routes,
      'GET /v1/channels': {
        data: [
          channel({ id: 'ch_x', name: 'Main X', provider: 'x' }),
          channel({ id: 'ch_x2', name: 'Second X', provider: 'x' }),
        ],
      },
      'GET /v1/providers': {
        data: [
          provider({
            id: 'x',
            name: 'X',
            settings_fields: [
              { key: 'made_with_ai', label: 'Made with AI', kind: 'boolean', required: false, default: 'false' },
              { key: 'reposters', label: 'Repost from', kind: 'channels', required: false, max_items: 10 },
            ],
          }),
        ],
      },
      'POST /v1/posts': post({ id: 'pst_9' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_x')
    await user.type(screen.getByTestId('post-content'), 'Launch day')
    await user.click(await screen.findByRole('tab', { name: /Settings/ }))
    const card = await screen.findByTestId('settings-ch_x')

    await user.click(within(card).getByRole('switch', { name: 'Made with AI' }))
    await user.click(within(card).getByRole('button', { name: /^Repost from/ }))
    await user.click(await screen.findByRole('option', { name: /Second X/ }))
    await user.keyboard('{Escape}')
    expect(screen.getByRole('tab', { name: /Settings/ })).toHaveTextContent('1')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts')[0].body).toMatchObject({
      targets: [{ channel_id: 'ch_x', settings: { made_with_ai: 'true', reposters: 'ch_x2' } }],
    })
  })

  it('adds tags, existing or new, and sends them with the post', async () => {
    const api = mockApi({
      ...routes,
      'GET /v1/tags': { data: [{ name: 'launch', count: 3 }] },
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
    // Tab adds what was typed too, and clears the box for the next one.
    const box = screen.getByPlaceholderText('Search or add...')

    await user.type(box, 'promo')
    await user.keyboard('{Tab}')
    expect(box).toHaveValue('')
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts')[0].body).toMatchObject({ tags: ['launch', 'spring-sale', 'promo'] })
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
    // Delays: a quick pick, and any number of minutes in the custom field.
    await user.click(screen.getByRole('button', { name: 'When comment 1 goes out' }))
    await user.click(await screen.findByRole('button', { name: '5 min later' }))
    expect(screen.getByRole('button', { name: 'When comment 1 goes out' })).toHaveTextContent('5 min later')
    await user.click(screen.getByRole('button', { name: 'When comment 2 goes out' }))
    await user.type(await screen.findByRole('spinbutton', { name: 'Custom delay in minutes' }), '45')
    await user.click(screen.getByRole('button', { name: 'Set' }))
    expect(screen.getByRole('button', { name: 'When comment 2 goes out' })).toHaveTextContent('45 min later')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts')[0].body).toMatchObject({
      thread: [
        { content: 'Ask us anything', media: [], delay_seconds: 300 },
        { content: 'Details in the docs', media: [], delay_seconds: 2700 },
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

  it("opens the channel whose text or media was refused, so the error isn't hidden under All channels", async () => {
    mockApi({
      ...routes,
      'POST /v1/posts': {
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'validation failed',
            fields: { 'targets.ch_2.content': 'the picture is larger than Bluesky takes' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    await user.type(screen.getByTestId('post-content'), 'x')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    expect(await screen.findByText('The picture is larger than Bluesky takes')).toBeVisible()
  })

  it('a comment keeps every file uploaded at once, and text typed while they upload', async () => {
    let n = 0

    mockApi({
      ...routes,
      'POST /v1/media': () => {
        n += 1

        return { id: `med_${n}`, mime_type: 'video/mp4', original_name: `clip-${n}.mp4`, size_bytes: 10 }
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await user.click(await screen.findByTestId('add-comment'))
    const part = screen.getByTestId('thread-part-0')
    const input = part.querySelector<HTMLInputElement>('input[type="file"]')!

    await user.type(within(part).getByTestId('thread-content-0'), 'Doors at 9')
    await user.upload(input, [
      new File(['a'], 'a.mp4', { type: 'video/mp4' }),
      new File(['b'], 'b.mp4', { type: 'video/mp4' }),
    ])
    await waitFor(() => expect(within(part).getAllByRole('img', { name: /^clip-\d\.mp4$/ })).toHaveLength(2))
    expect(within(part).getByTestId('thread-content-0')).toHaveValue('Doors at 9')
  })
})
