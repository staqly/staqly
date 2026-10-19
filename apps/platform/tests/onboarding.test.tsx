import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { SetupRedirect } from '@/components/auth/setup-redirect'
import { SignIn } from '@/components/auth/sign-in'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const CLOUD = {
  initialized: true,
  mode: 'cloud',
  signup_open: true,
  setup_token_required: false,
  google_sign_in: true,
  email_sign_in: true,
}
const SENT = { status: 202, body: { resend_in: 30, expires_in: 600 } }

describe('cloud sign-up (create your account)', () => {
  it('signs up with an email code and lands in the welcome; no API key, no workspace form', async () => {
    const api = mockApi({
      'GET /v1/setup': { ...CLOUD, legacy_sign_in: true },
      'POST /v1/auth/email/start': SENT,
      'POST /v1/auth/email/verify': { status: 201, body: { csrf_token: 'c', next: '/welcome' } },
    })
    const user = userEvent.setup()

    nav.set('/signup')
    renderWithProviders(<SignIn signup />)

    expect(await screen.findByRole('heading', { name: 'Create your account' })).toBeInTheDocument()
    expect(screen.getByText(/starter is free for 7 days/i)).toBeInTheDocument()
    expect(screen.getByTestId('google-sign-in')).toHaveAttribute('href', expect.stringContaining('next=%2Fwelcome'))
    // Even a development server (legacy sign-in on) offers no API key sign-in or workspace form here.
    expect(screen.queryByText(/api key/i)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/workspace name/i)).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login')

    await user.type(screen.getByLabelText(/email address/i), 'new@example.com')
    await user.click(screen.getByTestId('send-code'))
    await user.type(await screen.findByLabelText(/^code/i), '123456')
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/welcome', undefined))
    expect(api.find('POST', '/v1/auth/email/verify')[0].body).toMatchObject({ next: '/welcome' })
    expect(api.find('POST', '/v1/workspaces')).toHaveLength(0)
  })

  it('a plan picked on the pricing page opens the plan picker after sign-up', async () => {
    mockApi({ 'GET /v1/setup': CLOUD })
    nav.set('/signup', 'plan=pro&interval=year')
    renderWithProviders(<SignIn signup />)

    expect(await screen.findByTestId('google-sign-in')).toHaveAttribute(
      'href',
      expect.stringContaining(`next=${encodeURIComponent('/plans?plan=pro&interval=year')}`),
    )
    // The way back to sign-in keeps the picked plan.
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login?plan=pro&interval=year')
  })

  it('asks for the setup token on a closed server that has one', async () => {
    mockApi({ 'GET /v1/setup': { ...CLOUD, signup_open: false, setup_token_required: true } })
    renderWithProviders(<SignIn signup />)
    expect(await screen.findByLabelText('Setup token')).toBeInTheDocument()
  })

  it('says sign-ups are by invitation when they are closed', async () => {
    mockApi({ 'GET /v1/setup': { ...CLOUD, signup_open: false } })
    renderWithProviders(<SignIn signup />)
    expect(await screen.findByRole('heading', { name: /by invitation/i })).toBeInTheDocument()
    expect(screen.queryByTestId('google-sign-in')).not.toBeInTheDocument()
  })

  it('the old /setup address moves to /signup with its query', async () => {
    nav.set('/setup', 'plan=plus')
    window.history.replaceState(null, '', '/setup?plan=plus')
    renderWithProviders(<SetupRedirect />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/signup?plan=plus', undefined))
    window.history.replaceState(null, '', '/')
  })
})

describe('cloud sign-in', () => {
  it('offers Google, a passkey and an email code, links to sign-up and never offers an API key', async () => {
    mockApi({ 'GET /v1/setup': { ...CLOUD, legacy_sign_in: true } })
    nav.set('/login')
    renderWithProviders(<SignIn />)

    expect(await screen.findByRole('heading', { name: 'Sign in to Staqly' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Create an account' })).toHaveAttribute('href', '/signup')
    expect(screen.queryByText(/api key/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /lost your key/i })).not.toBeInTheDocument()
    // Cloud mode never bounces from /login ("initialized" is meaningless there).
    expect(nav.replace).not.toHaveBeenCalled()
  })

  it('hides the sign-up link when sign-ups are closed', async () => {
    mockApi({ 'GET /v1/setup': { ...CLOUD, signup_open: false } })
    renderWithProviders(<SignIn />)
    expect(await screen.findByRole('heading', { name: 'Sign in to Staqly' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Create an account' })).not.toBeInTheDocument()
  })
})

describe('local mode', () => {
  it('has no sign-up: it goes straight to the schedule', async () => {
    const api = mockApi({ 'GET /v1/setup': { initialized: true, mode: 'local', setup_token_required: false } })

    renderWithProviders(<SignIn signup />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
    expect(api.find('POST', '/v1/workspaces')).toHaveLength(0)
  })

  it('has no sign-in either', async () => {
    mockApi({ 'GET /v1/setup': { initialized: true, mode: 'local', setup_token_required: false } })
    renderWithProviders(<SignIn />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
  })
})
