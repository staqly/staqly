import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { MembersPage } from '@/components/settings/members-page'
import type { Invitation, Member, Role } from '@/lib/api'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

function member(id: string, name: string, role: Role, you = false): Member {
  return {
    user_id: id,
    name,
    email: `${id}@example.com`,
    avatar_url: '',
    role,
    you,
    created_at: '2026-10-19T00:00:00Z',
  }
}

function invitation(over: Partial<Invitation> = {}): Invitation {
  return {
    id: 'inv_1',
    email: 'sam@example.com',
    role: 'editor',
    invited_by: 'Ana Owner',
    created_at: '2026-11-10T10:00:00Z',
    expires_at: '2026-11-17T10:00:00Z',
    expired: false,
    ...over,
  }
}

const ANA = member('usr_1', 'Ana Owner', 'owner')
const BO = member('usr_2', 'Bo Admin', 'admin')
const CLEO = member('usr_3', 'Cleo', 'editor')

const refused = (status: number, code: string, message: string) => ({ status, body: { error: { code, message } } })

/** The page as `me` (one of the members), with what the workspace answers. */
function open(me: Member, routes: Record<string, unknown> = {}, members = [ANA, BO, CLEO]) {
  const api = mockApi({
    'GET /v1/members': { data: members.map((m) => ({ ...m, you: m.user_id === me.user_id })) },
    'GET /v1/invitations': { data: [] },
    ...routes,
  })

  renderWithProviders(<MembersPage />, { role: me.role })

  return api
}

const roles = () => within(screen.getByLabelText('Role')).getAllByRole('option')
const row = (name: string) => screen.getByText(name).closest('li')!

beforeEach(() => nav.set('/settings/members'))

describe('inviting people', () => {
  it('sends the address and the role, then shows the link once, saying whether it was emailed', async () => {
    const sent = invitation({ role: 'operator', link: 'https://app.staqly.test/invite#tok', emailed: false })
    const api = open(ANA, { 'POST /v1/invitations': { status: 201, body: sent } })
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Invite people' }))

    const dialog = await screen.findByRole('dialog', { name: 'Invite people' })

    await user.type(within(dialog).getByLabelText(/Email/), '  sam@example.com ')
    await user.selectOptions(within(dialog).getByLabelText('Role'), 'operator')
    // What the picked role may do is said before it is given.
    expect(within(dialog).getByText(/Manages content, channels, schedules, webhooks and alerts/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Send invitation' }))

    const ready = await screen.findByRole('dialog', { name: 'Invitation ready' })

    expect(api.find('POST', '/v1/invitations')[0]!.body).toEqual({ email: 'sam@example.com', role: 'operator' })
    expect(ready).toHaveTextContent('sam@example.com joins as operator after signing in as that address.')
    expect(ready).toHaveTextContent('Send this link yourself. It is shown only now:')
    expect(within(ready).getByLabelText('Invitation link')).toHaveValue('https://app.staqly.test/invite#tok')
  })

  it('an invitation the server emailed says so', async () => {
    open(ANA, {
      'POST /v1/invitations': { status: 201, body: invitation({ link: 'https://x.test/i#t', emailed: true }) },
    })
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Invite people' }))
    await user.type(await screen.findByLabelText(/Email/), 'sam@example.com')
    await user.click(screen.getByRole('button', { name: 'Send invitation' }))

    expect(await screen.findByText('We emailed the link. You can also send it yourself:')).toBeInTheDocument()
  })

  it('no address, or one the server refuses, is said under the field; a plan limit in its own words', async () => {
    let answer = refused(422, 'validation_failed', 'invalid input')
    const api = open(ANA, { 'POST /v1/invitations': () => answer })
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Invite people' }))

    const dialog = await screen.findByRole('dialog', { name: 'Invite people' })

    await user.click(within(dialog).getByRole('button', { name: 'Send invitation' }))
    expect(await within(dialog).findByText('Enter an email address such as ana@example.com.')).toBeInTheDocument()
    expect(within(dialog).getByLabelText(/Email/)).toBeInvalid()
    expect(api.find('POST', '/v1/invitations')).toHaveLength(0)

    await user.type(within(dialog).getByLabelText(/Email/), 'sam@')
    await user.click(within(dialog).getByRole('button', { name: 'Send invitation' }))
    await waitFor(() => expect(api.find('POST', '/v1/invitations')).toHaveLength(1))
    expect(within(dialog).getByText('Enter an email address such as ana@example.com.')).toBeInTheDocument()

    answer = refused(409, 'conflict', 'sam@example.com is already a member')
    await user.type(within(dialog).getByLabelText(/Email/), 'example.com')
    await user.click(within(dialog).getByRole('button', { name: 'Send invitation' }))
    expect(await within(dialog).findByText('Sam@example.com is already a member')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: 'Invitation ready' })).not.toBeInTheDocument()
  })

  it('an admin invites up to admin, an owner up to owner', async () => {
    open(BO)
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Invite people' }))

    await screen.findByRole('dialog', { name: 'Invite people' })
    expect(roles().map((o) => o.textContent)).toEqual(['Viewer', 'Editor', 'Operator', 'Admin'])
    // Editor is the role picked first: the least that can write.
    expect(screen.getByLabelText('Role')).toHaveValue('editor')
  })

  it('open invitations say their role and when they end; Resend shows a new link, Revoke asks first', async () => {
    const open1 = invitation()
    const old = invitation({ id: 'inv_2', email: 'old@example.com', role: 'viewer', expired: true })
    const api = open(ANA, {
      'GET /v1/invitations': { data: [open1, old] },
      'POST /v1/invitations/inv_2/resend': { ...old, expired: false, link: 'https://x.test/i#new', emailed: false },
      'DELETE /v1/invitations/inv_1': { status: 204 },
    })
    const user = userEvent.setup()
    const rows = within(await screen.findByTestId('invitations')).getAllByRole('listitem')

    expect(rows[0]).toHaveTextContent('sam@example.com')
    expect(rows[0]).toHaveTextContent(/Editor · expires /)
    expect(rows[1]).toHaveTextContent('Viewer · expired')

    await user.click(within(rows[1]!).getByRole('button', { name: 'Resend' }))
    expect(await screen.findByLabelText('Invitation link')).toHaveValue('https://x.test/i#new')
    await user.click(screen.getByRole('button', { name: 'Done' }))

    await user.click(within(rows[0]!).getByRole('button', { name: 'Revoke' }))

    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Revoke the invitation for sam@example.com?')
    expect(api.find('DELETE', '/v1/invitations/inv_1')).toHaveLength(0)
    await user.click(within(confirm).getByRole('button', { name: 'Revoke' }))
    expect(await screen.findByText('Invitation for sam@example.com revoked')).toBeInTheDocument()
    expect(api.find('DELETE', '/v1/invitations/inv_1')).toHaveLength(1)
  })
})

describe('removing people', () => {
  it('asks first and says what they lose; Cancel removes nobody', async () => {
    const api = open(ANA, { 'DELETE /v1/members/usr_3': { status: 204 } })
    const user = userEvent.setup()
    const cleo = (await screen.findByText('Cleo')).closest('li')!

    await user.click(within(cleo).getByRole('button', { name: 'Remove' }))

    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Remove Cleo?')
    expect(confirm).toHaveTextContent('They lose access at once and are signed out of this workspace.')
    expect(confirm).toHaveTextContent('Their API keys stop working.')
    await user.click(within(confirm).getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument())
    expect(api.find('DELETE', '/v1/members/usr_3')).toHaveLength(0)

    await user.click(within(cleo).getByRole('button', { name: 'Remove' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove' }))
    expect(await screen.findByText('Cleo was removed')).toBeInTheDocument()
    expect(api.find('DELETE', '/v1/members/usr_3')).toHaveLength(1)
  })

  it('an admin removes members but never an owner; leaving takes them out of the workspace', async () => {
    const assign = vi.fn<(url: string) => void>()

    vi.stubGlobal('location', { ...window.location, assign })

    const api = open(BO, { 'DELETE /v1/members/usr_2': { status: 204 } })
    const user = userEvent.setup()

    await screen.findByText('Cleo')
    expect(within(row('Ana Owner')).queryByRole('button')).not.toBeInTheDocument()
    expect(within(row('Ana Owner')).getByText('Owner')).toBeInTheDocument()
    expect(within(row('Cleo')).getByRole('button', { name: 'Remove' })).toBeInTheDocument()

    await user.click(within(row('Bo Admin')).getByRole('button', { name: 'Leave' }))

    const confirm = await screen.findByRole('alertdialog')

    expect(confirm).toHaveTextContent('Leave this workspace?')
    await user.click(within(confirm).getByRole('button', { name: 'Leave' }))
    expect(await screen.findByText('You left the workspace')).toBeInTheDocument()
    expect(api.find('DELETE', '/v1/members/usr_2')).toHaveLength(1)
    expect(assign).toHaveBeenCalledWith('/schedule')
  })

  it('an owner among others hands the workspace over before leaving, and may make another owner', async () => {
    const api = open(ANA, { 'PATCH /v1/members/usr_2': { ...BO, role: 'owner' } })
    const user = userEvent.setup()

    expect(await screen.findByTestId('sole-owner-note')).toHaveTextContent('Make someone else an owner to leave')
    expect(screen.queryByRole('button', { name: 'Leave' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Role of Ana Owner')).not.toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Role of Bo Admin'), 'owner')
    expect(await screen.findByText('Bo Admin is now owner')).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/members/usr_2')[0]!.body).toEqual({ role: 'owner' })
  })

  it('a change the server refuses (the last owner, a seat that cannot be billed) is said and changes nothing', async () => {
    open(ANA, { 'PATCH /v1/members/usr_3': refused(409, 'conflict', 'the workspace needs an owner') })
    const user = userEvent.setup()

    await user.selectOptions(await screen.findByLabelText('Role of Cleo'), 'admin')
    expect(await screen.findByText('The workspace needs an owner')).toBeInTheDocument()
    expect(screen.getByLabelText('Role of Cleo')).toHaveValue('editor')
  })
})
