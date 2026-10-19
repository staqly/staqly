import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ComposerForm } from '@/components/posts/composer'
import { PostsWorkspace } from '@/components/posts/posts-workspace'
import { Sheet, SheetContent } from '@/components/ui/sheet'
import { removePostFromLists, syncPostInCache } from '@/lib/queries'

import { channel, collection, post, provider, target } from './fixtures'
import { nav } from './navigation'
import { renders } from './render-count'
import { mockApi, renderWithProviders } from './utils'

// ProseMirror does not type in jsdom: the editor is a textarea here, like in composer.test.tsx.
vi.mock('@/components/posts/post-editor', () => ({
  PostEditor: (p: {
    id: string
    value: string
    onChange: (t: string) => void
    testId?: string
    below?: React.ReactNode
    start?: React.ReactNode
    end?: React.ReactNode
  }) => (
    <>
      <textarea id={p.id} value={p.value} onChange={(e) => p.onChange(e.target.value)} data-testid={p.testId} />
      {p.below}
      {p.start}
      {p.end}
    </>
  ),
}))

const CHANNELS = [
  channel(),
  channel({ id: 'ch_2', name: 'Brand on Bluesky', provider: 'bluesky' }),
  channel({ id: 'ch_3', name: 'Brand on X', provider: 'x' }),
]

const POSTS = Array.from({ length: 50 }, (_, i) =>
  post({
    id: `pst_${i}`,
    content: `Post number ${i}\nwith an excerpt`,
    targets: CHANNELS.map((c) => target({ channel_id: c.id, channel_name: c.name, provider: c.provider })),
  }),
)

function setupList() {
  nav.set('/posts', 'collection=col_1')

  return mockApi({
    'GET /v1/collections': { data: [collection()] },
    'GET /v1/collections/col_1': collection(),
    'GET /v1/channels': { data: CHANNELS },
    'GET /v1/tags': { data: [] },
    'GET /v1/events': { data: [] },
    'GET /v1/posts': { data: POSTS, next_cursor: null },
  })
}

async function renderList() {
  setupList()
  const out = renderWithProviders(<PostsWorkspace />, { mode: 'local' })

  await waitFor(() => expect(screen.getAllByTestId('post-row')).toHaveLength(50))
  // Let the queries settle (events, tags) before counting.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 50))
  })

  return out
}

const SAVED = post({
  id: 'pst_c',
  content: 'Hello',
  targets: CHANNELS.map((c) => target({ channel_id: c.id, channel_name: c.name, provider: c.provider })),
})

function setupComposer() {
  mockApi({
    'GET /v1/collections': { data: [collection()] },
    'GET /v1/channels': { data: CHANNELS },
    'GET /v1/channel-groups': { data: [] },
    'GET /v1/tags': { data: [] },
    'GET /v1/providers': {
      data: [provider(), provider({ id: 'bluesky', name: 'Bluesky' }), provider({ id: 'x', name: 'X' })],
    },
  })
}

async function renderComposer() {
  setupComposer()
  const out = renderWithProviders(
    <Sheet open>
      <SheetContent>
        <ComposerForm post={SAVED} onDone={() => {}} />
      </SheetContent>
    </Sheet>,
    { mode: 'local' },
  )

  await screen.findByTestId('tab-channel-ch_3')
  await act(async () => {
    await new Promise((r) => setTimeout(r, 50))
  })

  return out
}

beforeEach(() => nav.reset())

afterEach(() => vi.useRealTimers())

// Budgets for the posts table and the composer (apps/platform/AGENTS.md, Rendering performance). A failure means a
// change made components render that shouldn't: a fresh array or closure in a memo row's props, a clock in a form.
describe('render budgets: the posts list (50 posts)', () => {
  it('the first selection renders at most two rows, a second one row', async () => {
    await renderList()
    const boxes = screen.getAllByTestId('post-select')

    renders.reset()
    fireEvent.click(boxes[0]!)
    expect(renders.of('PostRow')).toBeLessThanOrEqual(2)

    renders.reset()
    fireEvent.click(boxes[1]!)
    expect(renders.of('PostRow')).toBe(1)
  })

  it('syncPostInCache renders the changed row only; removePostFromLists renders no row', async () => {
    const { client } = await renderList()

    renders.reset()
    act(() => syncPostInCache(client, { ...POSTS[5]!, content: 'Changed\nexcerpt' }))
    await screen.findByText('Changed')
    expect(renders.of('PostRow')).toBe(1)

    renders.reset()
    act(() => removePostFromLists(client, 'pst_7'))
    await waitFor(() => expect(screen.getAllByTestId('post-row')).toHaveLength(49))
    expect(renders.of('PostRow')).toBe(0)
  })
})

describe('render budgets: the composer (3 channels)', () => {
  it('a keystroke in the shared text stays within budget', async () => {
    await renderComposer()

    renders.reset()
    fireEvent.change(screen.getByTestId('post-content'), { target: { value: 'Hello!' } })
    const total = Object.values(renders.all()).reduce((a, b) => a + b, 0)

    // 33 when measured (202 before the tab editors were memo components): the form, the shared editor, its
    // counter and the links notices. The hidden channel editors don't render.
    expect(total).toBeLessThanOrEqual(36)
    expect(renders.of('ChannelEditor')).toBe(1)
    expect(renders.of('SettingsCard')).toBe(0)
    expect(renders.of('ChannelTab')).toBe(0)
  })

  it('a second passing with the composer open renders nothing', async () => {
    // Before rendering, so the composer's timers are fake ones; they still advance with real time for findBy.
    vi.useFakeTimers({ shouldAdvanceTime: true })
    await renderComposer()

    renders.reset()
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    // The footer's "picked time is past" check is one timer to that time, not a clock in the form.
    expect(renders.of('ComposerForm')).toBe(0)
    expect(Object.keys(renders.all())).toHaveLength(0)
  })
})
