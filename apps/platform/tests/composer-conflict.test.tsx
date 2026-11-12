import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { PostSheet } from '@/components/posts/post-sheet'
import { qk } from '@/lib/queries'

import { channel, post, provider, target } from './fixtures'
import { type Call, mockApi, renderWithProviders } from './utils'

vi.mock('@/components/posts/post-editor', () => ({
  PostEditor: (p: { id: string; value: string; onChange: (t: string) => void; testId?: string }) => (
    <textarea id={p.id} value={p.value} onChange={(e) => p.onChange(e.target.value)} data-testid={p.testId} />
  ),
}))

const mine = post({ id: 'pst_1', status: 'draft', version: 4, title: 'Mine', content: 'Mine', targets: [target()] })
const theirs = { ...mine, version: 5, content: 'Rewritten by an agent', title: 'Agent title' }

type Patch = { content: string; expected_version: number }

/** The post as the server holds it: a save based on another version answers 409, like the API. */
function open(server = { ...mine }) {
  const api = mockApi({
    'GET /v1/posts/pst_1': () => server,
    'GET /v1/channels': { data: [channel()] },
    'GET /v1/providers': { data: [provider()] },
    'PATCH /v1/posts/pst_1': (call: Call) => {
      const body = call.body as Patch

      if (body.expected_version !== server.version) {
        return { status: 409, body: { error: { code: 'post_changed', message: 'post was modified' } } }
      }

      Object.assign(server, { content: body.content, version: server.version + 1 })

      return server
    },
    'GET /v1/collections': { data: [] },
    'GET /v1/tags': { data: [] },
    'GET /v1/channel-groups': { data: [] },
    'GET /v1/posts/pst_1/comments': { data: [] },
  })
  const out = renderWithProviders(<PostSheet id="pst_1" onClose={() => {}} />, { mode: 'local' })
  // What a refetch (live updates, window focus) does: the other edit lands in the query.
  const editElsewhere = () => {
    Object.assign(server, theirs)
    act(() => out.client.setQueryData(qk.post('pst_1'), { ...theirs }))
  }
  const patches = () => api.find('PATCH', '/v1/posts/pst_1').map((c) => c.body as Patch)

  return { server, client: out.client, editElsewhere, patches }
}

describe('a post edited elsewhere while its drawer is open', () => {
  it('untouched fields take the new version in', async () => {
    const { editElsewhere, patches } = open()
    const user = userEvent.setup()

    expect(await screen.findByTestId('post-content', {}, { timeout: 5000 })).toHaveValue('Mine')
    editElsewhere()
    await waitFor(() => expect(screen.getByTestId('post-content')).toHaveValue('Rewritten by an agent'))
    expect(screen.getByTestId('post-title')).toHaveValue('Agent title')

    await user.type(screen.getByTestId('post-content'), ' typo fix')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0]).toMatchObject({ content: 'Rewritten by an agent typo fix', expected_version: 5 })
  })

  it('typed fields stay on their version: the save is refused and the person chooses to save theirs over it', async () => {
    const { server, editElsewhere, patches } = open()
    const user = userEvent.setup()

    await user.type(await screen.findByTestId('post-content', {}, { timeout: 5000 }), ' typo fix')
    editElsewhere()
    expect(screen.getByTestId('post-content')).toHaveValue('Mine typo fix')
    await user.click(screen.getByRole('button', { name: 'Save draft' }))

    // Based on the version the fields were loaded from, never on the one that arrived under them.
    const dialog = await screen.findByRole('alertdialog', { name: 'This post was changed elsewhere' })

    expect(patches()).toHaveLength(1)
    expect(patches()[0]).toMatchObject({ content: 'Mine typo fix', expected_version: 4 })
    expect(server.content).toBe('Rewritten by an agent')
    expect(screen.getByTestId('post-content')).toHaveValue('Mine typo fix')

    await user.click(screen.getByRole('button', { name: 'Save my version' }))
    await waitFor(() => expect(patches()).toHaveLength(2))
    expect(patches()[1]).toMatchObject({ content: 'Mine typo fix', expected_version: 5 })
    expect(dialog).not.toBeInTheDocument()
  })

  it('or loads the other version and drops the typed text', async () => {
    const { editElsewhere, patches } = open()
    const user = userEvent.setup()

    await user.type(await screen.findByTestId('post-content', {}, { timeout: 5000 }), ' typo fix')
    editElsewhere()
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await user.click(await screen.findByRole('button', { name: 'Load their version' }))
    await waitFor(() => expect(screen.getByTestId('post-content')).toHaveValue('Rewritten by an agent'))
    expect(patches()).toHaveLength(1)
  })

  it('a change to what the form does not edit (a retry, an outcome) moves the version, not the text', async () => {
    const { server, client, patches } = open()
    const user = userEvent.setup()

    await user.type(await screen.findByTestId('post-content', {}, { timeout: 5000 }), ' typo fix')
    Object.assign(server, { version: 5, updated_at: '2026-02-01T00:00:00Z' })
    // The query tells its observers a tick later: wait for it, like the person's next click does.
    await act(async () => {
      client.setQueryData(qk.post('pst_1'), { ...server })
      await new Promise((r) => setTimeout(r, 20))
    })
    await user.click(screen.getByRole('button', { name: 'Save draft' }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0]).toMatchObject({ content: 'Mine typo fix', expected_version: 5 })
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
})
