import { screen, waitFor, within, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { PostActions, PostStats } from '@/components/posts/post-actions'
import { PreviewPage } from '@/components/preview/preview-page'

import { comment, post, target } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const published = post({
  id: 'pst_1',
  status: 'published',
  targets: [
    target({
      channel_id: 'ch_x',
      channel_name: 'JumpTheBear',
      provider: 'x',
      status: 'published',
      external_url: 'https://x.com/a/status/1',
    }),
  ],
})

describe('post actions', () => {
  it('opens the post on its platform, duplicates into a new draft and explains what deleting does', async () => {
    const api = mockApi({
      'GET /v1/posts/pst_1/comments': { data: [] },
      'POST /v1/posts/pst_1/duplicate': post({ id: 'pst_2', status: 'draft' }),
      'DELETE /v1/posts/pst_1': { status: 204 },
    })
    const onDeleted = vi.fn<() => void>()
    const user = userEvent.setup()

    renderWithProviders(<PostActions post={published} onDeleted={onDeleted} />)
    expect(screen.getByRole('link', { name: 'Open post on JumpTheBear' })).toHaveAttribute(
      'href',
      'https://x.com/a/status/1',
    )
    await user.click(screen.getByRole('button', { name: 'Duplicate' }))
    await waitFor(() => expect(nav.push).toHaveBeenCalledWith('/posts/pst_2'))
    expect(api.find('POST', '/v1/posts/pst_1/duplicate')).toHaveLength(1)

    await user.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('alertdialog')

    expect(dialog).toHaveTextContent('removed from Staqly only')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/posts/pst_1')).toHaveLength(1))
    await waitFor(() => expect(onDeleted).toHaveBeenCalled())
  })

  it('shows numbers per channel and says when a platform gives none', async () => {
    const withTwo = post({
      id: 'pst_1',
      status: 'published',
      targets: [
        target({
          channel_id: 'ch_x',
          channel_name: 'JumpTheBear',
          provider: 'x',
          status: 'published',
          external_url: 'https://x.com/a/status/1',
        }),
        target({
          channel_id: 'ch_t',
          channel_name: 'Telegram news',
          provider: 'telegram',
          status: 'published',
          external_url: 'https://t.me/news/2',
        }),
      ],
    })

    mockApi({
      'GET /v1/posts/pst_1/comments': { data: [] },
      'GET /v1/posts/pst_1/metrics': {
        data: [
          {
            channel_id: 'ch_x',
            channel_name: 'JumpTheBear',
            provider: 'x',
            status: 'published',
            external_url: '',
            available: true,
            metrics: { impressions: 1200, likes: 34 },
            updated_at: new Date().toISOString(),
          },
          {
            channel_id: 'ch_t',
            channel_name: 'Telegram news',
            provider: 'telegram',
            status: 'published',
            external_url: '',
            available: false,
            metrics: null,
            updated_at: null,
          },
        ],
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostActions post={withTwo} onDeleted={() => {}} />)
    await user.click(screen.getByRole('button', { name: 'Open post' }))
    expect(await screen.findByRole('menuitem', { name: /Telegram news/ })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    // The numbers are the sheet's Statistics tab now, not a header button.
    expect(screen.queryByRole('button', { name: 'Post statistics' })).not.toBeInTheDocument()
    cleanup()
    renderWithProviders(<PostStats post={withTwo} active />)
    const stats = await screen.findByTestId('post-stats')

    expect(await within(stats).findByText('1,200')).toBeInTheDocument()
    expect(within(stats).getByText('Views')).toBeInTheDocument()
    expect(within(stats).getByText('Not available for this platform.')).toBeInTheDocument()
  })

  it('creates a preview link to copy and lists the feedback threads with emails, replies and resolves', async () => {
    const api = mockApi({
      'GET /v1/posts/pst_1/comments': {
        data: [
          comment({
            id: 'cmt_1',
            author_name: 'Ana',
            author_email: 'ana@example.com',
            body: 'Shorter, please',
            quote: 'v1.4',
            channel_id: 'ch_x',
            anchor_start: 0,
            anchor_end: 4,
          }),
          comment({ id: 'cmt_2', parent_id: 'cmt_1', author_name: 'Ben', body: 'Agreed' }),
          comment({ id: 'cmt_3', author_name: 'Cy', body: 'Old note', quote: 'gone words', text_changed: true }),
        ],
      },
      'POST /v1/posts/pst_1/comments': comment({ id: 'cmt_4', parent_id: 'cmt_1', author_kind: 'team', body: 'Done' }),
      'POST /v1/posts/pst_1/comments/cmt_1/resolve': comment({ id: 'cmt_1', resolved_at: new Date().toISOString() }),
      'GET /v1/posts/pst_1/previews': { data: [] },
      'POST /v1/posts/pst_1/previews': {
        id: 'prv_1',
        post_id: 'pst_1',
        created_at: new Date().toISOString(),
        expires_at: null,
        revoked_at: null,
        active: true,
        url: 'http://localhost:8080/preview#tok',
        token: 'tok',
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<PostActions post={published} onDeleted={() => {}} />)
    await user.click(await screen.findByRole('button', { name: 'Preview and feedback, 2 open comments' }))
    const share = await screen.findByTestId('post-share')

    expect(await within(share).findByText('ana@example.com')).toBeInTheDocument()
    const first = within(share).getAllByTestId('feedback-thread')[0]!

    expect(within(first).getByText('Shorter, please')).toBeInTheDocument()
    expect(within(first).getByText('Agreed')).toBeInTheDocument()
    expect(within(first).getByText('In the post on JumpTheBear')).toBeInTheDocument()
    expect(within(share).getByText('Text changed')).toBeInTheDocument()
    await user.click(within(first).getByRole('button', { name: 'Reply' }))
    await user.type(within(first).getByRole('textbox', { name: 'Reply to Ana' }), 'Done')
    await user.click(within(first).getByRole('button', { name: 'Reply' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/comments')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts/pst_1/comments')[0]!.body).toEqual({ body: 'Done', parent_id: 'cmt_1' })
    await user.click(within(first).getByRole('button', { name: 'Resolve' }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/comments/cmt_1/resolve')).toHaveLength(1))
    await user.click(within(share).getByTestId('create-preview'))
    expect(await within(share).findByTestId('preview-url')).toHaveTextContent('http://localhost:8080/preview#tok')
    expect(api.find('POST', '/v1/posts/pst_1/previews')[0]!.body).toEqual({ expires_in_days: 7 })
  })
})

describe('preview page', () => {
  it('shows each channel version and takes a comment without signing in', async () => {
    window.location.hash = '#tok_abc'
    const api = mockApi({
      'GET /v1/previews/tok_abc': {
        workspace: 'Acme',
        content: 'v1.4 is out',
        status: 'scheduled',
        publish_at: '2099-01-02T09:00:00Z',
        published_at: null,
        media: [],
        thread: [],
        targets: [
          {
            channel_id: 'ch_x',
            channel_name: 'JumpTheBear',
            handle: '@JumpTheBear',
            avatar_url: '',
            provider: 'x',
            provider_name: 'X',
            content: 'v1.4!',
            status: 'pending',
            external_url: '',
            published_at: null,
          },
        ],
        comments: [],
        expires_at: null,
      },
      'POST /v1/previews/tok_abc/comments': {
        id: 'cmt_1',
        post_id: 'pst_1',
        author_name: 'Ana',
        body: 'Love it',
        quote: '',
        channel_id: '',
        created_at: new Date().toISOString(),
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<PreviewPage />)
    expect(await screen.findByText('v1.4!')).toBeInTheDocument()
    expect(screen.getByText('@JumpTheBear')).toBeInTheDocument()
    expect(screen.getByText(/Scheduled for/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Post comment' }))
    expect(await screen.findByText('Write a comment.')).toBeInTheDocument()
    await user.type(screen.getByRole('textbox', { name: /^Comment/ }), 'Love it')
    await user.click(screen.getByRole('button', { name: 'Post comment' }))
    // Who is commenting is asked once, before the first comment.
    const who = await screen.findByRole('dialog', { name: 'Who is commenting?' })

    await user.click(within(who).getByRole('button', { name: 'Continue' }))
    expect(await within(who).findByText(/Say who you are/)).toBeInTheDocument()
    await user.type(within(who).getByLabelText(/Your name/), 'Ana')
    await user.click(within(who).getByRole('button', { name: 'Continue' }))
    await waitFor(() => expect(api.find('POST', '/v1/previews/tok_abc/comments')).toHaveLength(1))
    expect(api.find('POST', '/v1/previews/tok_abc/comments')[0]!.body).toEqual({ name: 'Ana', body: 'Love it' })
    window.location.hash = ''
  })

  it('says so when the link no longer works', async () => {
    window.location.hash = '#gone'
    mockApi({ 'GET /v1/previews/gone': { status: 404, body: { error: { code: 'not_found', message: 'gone' } } } })
    renderWithProviders(<PreviewPage />)
    expect(await screen.findByText('This preview is not available')).toBeInTheDocument()
    window.location.hash = ''
  })
})
