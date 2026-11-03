import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { FeedbackDialog } from '@/components/feedback/feedback-dialog'

import { mockApi, renderWithProviders } from './utils'

function renderFeedback() {
  const close = vi.fn<(open: boolean) => void>()

  renderWithProviders(<FeedbackDialog open onOpenChange={close} />)

  return close
}

describe('product feedback', () => {
  it('keeps the description and screenshot after an error and allows retry', async () => {
    let fail = true
    const api = mockApi({
      'POST /v1/feedback': () =>
        fail
          ? { status: 500, body: { error: { code: 'internal', message: 'Please try again' } } }
          : { status: 201, body: { id: 'fb_1' } },
    })
    const close = renderFeedback()
    const user = userEvent.setup()
    const screenshot = new File(['image'], 'screenshot.png', { type: 'image/png' })

    window.history.replaceState({}, '', '/schedule?token=private#secret')
    await user.type(screen.getByLabelText(/Description/), 'The schedule is blank')
    await user.upload(screen.getByLabelText(/Screenshot/), screenshot)
    await user.click(screen.getByRole('button', { name: 'Send feedback' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Please try again')
    expect(screen.getByLabelText(/Description/)).toHaveValue('The schedule is blank')
    expect(close).not.toHaveBeenCalled()
    fail = false
    await user.click(screen.getByRole('button', { name: 'Send feedback' }))
    await waitFor(() => expect(close).toHaveBeenCalledWith(false))
    expect(await screen.findByText('Feedback sent. Thank you!')).toBeInTheDocument()

    const calls = api.find('POST', '/v1/feedback')
    const body = calls[1].body as FormData

    expect(calls).toHaveLength(2)
    expect(body.get('page_path')).toBe('/schedule')
    expect(body.get('description')).toBe('The schedule is blank')
    expect(body.get('kind')).toBe('problem')
    expect(body.get('screenshot')).toBe(screenshot)
    expect(calls[1].headers['X-CSRF-Token']).toBeTruthy()
  })

  it('blocks repeated submission and closing while sending', async () => {
    const api = mockApi({ 'POST /v1/feedback': { status: 201, body: { id: 'fb_2' } } })
    const originalFetch = globalThis.fetch
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })

    vi.stubGlobal(
      'fetch',
      vi.fn(async (...args: Parameters<typeof fetch>) => {
        await pending

        return originalFetch(...args)
      }),
    )
    const close = renderFeedback()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText(/Description/), 'My idea')
    const submit = screen.getByRole('button', { name: 'Send feedback' })

    await user.click(submit)
    expect(await screen.findByRole('button', { name: 'Sending...' })).toBeDisabled()
    fireEvent.submit(submit.closest('form')!)
    await user.keyboard('{Escape}')
    expect(close).not.toHaveBeenCalled()
    release()
    await waitFor(() => expect(close).toHaveBeenCalledWith(false))
    expect(api.find('POST', '/v1/feedback')).toHaveLength(1)
  })

  it('refuses an oversized screenshot without sending', async () => {
    const api = mockApi({})

    renderFeedback()
    const user = userEvent.setup()

    await user.type(screen.getByLabelText(/Description/), 'Problem')
    await user.upload(
      screen.getByLabelText(/Screenshot/),
      new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'big.png', { type: 'image/png' }),
    )
    await user.click(screen.getByRole('button', { name: 'Send feedback' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('up to 5 MB')
    expect(api.find('POST', '/v1/feedback')).toHaveLength(0)
  })
})
