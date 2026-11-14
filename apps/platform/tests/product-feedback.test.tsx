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
    const second = new File(['second'], 'second.webp', { type: 'image/webp' })
    const screenshot = new File(['image'], 'screenshot.png', { type: 'image/png' })

    window.history.replaceState({}, '', '/schedule?token=private#secret')
    await user.type(screen.getByLabelText(/Description/), 'The schedule is blank')
    await user.upload(screen.getByLabelText(/Screenshot/), [screenshot, second])
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
    expect(body.getAll('screenshot')).toEqual([screenshot, second])
    expect(calls[1].headers['X-CSRF-Token']).toBeTruthy()
  })

  it('sends a question to the AI Assistant, or to the docs when the server has none', async () => {
    const close = vi.fn<(open: boolean) => void>()
    const ask = vi.fn<() => void>()
    const { unmount } = renderWithProviders(<FeedbackDialog open onOpenChange={close} onAskAssistant={ask} />)

    await userEvent.click(screen.getByRole('button', { name: /Need help\?.*AI Assistant/ }))
    expect(close).toHaveBeenCalledWith(false)
    expect(ask).toHaveBeenCalledOnce()

    unmount()
    renderWithProviders(<FeedbackDialog open onOpenChange={close} />)
    expect(screen.getByRole('link', { name: /Need help\?/ })).toHaveAttribute('href', 'https://docs.staqly.ai')
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
  it('limits selection to ten screenshots and allows removing and adding files', async () => {
    const api = mockApi({ 'POST /v1/feedback': { status: 201, body: { id: 'fb_3' } } })

    renderFeedback()
    const user = userEvent.setup()
    const input = screen.getByLabelText(/Screenshot/)
    const files = Array.from({ length: 10 }, (_, i) => new File(['image'], `shot-${i}.png`, { type: 'image/png' }))
    const extra = new File(['extra'], 'extra.png', { type: 'image/png' })

    await user.type(screen.getByLabelText(/Description/), 'Problem')
    await user.upload(input, files)
    expect(screen.getAllByRole('button', { name: /Remove screenshot/ })).toHaveLength(10)
    await user.upload(input, extra)
    expect(await screen.findByRole('alert')).toHaveTextContent('at most 10')
    expect(screen.getAllByRole('button', { name: /Remove screenshot/ })).toHaveLength(10)
    await user.click(screen.getByRole('button', { name: 'Remove screenshot 1: shot-0.png' }))
    await user.upload(input, extra)
    await user.click(screen.getByRole('button', { name: 'Send feedback' }))
    await waitFor(() => expect(api.find('POST', '/v1/feedback')).toHaveLength(1))
    expect((api.find('POST', '/v1/feedback')[0].body as FormData).getAll('screenshot')).toEqual([
      ...files.slice(1),
      extra,
    ])
  })
})
