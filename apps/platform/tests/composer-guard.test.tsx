import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Composer } from '@/components/posts/composer'

import { channel, collection, post, provider } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

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

async function openComposer() {
  mockApi({
    'GET /v1/collections': { data: [collection()] },
    'GET /v1/channels': { data: [channel()] },
    'GET /v1/providers': { data: [provider()] },
    'POST /v1/media': { id: 'med_1', mime_type: 'image/png', original_name: 'a.png', size_bytes: 10 },
  })
  const onOpenChange = vi.fn<(o: boolean) => void>()
  const user = userEvent.setup()

  renderWithProviders(<Composer open onOpenChange={onOpenChange} collectionId="col_1" />, { mode: 'local' })
  await user.click(await screen.findByRole('button', { name: 'Channels' }))
  await user.click(await screen.findByTestId('pick-channel-ch_1'))
  await user.keyboard('{Escape}')

  return { onOpenChange, user }
}

async function upload(user: ReturnType<typeof userEvent.setup>) {
  await user.upload(
    document.querySelector<HTMLInputElement>('input[type="file"]')!,
    new File(['a'], 'a.png', { type: 'image/png' }),
  )
  await waitFor(() => expect(screen.getByRole('img', { name: 'a.png' })).toBeInTheDocument())
  // The guard reads the fields a tick after a save.
  await new Promise((r) => setTimeout(r, 50))
}

describe('the composer keeps unsaved work', () => {
  it('typed text, then an uploaded attachment, then Esc: still asks before discarding', async () => {
    const { onOpenChange, user } = await openComposer()

    await user.type(await screen.findByTestId('post-content'), 'A long post nobody saved yet')
    await upload(user)
    await user.click(screen.getByTestId('post-content'))
    await user.keyboard('{Escape}')

    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    expect(await screen.findByRole('alertdialog', { name: 'Discard your changes?' })).toBeInTheDocument()
  })

  it('an attachment alone is a change too', async () => {
    const { onOpenChange, user } = await openComposer()

    await user.click(await screen.findByTestId('post-content'))
    await upload(user)
    await user.click(screen.getByTestId('post-content'))
    await user.keyboard('{Escape}')

    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    expect(await screen.findByRole('alertdialog', { name: 'Discard your changes?' })).toBeInTheDocument()
  })

  it('a submit the buttons would not allow (no channel picked) sends nothing', async () => {
    const api = mockApi({
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/channels': { data: [channel()] },
      'GET /v1/providers': { data: [provider()] },
      'POST /v1/posts': post({ id: 'pst_new' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<Composer open onOpenChange={() => {}} collectionId="col_1" />, { mode: 'local' })
    await user.type(await screen.findByTestId('post-content'), 'Written, not yet meant to go out')
    // What Enter in a one-line field does in a browser (jsdom does not): the real one is in e2e/ui.
    fireEvent.submit(screen.getByTestId('composer'))
    await new Promise((r) => setTimeout(r, 50))

    expect(api.find('POST', '/v1/posts')).toHaveLength(0)
  })
})
