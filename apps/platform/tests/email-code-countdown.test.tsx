import { act, fireEvent, screen } from '@testing-library/react'
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
