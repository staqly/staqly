import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { forgetTrialUsedSeen, TrialUsedDialog } from '@/components/billing/trial-used-dialog'
import { OAuthCallback } from '@/components/channels/oauth-callback'
import { ApiError } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'
import { closeTrialUsed } from '@/lib/trial-used'

import { nav } from './navigation'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

function billing(state: { review?: string | null; revoked?: string | null; status?: string } = {}) {
  return {
    enabled: true,
    plan: 'starter',
    subscription_status: state.status ?? 'trialing',
    plan_expires_at: null,
    trial_ends_at: null,
    suspended: false,
    has_customer: true,
    seats: 1,
    trial: 'none',
    trial_revoked: state.revoked ?? null,
    trial_review: state.review ?? null,
  }
}

function setup(role: 'owner' | 'admin' | 'editor', state: Parameters<typeof billing>[0] = {}) {
  const client = makeClient()

  client.setQueryData(qk.session, { ...sessionFixture('cloud'), role, accounts_enabled: true })
  const api = mockApi({
    'GET /v1/billing': billing(state),
    'POST /v1/billing/start-paid-plan': billing({ status: 'active' }),
  })

  renderWithProviders(<TrialUsedDialog />, { client })

  return api
}

const paused = () =>
  new ApiError(402, {
    code: 'trial_already_used',
    message: "These accounts were used in an earlier free trial that wasn't paid for",
    details: { reason: 'channels' },
  })

describe('a free trial that reused an unpaid one', () => {
  beforeEach(() => forgetTrialUsedSeen())
  afterEach(() => {
    nav.reset()
    act(() => closeTrialUsed())
  })

  it('explains a paused trial after a refused connect, with both ways out', async () => {
    const api = setup('owner')

    expect(screen.queryByTestId('trial-used')).not.toBeInTheDocument()
    act(() => toastApiError(paused()))

    const dialog = await screen.findByTestId('trial-used')

    expect(within(dialog).getByText('Your free trial is paused')).toBeInTheDocument()
    expect(dialog).toHaveTextContent('Nothing has been charged')
    expect(within(dialog).getByRole('link', { name: /Contact us/ })).toHaveAttribute(
      'href',
      expect.stringMatching(/^mailto:support@staqly\.ai/),
    )
    await userEvent.setup().click(within(dialog).getByRole('button', { name: /Start paid plan now/ }))
    await waitFor(() => expect(api.find('POST', '/v1/billing/start-paid-plan')).toHaveLength(1))
    await waitFor(() => expect(screen.queryByTestId('trial-used')).not.toBeInTheDocument())
  })

  it('tells people who cannot pay who can', async () => {
    setup('editor')
    act(() => toastApiError(paused()))

    const dialog = await screen.findByTestId('trial-used')

    expect(dialog).toHaveTextContent('An owner of this workspace can start the paid plan')
    expect(within(dialog).queryByRole('button', { name: /Start paid plan now/ })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('link', { name: /Contact us/ })).toBeInTheDocument()
  })

  it('opens from the billing state once a page load', async () => {
    setup('admin', { review: 'channels' })

    const dialog = await screen.findByTestId('trial-used')

    expect(dialog).toHaveAttribute('data-kind', 'paused')
    await userEvent.setup().click(within(dialog).getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByTestId('trial-used')).not.toBeInTheDocument())
  })

  it('says a card used for a trial before means no trial, charged or not', async () => {
    setup('owner', { revoked: 'card', status: 'active' })

    const dialog = await screen.findByTestId('trial-used')

    expect(dialog).toHaveTextContent('This card was already used for a free trial')
    expect(dialog).toHaveTextContent('Your plan was charged today')
  })

  it('a card whose charge failed points to Plan & usage', async () => {
    setup('owner', { revoked: 'card', status: 'past_due' })

    const dialog = await screen.findByTestId('trial-used')

    expect(dialog).toHaveTextContent("the charge didn't go through")
    expect(within(dialog).getByRole('link', { name: /Go to Plan & usage/ })).toHaveAttribute('href', '/settings/plan')
  })

  it('stays closed when nothing happened', async () => {
    setup('owner')
    await waitFor(() => expect(screen.queryByTestId('trial-used')).not.toBeInTheDocument())
  })

  it('a sign-in refused for it lands on Channels with the explanation, not the error', async () => {
    nav.set(
      '/channels/callback/fakeoauth',
      `error=${encodeURIComponent('These accounts were used')}&code=trial_already_used`,
    )
    setup('owner')
    renderWithProviders(<OAuthCallback />)
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/channels', undefined))
    expect(await screen.findByTestId('trial-used')).toHaveTextContent('Your free trial is paused')
  })
})
