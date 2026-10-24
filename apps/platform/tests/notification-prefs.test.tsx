import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { NotificationPrefsCard } from '@/components/settings/notification-prefs'
import { notificationsFor } from '@/lib/notifications'

import { mockApi, renderWithProviders } from './utils'

const EVENTS = [
  'post.published',
  'post.failed',
  'post.feedback_added',
  'channel.needs_reconnect',
  'schedule.posts_requested',
  'invitation.accepted',
]
const prefs = {
  scope: 'mine',
  email: 'ana@example.com',
  mail_ready: true,
  events: EVENTS.map((event) => ({ event, email: true, in_app: true })),
}

describe('notification settings', () => {
  it('lists every event with email and bell switches and saves the changes once', async () => {
    const api = mockApi({ 'GET /v1/account/notifications': prefs, 'PUT /v1/account/notifications': prefs })
    const user = userEvent.setup()

    renderWithProviders(<NotificationPrefsCard />, { mode: 'cloud' })
    expect(await screen.findByText(/by email to ana@example.com/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
    await user.click(screen.getByRole('switch', { name: 'Post published: email' }))
    await user.selectOptions(screen.getByLabelText('Posts to tell me about'), 'all')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(api.find('PUT', '/v1/account/notifications')).toHaveLength(1))
    const body = api.find('PUT', '/v1/account/notifications')[0]!.body as typeof prefs

    expect(body.scope).toBe('all')
    expect(body.events.find((c) => c.event === 'post.published')).toEqual({
      event: 'post.published',
      email: false,
      in_app: true,
    })
  })

  it('leaves out an event this dashboard has no name for, and saves it unchanged', async () => {
    const withNew = { ...prefs, events: [...prefs.events, { event: 'post.archived', email: true, in_app: false }] }
    const api = mockApi({ 'GET /v1/account/notifications': withNew, 'PUT /v1/account/notifications': withNew })
    const user = userEvent.setup()

    renderWithProviders(<NotificationPrefsCard />, { mode: 'cloud' })
    await user.click(await screen.findByRole('switch', { name: 'Post published: email' }))
    expect(screen.queryByText('post.archived')).not.toBeInTheDocument()
    expect(screen.getAllByRole('row')).toHaveLength(EVENTS.length + 1)
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(api.find('PUT', '/v1/account/notifications')).toHaveLength(1))
    const body = api.find('PUT', '/v1/account/notifications')[0]!.body as typeof prefs

    expect(body.events.find((c) => c.event === 'post.archived')).toEqual({
      event: 'post.archived',
      email: true,
      in_app: false,
    })
  })

  it('says when the server cannot send email', async () => {
    mockApi({ 'GET /v1/account/notifications': { ...prefs, mail_ready: false } })
    renderWithProviders(<NotificationPrefsCard />, { mode: 'cloud' })
    expect(await screen.findByText(/only the bell works/)).toBeInTheDocument()
  })

  it('the bell leaves out events turned off for it', () => {
    const events = [
      { id: 'e1', seq: 1, type: 'post.published', created_at: '', data: { post: { content: 'Hi' } } },
      { id: 'e2', seq: 2, type: 'post.failed', created_at: '', data: { post: { content: 'Oops' } } },
    ] as unknown as Parameters<typeof notificationsFor>[0]

    expect(notificationsFor(events)).toHaveLength(2)
    expect(notificationsFor(events, new Set(['post.published'])).map((n) => n.id)).toEqual(['e2'])
  })
})
