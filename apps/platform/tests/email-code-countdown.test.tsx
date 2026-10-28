import { act, fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { EmailCodeForm } from '@/components/auth/email-sign-in'

import { renderWithProviders } from './utils'

describe('email code countdown', () => {
  afterEach(() => vi.useRealTimers())

  it('a new code counts its wait from when it was sent, not from the last tick of the previous wait', async () => {
    // Only the clock and the countdown's interval are fake; promises and React Query's timers stay real.
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    renderWithProviders(
      <EmailCodeForm
        email="jane@example.com"
        sent={{ resend_in: 2, expires_in: 600 }}
        verify={() => Promise.resolve({})}
        resend={() => Promise.resolve({ resend_in: 30, expires_in: 600 })}
        onDone={() => {}}
        onBack={() => {}}
      />,
    )
    const button = screen.getByTestId('resend-code')

    expect(button).toHaveTextContent('Send a new code in 2 s')
    act(() => vi.advanceTimersByTime(3000))
    expect(button).toHaveTextContent('Send a new code')
    expect(button).toBeEnabled()

    // Two minutes later (the interval stopped at 0, nothing ticks meanwhile), ask for a new code.
    vi.setSystemTime(Date.now() + 120_000)
    fireEvent.click(button)
    expect(await screen.findByText('Send a new code in 30 s')).toBeInTheDocument()
  })
})

const renderForm = () => {
  const verify = vi.fn<(code: string) => Promise<unknown>>(() => new Promise(() => {}))

  renderWithProviders(
    <EmailCodeForm
      email="jane@example.com"
      sent={{ resend_in: 30, expires_in: 600 }}
      verify={verify}
      resend={() => Promise.resolve({ resend_in: 30, expires_in: 600 })}
      onDone={() => {}}
      onBack={() => {}}
    />,
  )

  return { verify, input: screen.getByLabelText(/code/i) }
}

describe('email code field', () => {
  it('is a one-time-code text field of 6 digits', () => {
    const { input } = renderForm()

    expect(input).toHaveAttribute('type', 'text')
    expect(input).toHaveAttribute('inputmode', 'numeric')
    expect(input).toHaveAttribute('autocomplete', 'one-time-code')
    expect(input).toHaveAttribute('maxlength', '6')
  })

  it('typed digit by digit, keeps the leading zero and submits at 6', async () => {
    const { verify, input } = renderForm()

    await userEvent.type(input, '012345')
    expect(input).toHaveValue('012345')
    expect(verify).toHaveBeenCalledExactlyOnceWith('012345')
  })

  it('pasted or autofilled whole, keeps the leading zero and submits once', async () => {
    const { verify, input } = renderForm()

    input.focus()
    await userEvent.paste('000123')
    expect(input).toHaveValue('000123')
    expect(verify).toHaveBeenCalledExactlyOnceWith('000123')
  })
})
