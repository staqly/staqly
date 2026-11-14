import { screen, waitFor, within, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { Composer } from '@/components/posts/composer'

import { channel, collection, post, provider, target } from './fixtures'
import { mockApi, renderWithProviders, type Call } from './utils'

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
    data: [
      channel({ comments: true }),
      channel({ id: 'ch_2', name: 'Brand on Bluesky', provider: 'bluesky', comments: false }),
    ],
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
          comments_text_only: false,
          needs_media: false,
          links: 'keep',
          link_modes: ['keep', 'remove', 'defang'],
        },
        settings_fields: [],
      }),
    ],
  },
}

const xPosts = (left: number) => ({
  period: '2026-11',
  period_end: '2026-12-01T00:00:00Z',
  unlimited: false,
  included: 100,
  used: 100 - left,
  included_left: left,
  credits_left: 0,
  credits_expire_at: null,
  left,
  bundles: [],
  currency: 'eur',
  link_post_weight: 14,
})

/** An AI operation the server offers. */
const aiOp = (id: string, kind: string) => ({
  id,
  kind,
  prompt: 'required',
  image: false,
  mask: false,
  provider: 'fal',
  model: 'm',
  fallback: '',
})

/** Opens the Channels multi-select, picks these channels and closes it. */
async function pickChannels(user: ReturnType<typeof userEvent.setup>, ...ids: string[]) {
  await user.click(await screen.findByRole('button', { name: 'Channels' }))

  for (const id of ids) {
    await user.click(await screen.findByTestId(`pick-channel-${id}`))
  }

  await user.keyboard('{Escape}')
}

describe('composer', () => {
  afterEach(() => toast.dismiss())

  it('writes the post with a video made by AI and adds the video to the post', async () => {
    const job = {
      id: 'aij_1',
      operation: 'text_to_video',
      kind: 'video',
      status: 'succeeded',
      result: { content_type: 'video/mp4' },
      created_at: new Date().toISOString(),
    }
    const clip = {
      id: 'med_9',
      original_name: 'clip.mp4',
      mime_type: 'video/mp4',
      size: 10,
      created_at: new Date().toISOString(),
    }
    const api = mockApi({
      ...routes,
      'GET /v1/ai': {
        configured: true,
        providers: ['fal'],
        operations: [aiOp('assistant', 'text'), aiOp('generate', 'image'), aiOp('text_to_video', 'video')],
        usage: { period: '2026-10', used_usd: 0, limit_usd: 10 },
      },
      'POST /v1/ai/post': { title: 'Launch', content: 'It is out.', comments: [], media_prompt: 'A rocket at dawn' },
      'POST /v1/ai/jobs': { ...job, status: 'queued' },
      'GET /v1/ai/jobs/aij_1': job,
      'POST /v1/ai/jobs/aij_1/accept': clip,
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await user.click(await screen.findByTestId('ai-post-open'))
    const drawer = await screen.findByTestId('ai-post-dialog')

    await user.click(within(drawer).getByRole('button', { name: /Announce a feature/ }))
    expect(within(drawer).getByTestId('ai-post-brief')).toHaveValue('Announce our newest feature: ')
    await user.type(within(drawer).getByTestId('ai-post-brief'), 'v2')
    await user.click(within(drawer).getByRole('radio', { name: /Video/ }))
    await user.click(within(drawer).getByRole('radio', { name: 'Portrait' }))
    await user.click(within(drawer).getByRole('radio', { name: '10 seconds' }))
    await user.click(within(drawer).getByTestId('ai-post-write'))
    expect(await within(drawer).findByText('It is out.')).toBeInTheDocument()
    expect(api.find('POST', '/v1/ai/post')[0]?.body).toMatchObject({ media: 'video' })
    await waitFor(() => expect(within(drawer).getByTestId('ai-post-use')).toBeEnabled())
    const sent = api.find('POST', '/v1/ai/jobs')[0]?.body as FormData

    expect(sent.get('operation')).toBe('text_to_video')
    expect(sent.get('prompt')).toBe('A rocket at dawn')
    expect(sent.get('aspect_ratio')).toBe('9:16')
    expect(sent.get('duration')).toBe('10')
    await user.click(within(drawer).getByTestId('ai-post-use'))
    await waitFor(() => expect(screen.queryByTestId('ai-post-dialog')).not.toBeInTheDocument())
    expect(api.find('POST', '/v1/ai/jobs/aij_1/accept')).toHaveLength(1)
    expect(screen.getByTestId('post-title')).toHaveValue('Launch')
    expect(screen.getByTestId('post-content')).toHaveValue('It is out.')
  })

  it("writes the post with AI and puts the draft into the form, within the picked channels' limit", async () => {
    const assistant = {
      id: 'assistant',
      kind: 'text',
      prompt: 'required',
      image: false,
      mask: false,
      provider: 'openai_compatible',
      model: 'm',
      fallback: '',
    }
    const api = mockApi({
      ...routes,
      'GET /v1/ai': {
        configured: true,
        providers: ['fal'],
        operations: [assistant],
        usage: { period: '2026-10', used_usd: 0, limit_usd: 10 },
      },
      'POST /v1/ai/post': {
        title: 'Spring sale',
        content: 'Our spring sale starts today.',
        comments: ['20% off everything.'],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_2')
    await user.click(await screen.findByTestId('ai-post-open'))
    const dialog = await screen.findByTestId('ai-post-dialog')

    await user.click(within(dialog).getByTestId('ai-post-write'))
    expect(within(dialog).getByText('Say what the post is about.')).toBeInTheDocument()
    expect(api.find('POST', '/v1/ai/post')).toHaveLength(0)
    await user.type(within(dialog).getByTestId('ai-post-brief'), 'Spring sale, 20% off')
    await user.click(within(dialog).getByRole('radio', { name: /Thread/ }))
    await user.click(within(dialog).getByRole('radio', { name: 'Brand ("we")' }))
    await user.click(within(dialog).getByTestId('ai-post-write'))
    expect(await within(dialog).findByText('Our spring sale starts today.')).toBeInTheDocument()
    expect(api.find('POST', '/v1/ai/post')[0]?.body).toMatchObject({
      brief: 'Spring sale, 20% off',
      format: 'thread',
      voice: 'brand',
      max_chars: 300,
    })

    await user.click(within(dialog).getByTestId('ai-post-use'))
    await waitFor(() => expect(screen.queryByTestId('ai-post-dialog')).not.toBeInTheDocument())
    expect(screen.getByTestId('post-title')).toHaveValue('Spring sale')
    expect(screen.getByTestId('post-content')).toHaveValue('Our spring sale starts today.')
    expect(screen.getByDisplayValue('20% off everything.')).toBeInTheDocument()
  })

  it.each(['new', 'existing'] as const)(
    'retries a %s post after scheduling fails without duplicating it or reusing a stale version',
    async (kind) => {
      const draft = post({ id: 'pst_9', status: 'draft', version: 4, content: 'Ready', targets: [target()] })
      let version = draft.version
      let attempts = 0
      const onSaved = vi.fn<() => void>()
      const api = mockApi({
        ...routes,
        'POST /v1/posts': () => ({ ...draft, version: ++version }),
        'PATCH /v1/posts/pst_9': ({ body }: Call) => {
          if ((body as { expected_version: number }).expected_version !== version) {
            return { status: 409, body: { error: { code: 'conflict', message: 'Stale version' } } }
          }

          return { ...draft, content: (body as { content: string }).content, version: ++version }
        },
        'POST /v1/posts/pst_9/schedule': () => {
          attempts++

          return attempts === 1
            ? { status: 503, body: { error: { code: 'unavailable', message: 'Scheduling unavailable' } } }
            : { ...draft, status: 'scheduled', version: ++version }
        },
        'GET /v1/posts': () => ({ data: [{ ...draft, version }] }),
      })
      const user = userEvent.setup()

      renderWithProviders(
        <Composer
          open
          onOpenChange={() => {}}
          onSaved={onSaved}
          collectionId="col_1"
          post={kind === 'existing' ? draft : undefined}
        />,
        { mode: 'local' },
      )

      if (kind === 'new') {
        await pickChannels(user, 'ch_1')
        await user.type(screen.getByTestId('post-content'), 'Ready')
      }

      await user.click(await screen.findByTestId('publish-now'))
      await screen.findAllByText('Scheduling unavailable')
      expect(onSaved).not.toHaveBeenCalled()
      await user.type(screen.getByTestId('post-content'), ' again')
      await user.click(screen.getByTestId('publish-now'))
      await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1))
      expect(api.find('POST', '/v1/posts')).toHaveLength(kind === 'new' ? 1 : 0)
      expect(api.find('PATCH', '/v1/posts/pst_9').at(-1)?.body).toMatchObject({ content: 'Ready again' })
      expect(attempts).toBe(2)
    },
  )

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

  it('Schedule offers the channels that say when a stream starts: picking one schedules the post for it', async () => {
    const live = {
      'GET /v1/channels': {
        data: [...routes['GET /v1/channels'].data, channel({ id: 'ch_k', name: 'Jane on Kick', provider: 'kick' })],
      },
      'GET /v1/providers': {
        data: [
          ...routes['GET /v1/providers'].data,
          provider({ id: 'kick', name: 'Kick', live_trigger: true, settings_fields: [] }),
        ],
      },
    }
    const api = mockApi({ ...routes, ...live, 'POST /v1/posts': post({ id: 'pst_9', status: 'scheduled' }) })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-title'), 'Live now')
    await user.type(screen.getByTestId('post-content'), 'Come hang out')
    await user.click(screen.getByRole('button', { name: 'Schedule for later' }))
    const picker = await screen.findByRole('dialog', { name: 'Pick a date and time' })
    const streams = within(picker).getByTestId('schedule-live')

    // Only Kick says when a stream starts.
    expect(within(streams).getAllByRole('button')).toHaveLength(1)
    await user.click(within(streams).getByRole('button', { name: 'When Jane on Kick goes live' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
    const body = api.find('POST', '/v1/posts')[0].body as {
      status: string
      publish_at: string | null
      publish_on: unknown
    }

    expect(body.status).toBe('scheduled')
    expect(body.publish_at).toBeNull()
    expect(body.publish_on).toEqual({ event: 'live', channel_id: 'ch_k' })
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

  it("says in the schedule picker that a picked channel's sign-in runs out before the picked time", async () => {
    // Schedule saves, schedules and closes in one step: the warning in the form alone was never seen.
    mockApi({
      ...routes,
      'GET /v1/channels': {
        data: [
          channel({ renew_by: new Date(Date.now() + 30 * 60_000).toISOString() }),
          channel({ id: 'ch_2', name: 'Brand on Bluesky', provider: 'bluesky' }),
        ],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    await user.click(await screen.findByRole('button', { name: 'Schedule for later' }))
    const picker = await screen.findByRole('dialog', { name: 'Pick a date and time' })

    await user.click(within(picker).getByRole('button', { name: 'In 1 hour' }))
    const warning = await within(picker).findByTestId('channel-renew-first')

    expect(warning).toHaveTextContent(/^The sign-in of .+ runs out .+, before this post goes out\./)
    expect(within(picker).getAllByTestId('channel-renew-first')).toHaveLength(1)
    // It doesn't stop the scheduling.
    expect(within(picker).getByRole('button', { name: 'Schedule' })).toBeEnabled()
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
    screen.getByRole('button', { name: 'Clear schedule' }).focus() // moved there from another control
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

  it('with one channel, its refusal shows under the text (it has no tab of its own)', async () => {
    mockApi({
      ...routes,
      'POST /v1/posts': {
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'validation failed',
            fields: { 'targets.ch_1.content': 'Pinterest pins take one picture' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'x')
    await user.click(screen.getByTestId('publish-now'))
    expect(await screen.findByText('Pinterest pins take one picture')).toBeVisible()
  })

  it('before publishing, lists what each channel leaves out, and publishes on Go on', async () => {
    const api = mockApi({
      ...routes,
      'POST /v1/posts': post({ id: 'pst_9' }),
      'POST /v1/posts/pst_9/schedule': post({ id: 'pst_9', status: 'scheduled' }),
      'POST /v1/media': () => ({ id: 'med_v', mime_type: 'video/mp4', original_name: 'clip.mp4', size_bytes: 10 }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    await user.type(screen.getByTestId('post-content'), 'Launch day')
    const input = document.querySelector<HTMLInputElement>('input[type="file"]')!

    await user.upload(input, new File(['v'], 'clip.mp4', { type: 'video/mp4' }))
    await screen.findByRole('img', { name: 'clip.mp4' })
    await user.click(screen.getByTestId('publish-now'))
    const review = await screen.findByTestId('publish-review')

    expect(review).toHaveTextContent('Brand on Bluesky')
    expect(review).toHaveTextContent('Takes pictures only, 4 files at most')
    expect(review).toHaveTextContent('leaves out clip.mp4')
    expect(review).not.toHaveTextContent('Brand on Mastodon')
    expect(api.find('POST', '/v1/posts')).toHaveLength(0)
    await user.click(screen.getByTestId('publish-review-confirm'))
    await waitFor(() => expect(api.find('POST', '/v1/posts')).toHaveLength(1))
  })

  it('a platform that takes one file: after the first, Upload, the library and Design are off and say why', async () => {
    const limits = { ...provider().limits, max_media: 1, media_kinds: ['image' as const] }

    mockApi({
      ...routes,
      'GET /v1/channels': { data: [channel({ id: 'ch_pin', name: 'Brand on Pinterest', provider: 'pinterest' })] },
      'GET /v1/providers': { data: [provider({ id: 'pinterest', name: 'Pinterest', limits, settings_fields: [] })] },
      'POST /v1/media': { id: 'med_1', mime_type: 'image/png', original_name: 'a.png', size_bytes: 10 },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_pin')

    const uploadButton = screen.getByRole('button', { name: 'Upload' })

    expect(uploadButton).toBeEnabled()
    expect(uploadButton).not.toHaveAttribute('title')
    await user.upload(
      document.querySelector<HTMLInputElement>('input[type="file"]')!,
      new File(['a'], 'a.png', { type: 'image/png' }),
    )
    expect(await screen.findByRole('img', { name: 'a.png' })).toBeInTheDocument()

    await waitFor(() => expect(screen.getByRole('button', { name: 'Upload' })).toBeDisabled())
    expect(screen.getByRole('button', { name: 'Upload' })).toHaveAttribute(
      'title',
      'Takes pictures only, 1 file at most.',
    )
    expect(screen.getAllByTestId('design-menu')[0]).toBeDisabled()
  })

  it('a new post that saved but could not go out says why, and trying again sends it, never a second copy', async () => {
    let refuse = true
    const api = mockApi({
      ...routes,
      'POST /v1/posts': post({ id: 'pst_9', status: 'draft', version: 1 }),
      'PATCH /v1/posts/pst_9': post({ id: 'pst_9', status: 'draft', version: 2 }),
      'POST /v1/posts/pst_9/schedule': () =>
        refuse
          ? {
              status: 402,
              body: { error: { code: 'x_posts_exhausted', message: 'the X posts of this period are used up' } },
            }
          : post({ id: 'pst_9', status: 'scheduled', version: 3 }),
    })
    const user = userEvent.setup()
    const onOpenChange = vi.fn<(o: boolean) => void>()

    renderWithProviders(<Composer open onOpenChange={onOpenChange} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'Hello world')
    await user.click(screen.getByTestId('publish-now'))

    expect(await screen.findByText('Plan limit reached')).toBeInTheDocument()
    expect(screen.getByText('The X posts of this period are used up')).toBeInTheDocument()
    // Nothing went out, so the drawer stays open with the text in it.
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    expect(screen.getByTestId('post-content')).toHaveValue('Hello world')
    expect(api.find('POST', '/v1/posts')).toHaveLength(1)

    refuse = false
    await user.click(await screen.findByTestId('publish-now'))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_9/schedule')).toHaveLength(2))
    // The post made by the first try is the one saved and sent: saved over the version that try wrote.
    expect(api.find('POST', '/v1/posts')).toHaveLength(1)
    expect(api.find('PATCH', '/v1/posts/pst_9')).toHaveLength(1)
    expect(api.find('PATCH', '/v1/posts/pst_9')[0]!.body).toMatchObject({ content: 'Hello world', expected_version: 1 })
    expect(await screen.findByText(/Publishing in \d s/)).toBeInTheDocument()
  })

  it('a save the server refuses for a reason of its own (a plan that ended) is said, and nothing is lost', async () => {
    const api = mockApi({
      ...routes,
      'POST /v1/posts': {
        status: 402,
        body: { error: { code: 'plan_expired', message: 'the plan of this workspace has ended' } },
      },
    })
    const user = userEvent.setup()
    const onOpenChange = vi.fn<(o: boolean) => void>()

    renderWithProviders(<Composer open onOpenChange={onOpenChange} collectionId="col_1" />, { mode: 'cloud' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'Hello world')
    await user.click(screen.getByRole('button', { name: /Save draft/i }))

    expect(await screen.findByText('This workspace is read-only')).toBeInTheDocument()
    expect(screen.getByText('The plan of this workspace has ended')).toBeInTheDocument()
    expect(api.find('POST', '/v1/posts')).toHaveLength(1)
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    expect(screen.getByTestId('post-content')).toHaveValue('Hello world')
  })

  it("says before publishing to X that this period's X posts are used up, and only when they are", async () => {
    let left = 0

    mockApi({
      ...routes,
      'GET /v1/channels': { data: [channel({ id: 'ch_x', name: 'Brand on X', provider: 'x' })] },
      'GET /v1/providers': { data: [provider({ id: 'x', name: 'X', settings_fields: [] })] },
      'GET /v1/billing/x-posts': () => xPosts(left),
    })
    const user = userEvent.setup()
    const { unmount } = renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, {
      mode: 'cloud',
    })

    await pickChannels(user, 'ch_x')
    expect(await screen.findByTestId('x-posts-exhausted')).toHaveTextContent(
      'Your X posts for this period are used up. Buy more or wait until Dec 1.',
    )
    unmount()

    left = 3
    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'cloud' })
    await pickChannels(user, 'ch_x')
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByTestId('x-posts-exhausted')).not.toBeInTheDocument()
  })

  it('with one channel, offers only what it takes: no comments where it posts none', async () => {
    mockApi(routes)
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_2')
    expect(screen.queryByTestId('add-comment')).not.toBeInTheDocument()
    expect(
      screen.getByText('Bluesky has no comments under a post, so this one is published alone.'),
    ).toBeInTheDocument()
    expect(document.querySelector('input[type="file"]')).toHaveAttribute('accept', 'image/*')
  })

  it('always has Post and Settings; Settings waits for a channel, and its card names what it needs', async () => {
    mockApi({
      ...routes,
      'GET /v1/providers': {
        data: [provider({ settings_fields: [{ key: 'board', label: 'Board', kind: 'text', required: true }] })],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    expect(await screen.findByTestId('tab-settings')).toBeDisabled()
    await pickChannels(user, 'ch_1')
    expect(screen.getByTestId('tab-settings')).toBeEnabled()
    await user.click(screen.getByTestId('tab-settings'))
    expect(within(screen.getByTestId('settings-ch_1')).getByText(/Needs Board/)).toBeInTheDocument()
  })

  it('opens one channel settings card at a time', async () => {
    const fields = [{ key: 'board', label: 'Board', kind: 'text' as const, required: false }]

    mockApi({
      ...routes,
      'GET /v1/providers': {
        data: [
          provider({ settings_fields: fields }),
          provider({ id: 'bluesky', name: 'Bluesky', settings_fields: fields }),
        ],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1', 'ch_2')
    await user.click(screen.getByTestId('tab-settings'))
    const first = screen.getByTestId('settings-ch_1')
    const second = screen.getByTestId('settings-ch_2')

    await user.click(within(first).getByRole('button', { expanded: false }))
    expect(within(first).getByRole('button', { expanded: true })).toBeInTheDocument()
    await user.click(within(second).getByRole('button', { expanded: false }))
    expect(within(second).getByRole('button', { expanded: true })).toBeInTheDocument()
    expect(within(first).getByRole('button', { expanded: false })).toBeInTheDocument()
  })

  it('a post that went out has no Settings tab', async () => {
    mockApi({
      ...routes,
      'GET /v1/providers': {
        data: [provider({ settings_fields: [{ key: 'board', label: 'Board', kind: 'text', required: false }] })],
      },
    })
    const sent = post({ id: 'pst_8', status: 'published', content: 'Out', targets: [target({ status: 'published' })] })

    renderWithProviders(<Composer open onOpenChange={() => {}} post={sent} />, { mode: 'local' })
    expect(await screen.findByRole('tab', { name: 'Post' })).toBeInTheDocument()
    expect(screen.queryByTestId('tab-settings')).not.toBeInTheDocument()
  })

  it('marks every required setting left empty at once, in Settings, without sending', async () => {
    const api = mockApi({
      ...routes,
      'GET /v1/providers': {
        data: [
          provider({
            settings_fields: [
              { key: 'board', label: 'Board', kind: 'text', required: true },
              { key: 'link', label: 'Link', kind: 'text', required: true },
            ],
          }),
        ],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'x')
    await user.click(screen.getByTestId('publish-now'))
    await waitFor(() => expect(screen.getByTestId('tab-settings')).toHaveAttribute('data-state', 'active'))
    expect(screen.getByText('Board is required')).toBeVisible()
    expect(screen.getByText('Link is required')).toBeVisible()
    expect(api.find('POST', '/v1/posts')).toHaveLength(0)
    // Filling a field in takes its error away; the other stays.
    await user.type(screen.getByLabelText(/^Link/), 'https://example.com')
    expect(screen.queryByText('Link is required')).not.toBeInTheDocument()
    expect(screen.getByText('Board is required')).toBeVisible()
  })

  it('opens Settings when a setting is refused, even with the text refused too', async () => {
    mockApi({
      ...routes,
      'POST /v1/posts': {
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'validation failed',
            fields: { 'targets.ch_1.content': 'too many files', 'targets.ch_1.settings.visibility': 'pick one' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await pickChannels(user, 'ch_1')
    await user.type(screen.getByTestId('post-content'), 'x')
    await user.click(screen.getByTestId('publish-now'))
    await waitFor(() => expect(screen.getByTestId('tab-settings')).toHaveAttribute('data-state', 'active'))
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
