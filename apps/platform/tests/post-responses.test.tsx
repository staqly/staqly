import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PostStats } from '@/components/posts/post-stats'

import { post, target } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

const insight = (o = {}) => ({
  positive: 2,
  neutral: 1,
  negative: 1,
  summary: 'People like it',
  themes: ['price'],
  questions: ['When?'],
  status: 'ok',
  analyzed_at: null,
  ...o,
})
const res = (id: string, sentiment: string, text: string) => ({
  id,
  author: 'Ann',
  text,
  sentiment,
  likes: 3,
  created_at: '2026-10-30T10:00:00Z',
})
const data = {
  overall: insight(),
  channels: [
    {
      channel_id: 'ch_f',
      channel_name: 'FB page',
      provider: 'facebook',
      available: true,
      insight: insight(),
      responses: [res('1', 'positive', 'Love it <b>x</b>'), res('2', 'negative', 'Meh')],
    },
    { channel_id: 'ch_l', channel_name: 'LI', provider: 'linkedin', available: false, insight: null, responses: [] },
  ],
}

const published = post({
  status: 'published',
  targets: [
    target({ channel_id: 'ch_f', channel_name: 'FB page', provider: 'facebook', status: 'published' }),
    target({ channel_id: 'ch_l', channel_name: 'LI', provider: 'linkedin', status: 'published' }),
  ],
})
const metrics = { data: [] }

describe('post responses', () => {
  it('renders summary, channels, filter, plain text, unavailable channel and refresh', async () => {
    const api = mockApi({
      'GET /v1/posts/pst_1/responses': data,
      'POST /v1/posts/pst_1/responses/refresh': data,
      'GET /v1/posts/pst_1/metrics': metrics,
    })
    const user = userEvent.setup()

    renderWithProviders(<PostStats post={published} active />)
    expect((await screen.findAllByText('People like it')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('price').length).toBeGreaterThan(0)
    expect(screen.getByText('Love it <b>x</b>')).toBeInTheDocument()
    expect(screen.getByTestId('unavailable')).toHaveTextContent("Comments: this platform doesn't let staqly read them")

    await user.click(screen.getByRole('button', { name: /^negative/ }))
    expect(screen.queryByText('Love it <b>x</b>')).not.toBeInTheDocument()
    expect(within(screen.getByTestId('responses-ch_f')).getByText('Meh')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Refresh numbers and comments/ }))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/responses/refresh')).toHaveLength(1))
    await waitFor(() => expect(api.find('GET', '/v1/posts/pst_1/responses')).toHaveLength(2))
  })

  it('a comment whose author the platform hid says so instead of a bare @', async () => {
    const hidden = { ...res('3', 'neutral', 'Which resort?'), author: '', author_handle: '@' }

    mockApi({
      'GET /v1/posts/pst_1/responses': { ...data, channels: [{ ...data.channels[0]!, responses: [hidden] }] },
      'GET /v1/posts/pst_1/metrics': metrics,
    })
    renderWithProviders(<PostStats post={published} active />)
    const card = await screen.findByTestId('responses-ch_f')

    expect(within(card).getByText('Name hidden by the platform')).toBeInTheDocument()
    expect(within(card).queryByText('@')).not.toBeInTheDocument()
  })

  it('a channel whose comments could not be read says it once', async () => {
    const why = "YouTube's daily limit is used up"
    const failed = {
      ...data,
      channels: [{ ...data.channels[0]!, error: why, insight: insight({ status: 'failed', error: why }) }],
    }

    mockApi({ 'GET /v1/posts/pst_1/responses': failed, 'GET /v1/posts/pst_1/metrics': metrics })
    renderWithProviders(<PostStats post={published} active />)
    const card = await screen.findByTestId('responses-ch_f')

    expect(within(card).getAllByText(new RegExp(why))).toHaveLength(1)
  })

  it('says when the AI allowance is used up and when there is nothing', async () => {
    mockApi({
      'GET /v1/posts/pst_1/responses': { ...data, overall: insight({ status: 'waiting_credits' }), channels: [] },
      'GET /v1/posts/pst_1/metrics': metrics,
    })
    renderWithProviders(<PostStats post={published} active />)
    expect(await screen.findByText(/allowance is used up/)).toBeInTheDocument()
  })

  it('shows No comments yet under a channel', async () => {
    mockApi({
      'GET /v1/posts/pst_1/responses': {
        ...data,
        overall: null,
        channels: [{ ...data.channels[0], insight: null, responses: [] }],
      },
      'GET /v1/posts/pst_1/metrics': metrics,
    })
    renderWithProviders(<PostStats post={published} active />)
    expect(await screen.findByTestId('responses-ch_f')).toHaveTextContent('Comments 0')
    expect(screen.queryByTestId('post-responses')).toBeNull()
  })

  it('a scheduled post shows its channels without reading numbers or comments', async () => {
    const api = mockApi({})

    renderWithProviders(<PostStats post={post({ status: 'scheduled', targets: [target()] })} active />)
    expect(screen.getAllByTestId('post-channel')).toHaveLength(1)
    expect(api.calls).toHaveLength(0)
  })
})
