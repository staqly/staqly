import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AccountPage } from '@/components/settings/account-page'
import WorkspaceCard from '@/components/settings/workspace-card'
import { qk } from '@/lib/queries'

import { nav } from './navigation'
import { makeClient, mockApi, renderWithProviders, sessionFixture } from './utils'

// jsdom has no canvas: the scaling is the browser's; here the picked file just becomes this image.
const PNG = 'data:image/png;base64,iVBORw0KGgo='

vi.mock('@/lib/image-upload', () => ({ imageToDataUrl: async () => PNG }))

const file = () => new File(['x'], 'logo.png', { type: 'image/png' })
const fileInput = () => document.querySelector<HTMLInputElement>('input[type=file]')!

describe('workspace logo', () => {
  it("is staqly's until one is uploaded; it saves at once and can be removed", async () => {
    const session = sessionFixture('cloud')
    const client = makeClient()

    client.setQueryData(qk.session, session)
    const api = mockApi({
      'PATCH /v1/workspace': (c: { body: { logo?: string } }) => ({ ...session.workspace, logo: c.body.logo ?? '' }),
    })
    const user = userEvent.setup()

    nav.set('/settings')
    renderWithProviders(<WorkspaceCard />, { client })
    expect(await screen.findByRole('button', { name: 'Upload logo' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()

    await user.upload(fileInput(), file())
    await waitFor(() => expect(api.find('PATCH', '/v1/workspace').map((c) => c.body)).toEqual([{ logo: PNG }]))
    expect(await screen.findByRole('button', { name: 'Replace logo' })).toBeInTheDocument()
    expect(client.getQueryData<{ workspace: { logo: string } }>(qk.session)?.workspace.logo).toBe(PNG)

    await user.click(screen.getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/workspace').at(-1)?.body).toEqual({ logo: '' }))
    expect(await screen.findByRole('button', { name: 'Upload logo' })).toBeInTheDocument()
  })
})

describe('profile picture', () => {
  it('is optional: uploaded on the account page, sent as the avatar', async () => {
    const session = {
      ...sessionFixture('cloud'),
      user: { id: 'usr_1', name: 'Jane Doe', email: 'jane@example.com', avatar_url: '' },
      accounts_enabled: true,
      role: 'owner',
      workspaces: [],
    }
    const client = makeClient()

    client.setQueryData(qk.session, session)
    const api = mockApi({
      'GET /v1/account/sign-in-methods': { data: [] },
      'GET /v1/account/notifications': { events: [] },
      'PATCH /v1/account': (c: { body: { avatar?: string } }) => ({ ...session.user, avatar_url: c.body.avatar }),
    })
    const user = userEvent.setup()

    nav.set('/settings/account')
    renderWithProviders(<AccountPage />, { client })
    await user.upload(await waitFor(fileInput), file())
    await waitFor(() => expect(api.find('PATCH', '/v1/account').map((c) => c.body)).toEqual([{ avatar: PNG }]))
    expect(await screen.findByRole('button', { name: 'Replace picture' })).toBeInTheDocument()
  })
})
