import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PreviewPage } from '@/components/preview/preview-page'
import { anchorFromRange, marksFor, segments, threadsOf } from '@/lib/annotations'

import { comment } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

function selectIn(el: Element, start: number, end: number) {
  const text = el.firstChild!
  const range = document.createRange()

  range.setStart(text, start)
  range.setEnd(text, end)

  return range
}

describe('annotations', () => {
  it('turns a selection into a passage: offsets in the text, spaces around it left out', () => {
    const p = document.createElement('p')

    p.textContent = 'Our café opens Monday'
    document.body.append(p)
    // " opens " selected with its spaces.
    expect(anchorFromRange(p, selectIn(p, 8, 15))).toEqual({ quote: 'opens', start: 9, end: 14 })
    expect(anchorFromRange(p, selectIn(p, 8, 9))).toBeNull()
    const outside = document.createElement('p')

    outside.textContent = 'elsewhere'
    document.body.append(outside)
    expect(anchorFromRange(p, selectIn(outside, 0, 4))).toBeNull()
    p.remove()
    outside.remove()
  })

  it('cuts a text at overlapping passages and clamps marks outside it', () => {
    expect(
      segments('abcdefgh', [
        { id: 'a', start: 1, end: 5 },
        { id: 'b', start: 3, end: 7 },
        { id: 'c', start: 6, end: 99 },
      ]),
    ).toEqual([
      { text: 'a', start: 0, ids: [] },
      { text: 'bc', start: 1, ids: ['a'] },
      { text: 'de', start: 3, ids: ['a', 'b'] },
      { text: 'f', start: 5, ids: ['b'] },
      { text: 'g', start: 6, ids: ['b', 'c'] },
      { text: 'h', start: 7, ids: ['c'] },
    ])
    expect(segments('abc', [{ id: 'x', start: 5, end: 2 }])).toEqual([{ text: 'abc', start: 0, ids: [] }])
  })

  it('groups replies under their thread and marks only open threads with a passage still in the text', () => {
    const threads = threadsOf([
      comment({ id: 'r1', quote: 'café', channel_id: 'ch', anchor_start: 4, anchor_end: 8 }),
      comment({ id: 'x1', parent_id: 'r1' }),
      comment({ id: 'r2', quote: 'gone', channel_id: 'ch', text_changed: true }),
      comment({
        id: 'r3',
        quote: 'Our',
        channel_id: 'ch',
        anchor_start: 0,
        anchor_end: 3,
        resolved_at: '2026-01-01T00:00:00Z',
      }),
      comment({ id: 'r4', quote: 'Monday', channel_id: 'ch', part: 1, anchor_start: 0, anchor_end: 6 }),
    ])

    expect(threads.map((t) => [t.root.id, t.replies.map((r) => r.id)])).toEqual([
      ['r1', ['x1']],
      ['r2', []],
      ['r3', []],
      ['r4', []],
    ])
    expect(marksFor(threads, 'ch', 0)).toEqual([{ id: 'r1', start: 4, end: 8 }])
    expect(marksFor(threads, 'ch', 1)).toEqual([{ id: 'r4', start: 0, end: 6 }])
  })
})

const preview = (comments: ReturnType<typeof comment>[]) => ({
  workspace: 'Acme',
  content: 'Our café opens Monday',
  status: 'draft',
  publish_at: null,
  published_at: null,
  media: [],
  thread: [{ content: 'Doors at 9', media: [], delay_seconds: 0 }],
  targets: [
    {
      channel_id: 'ch_x',
      channel_name: 'JumpTheBear',
      handle: '@JumpTheBear',
      avatar_url: '',
      provider: 'x',
      provider_name: 'X',
      content: 'Our café opens Monday',
      status: 'pending',
      external_url: '',
      published_at: null,
    },
  ],
  comments,
  expires_at: null,
})

describe('preview comments on a passage', () => {
  it('comments on selected words, with the passage, as the remembered reviewer', async () => {
    window.location.hash = '#tok_sel'
    localStorage.setItem('staqly:reviewer', JSON.stringify({ name: 'Ana', email: '' }))
    const api = mockApi({
      'GET /v1/previews/tok_sel': preview([]),
      'POST /v1/previews/tok_sel/comments': comment({ id: 'cmt_9' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<PreviewPage />)
    const card = await screen.findByRole('article', { name: 'X: JumpTheBear' })
    const text = within(card).getByText('Our café opens Monday')

    act(() => {
      const sel = window.getSelection()!

      sel.removeAllRanges()
      sel.addRange(selectIn(text, 9, 21))
      document.dispatchEvent(new Event('selectionchange'))
    })
    await user.click(await screen.findByTestId('comment-selection'))
    await waitFor(() => expect(screen.getByRole('textbox', { name: 'Comment on the selection' })).toHaveFocus())
    expect(screen.getAllByText('opens Monday').length).toBeGreaterThan(0)
    await user.type(screen.getByRole('textbox', { name: 'Comment on the selection' }), 'Say the date')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))
    await waitFor(() => expect(api.find('POST', '/v1/previews/tok_sel/comments')).toHaveLength(1))
    expect(api.find('POST', '/v1/previews/tok_sel/comments')[0]!.body).toEqual({
      name: 'Ana',
      body: 'Say the date',
      channel_id: 'ch_x',
      part: 0,
      quote: 'opens Monday',
      anchor_start: 9,
      anchor_end: 21,
    })
    localStorage.clear()
    window.location.hash = ''
  })

  it('marks commented passages, opens their thread, folds resolved threads and resolves as the reviewer', async () => {
    window.location.hash = '#tok_hl'
    localStorage.setItem('staqly:reviewer', JSON.stringify({ name: 'Ben', email: '' }))
    const api = mockApi({
      'GET /v1/previews/tok_hl': preview([
        comment({
          id: 'cmt_a',
          quote: 'opens',
          channel_id: 'ch_x',
          anchor_start: 9,
          anchor_end: 14,
          body: 'Opens when?',
        }),
        comment({ id: 'cmt_b', parent_id: 'cmt_a', author_name: 'Cy', body: 'At nine' }),
        comment({
          id: 'cmt_c',
          quote: 'Doors',
          channel_id: 'ch_x',
          part: 1,
          anchor_start: 0,
          anchor_end: 5,
          body: 'Fixed',
          resolved_at: new Date().toISOString(),
          resolved_by: 'Ana',
        }),
      ]),
      'POST /v1/previews/tok_hl/comments/cmt_a/resolve': comment({ id: 'cmt_a' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<PreviewPage />)
    const card = await screen.findByRole('article', { name: 'X: JumpTheBear' })
    const mark = within(card).getByRole('button', { name: 'opens' })

    expect(mark).toHaveAttribute('aria-describedby', 'thread-cmt_a')
    // The resolved thread's passage is not marked.
    expect(within(card).queryByRole('button', { name: 'Doors' })).not.toBeInTheDocument()
    await user.click(mark)
    const thread = document.getElementById('thread-cmt_a')!

    expect(thread).toHaveFocus()
    expect(within(thread).getByText('At nine')).toBeInTheDocument()
    expect(within(thread).getByText('In the post on JumpTheBear')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Resolved (1)' }))
    expect(within(screen.getByTestId('resolved-threads')).getByText('Fixed')).toBeInTheDocument()
    expect(screen.getByText('Resolved by Ana')).toBeInTheDocument()
    await user.click(within(thread).getByRole('button', { name: 'Resolve' }))
    await waitFor(() => expect(api.find('POST', '/v1/previews/tok_hl/comments/cmt_a/resolve')).toHaveLength(1))
    expect(api.find('POST', '/v1/previews/tok_hl/comments/cmt_a/resolve')[0]!.body).toEqual({ name: 'Ben' })
    localStorage.clear()
    window.location.hash = ''
  })
})

describe('preview page loading and errors', () => {
  it('keeps the post and the comment being typed when a background refresh fails', async () => {
    window.location.hash = '#tok_blip'
    let fail = false

    mockApi({
      'GET /v1/previews/tok_blip': () =>
        fail ? { status: 503, body: { error: { code: 'unavailable', message: 'Down' } } } : preview([]),
    })
    const user = userEvent.setup()
    const { client } = renderWithProviders(<PreviewPage />)

    await screen.findByRole('article', { name: 'X: JumpTheBear' })
    await user.type(screen.getByPlaceholderText('What would you change?'), 'Half a thought')
    fail = true
    await act(() => client.refetchQueries({ queryKey: ['preview', 'tok_blip'] }))
    await waitFor(() => expect(client.getQueryState(['preview', 'tok_blip'])?.status).toBe('error'))
    expect(screen.queryByText('This preview is not available')).not.toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'X: JumpTheBear' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('What would you change?')).toHaveValue('Half a thought')
    window.location.hash = ''
  })

  it('says a link without a token or one that fails to open is not available', async () => {
    window.location.hash = ''
    mockApi({})
    const { unmount } = renderWithProviders(<PreviewPage />)

    expect(screen.getByText('This preview is not available')).toBeInTheDocument()
    unmount()

    window.location.hash = '#tok_gone'
    mockApi({ 'GET /v1/previews/tok_gone': { status: 404, body: { error: { code: 'not_found', message: 'Gone' } } } })
    renderWithProviders(<PreviewPage />)
    expect(await screen.findByText('This preview is not available')).toBeInTheDocument()
    window.location.hash = ''
  })
})
