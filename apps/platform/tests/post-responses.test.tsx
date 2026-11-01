import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PostResponses } from '@/components/posts/post-responses'

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

describe('post responses', () => {
  it('renders summary, channels, filter, plain text, unavailable channel and refresh', async () => {
    const api = mockApi({
      'GET /v1/posts/pst_1/responses': data,
      'POST /v1/posts/pst_1/responses/refresh': data,
    })
    const user = userEvent.setup()

    renderWithProviders(<PostResponses postId="pst_1" active />)
    expect((await screen.findAllByText('People like it')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('price').length).toBeGreaterThan(0)
    expect(screen.getByText('Love it <b>x</b>')).toBeInTheDocument()
    expect(screen.getByText("This platform doesn't let staqly read comments")).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'negative' }))
    expect(screen.queryByText('Love it <b>x</b>')).not.toBeInTheDocument()
    expect(within(screen.getByTestId('responses-ch_f')).getByText('Meh')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Refresh comments/ }))
    await waitFor(() => expect(api.calls.some((c) => c.method === 'POST')).toBe(true))
    await waitFor(() => expect(api.calls.filter((c) => c.method === 'GET').length).toBe(2))
  })

  it('says when the AI allowance is used up and when there is nothing', async () => {
    mockApi({
      'GET /v1/posts/pst_1/responses': { ...data, overall: insight({ status: 'waiting_credits' }), channels: [] },
    })
    renderWithProviders(<PostResponses postId="pst_1" active />)
    expect(await screen.findByText(/allowance is used up/)).toBeInTheDocument()
  })

  it('shows No comments yet', async () => {
    mockApi({ 'GET /v1/posts/pst_1/responses': { overall: null, channels: [] } })
    renderWithProviders(<PostResponses postId="pst_1" active />)
    expect(await screen.findByText('No comments yet')).toBeInTheDocument()
  })
})
