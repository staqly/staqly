import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AcceptInvitation } from '@/components/auth/accept-invitation'
import { SignIn } from '@/components/auth/sign-in'
import { SignInMethodsCard } from '@/components/settings/sign-in-methods'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const SETUP = { initialized: true, mode: 'cloud', signup_open: true, setup_token_required: false }
const SENT = { status: 202, body: { resend_in: 30, expires_in: 600 } }

describe('sign-in with an email code', () => {
  it('sends a code, explains a wrong one, then signs in and lands on next', async () => {
    let tries = 0
    const api = mockApi({
      'GET /v1/setup': { ...SETUP, google_sign_in: true, email_sign_in: true },
      'POST /v1/auth/email/start': SENT,
      'POST /v1/auth/email/verify': () =>
        ++tries === 1
          ? { status: 401, body: { error: { code: 'code_invalid', message: 'x', details: { attempts_left: 4 } } } }
          : { status: 201, body: { csrf_token: 'c', next: '/posts' } },
    })
    const user = userEvent.setup()

    nav.set('/login', 'next=%2Fposts')
    renderWithProviders(<SignIn />)

    expect(await screen.findByTestId('google-sign-in')).toBeInTheDocument()
    await user.click(screen.getByTestId('send-code'))
    expect(screen.getByText(/enter an email address/i)).toBeInTheDocument()
    await user.type(screen.getByLabelText(/email address/i), 'Zoe@Example.com')
    await user.click(screen.getByTestId('send-code'))

    expect(await screen.findByRole('heading', { name: /check your email/i })).toBeInTheDocument()
    expect(api.find('POST', '/v1/auth/email/start')[0].body).toEqual({ email: 'zoe@example.com' })
    expect(screen.getByTestId('resend-code')).toBeDisabled()
    expect(screen.getByTestId('resend-code')).toHaveTextContent(/in \d+ s/)

    // A pasted code with spaces submits by itself at 6 digits.
    await user.click(screen.getByLabelText(/^code/i))
    await user.paste('111 111')
    expect(await screen.findByText(/not right\. 4 tries left/i)).toBeInTheDocument()

    await user.type(screen.getByLabelText(/^code/i), '123456')
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/posts', undefined))
    expect(api.find('POST', '/v1/auth/email/verify')[1].body).toMatchObject({
      email: 'zoe@example.com',
      code: '123456',
      next: '/posts',
    })
  })

  it('an expired code says to send a new one; Use another address goes back', async () => {
    mockApi({
      'GET /v1/setup': { ...SETUP, email_sign_in: true },
      'POST /v1/auth/email/start': SENT,
      'POST /v1/auth/email/verify': { status: 410, body: { error: { code: 'code_expired', message: 'x' } } },
    })
    const user = userEvent.setup()

    nav.set('/login')
    renderWithProviders(<SignIn />)

    // Email only: no Google button.
    await user.type(await screen.findByLabelText(/email address/i), 'zoe@example.com')
    expect(screen.queryByTestId('google-sign-in')).not.toBeInTheDocument()
    await user.click(screen.getByTestId('send-code'))
    await user.type(await screen.findByLabelText(/^code/i), '123456')
    expect(await screen.findByText(/send a new one/i)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /use another address/i }))
    expect(await screen.findByLabelText(/email address/i)).toBeInTheDocument()
  })
})

describe('accepting an invitation with a code', () => {
  it('emails the invited address and accepts with the code', async () => {
    const api = mockApi({
      'GET /v1/setup': { ...SETUP, email_sign_in: true },
      'GET /v1/invitations/lookup': {
        workspace: 'Acme',
        email: 'zoe@example.com',
        role: 'editor',
        invited_by: 'Ana',
        status: 'open',
        expires_at: '2030-01-01T00:00:00.000000Z',
      },
      'POST /v1/auth/email/start': SENT,
      'POST /v1/auth/email/verify': { status: 201, body: { csrf_token: 'c', next: '/schedule' } },
    })
    const user = userEvent.setup()

    nav.set('/invite', 'token=staqly-invite-abc')
    renderWithProviders(<AcceptInvitation />)

    await user.click(await screen.findByTestId('accept-with-code'))
    await user.type(await screen.findByLabelText(/^code/i), '654321')
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/schedule', undefined))
    expect(api.find('POST', '/v1/auth/email/verify')[0].body).toEqual({
      email: 'zoe@example.com',
      code: '654321',
      invite: 'staqly-invite-abc',
    })
    expect(screen.queryByTestId('accept-invitation')).not.toBeInTheDocument()
  })
})

describe('sign-in methods', () => {
  const EMAIL = {
    provider: 'email',
    subject: 'zoe@example.com',
    email: 'zoe@example.com',
    name: '',
    created_at: '2026-10-01T00:00:00.000000Z',
    last_login_at: null,
  }
  const NEW = { ...EMAIL, subject: 'zoe@new.example', email: 'zoe@new.example' }
  const GOOGLE = { ...EMAIL, provider: 'google', subject: 'sub-zoe', email: 'zoe@gmail.example' }
  const PASSKEY = { ...EMAIL, provider: 'passkey', subject: 'pk_1', email: '', name: 'Mac' }

  it('a Google-only person adds an address in a drawer and removes one only while another remains', async () => {
    let methods = [GOOGLE]
    const api = mockApi({
      'GET /v1/setup': { ...SETUP, google_sign_in: true, email_sign_in: true },
      'GET /v1/account/sign-in-methods': () => ({ data: methods }),
      'POST /v1/account/sign-in-methods/email/start': SENT,
      'POST /v1/account/sign-in-methods/email/verify': () => {
        methods = [GOOGLE, EMAIL]

        return { data: methods }
      },
      'DELETE /v1/account/sign-in-methods/*': () => {
        methods = [EMAIL]

        return { data: methods }
      },
    })
    const user = userEvent.setup()

    nav.set('/settings/account')
    renderWithProviders(<SignInMethodsCard />)

    const list = await screen.findByTestId('sign-in-methods')

    expect(within(list).queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('change-email')).not.toBeInTheDocument()

    await user.click(await screen.findByRole('button', { name: /add email address/i }))
    const drawer = await screen.findByRole('dialog', { name: /add email address/i })

    await user.type(within(drawer).getByLabelText(/email address/i), 'zoe@example.com')
    await user.click(within(drawer).getByTestId('send-code'))
    await user.type(await within(drawer).findByLabelText(/^code/i), '222222')
    await waitFor(() => expect(within(list).getAllByRole('button', { name: 'Remove' })).toHaveLength(2))
    expect(api.find('POST', '/v1/account/sign-in-methods/email/verify')[0].body).toEqual({
      email: 'zoe@example.com',
      code: '222222',
    })
    // With an address, the card offers to change it, not to add another.
    expect(screen.queryByRole('button', { name: /add email address/i })).not.toBeInTheDocument()
    expect(screen.getByTestId('change-email')).toBeInTheDocument()

    await user.click(within(list).getAllByRole('button', { name: 'Remove' })[0])
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/account/sign-in-methods/google/sub-zoe')).toHaveLength(1))
  })

  it('changes the address with a code to the current one, then one to the new one', async () => {
    let methods = [EMAIL]
    const api = mockApi({
      'GET /v1/setup': { ...SETUP, email_sign_in: true },
      'GET /v1/account/sign-in-methods': () => ({ data: methods }),
      'POST /v1/account/sign-in-methods/email/change/current/start': SENT,
      'POST /v1/account/sign-in-methods/email/change/current/verify': { status: 204 },
      'POST /v1/account/sign-in-methods/email/change/new/start': SENT,
      'POST /v1/account/sign-in-methods/email/change/new/verify': () => {
        methods = [NEW]

        return { data: methods }
      },
    })
    const user = userEvent.setup()

    nav.set('/settings/account')
    renderWithProviders(<SignInMethodsCard />)

    await user.click(await screen.findByTestId('change-email'))
    const drawer = await screen.findByRole('dialog', { name: /change email address/i })

    expect(within(drawer).getByText('zoe@example.com')).toBeInTheDocument()
    await user.click(within(drawer).getByRole('button', { name: /send code/i }))
    await user.type(await within(drawer).findByLabelText(/^code/i), '111111')
    expect(api.find('POST', '/v1/account/sign-in-methods/email/change/current/verify')[0].body).toEqual({
      email: 'zoe@example.com',
      code: '111111',
    })

    await user.type(await within(drawer).findByLabelText(/new email address/i), 'zoe@new.example')
    await user.click(within(drawer).getByTestId('send-code'))
    expect(api.find('POST', '/v1/account/sign-in-methods/email/change/new/start')[0].body).toEqual({
      email: 'zoe@example.com',
      new_email: 'zoe@new.example',
    })
    await user.type(await within(drawer).findByLabelText(/^code/i), '222222')
    await waitFor(() => expect(screen.getByTestId('sign-in-methods')).toHaveTextContent('zoe@new.example'))
    expect(api.find('POST', '/v1/account/sign-in-methods/email/change/new/verify')[0].body).toEqual({
      email: 'zoe@example.com',
      new_email: 'zoe@new.example',
      code: '222222',
    })
  })

  it('adds a passkey with the browser and renames it', async () => {
    let methods: (typeof EMAIL)[] = [EMAIL]
    const created = { id: 'cred', rawId: 'cred', type: 'public-key', response: { clientDataJSON: 'x' } }

    stubPasskeys({ create: created })
    const api = mockApi({
      'GET /v1/setup': { ...SETUP, email_sign_in: true, passkey_sign_in: true },
      'GET /v1/account/sign-in-methods': () => ({ data: methods }),
      'POST /v1/account/sign-in-methods/passkey/start': { challenge_id: 'pkc_1', options: CREATE_OPTIONS },
      'POST /v1/account/sign-in-methods/passkey/finish': () => {
        methods = [EMAIL, PASSKEY]

        return { status: 201, body: { data: methods } }
      },
      'PATCH /v1/account/sign-in-methods/passkey/pk_1': () => {
        methods = [EMAIL, { ...PASSKEY, name: 'Work laptop' }]

        return { data: methods }
      },
    })
    const user = userEvent.setup()

    nav.set('/settings/account')
    renderWithProviders(<SignInMethodsCard />)

    await user.click(await screen.findByTestId('add-passkey'))
    const drawer = await screen.findByRole('dialog', { name: /add passkey/i })
    const name = within(drawer).getByLabelText(/^name/i)

    await user.clear(name)
    await user.type(name, 'Mac')
    await user.click(within(drawer).getByTestId('save-passkey'))
    await waitFor(() => expect(screen.getByTestId('sign-in-methods')).toHaveTextContent('Passkey Mac'))
    expect(api.find('POST', '/v1/account/sign-in-methods/passkey/finish')[0].body).toEqual({
      challenge_id: 'pkc_1',
      name: 'Mac',
      response: created,
    })

    await user.click(within(screen.getByTestId('sign-in-methods')).getByRole('button', { name: 'Rename' }))
    const rename = await screen.findByRole('dialog', { name: /rename passkey/i })

    await user.clear(within(rename).getByLabelText(/^name/i))
    await user.type(within(rename).getByLabelText(/^name/i), 'Work laptop')
    await user.click(within(rename).getByRole('button', { name: /save changes/i }))
    await waitFor(() => expect(screen.getByTestId('sign-in-methods')).toHaveTextContent('Work laptop'))
  })
})

describe('sign-in with a passkey', () => {
  it('opens the browser prompt and signs in', async () => {
    const asserted = { id: 'cred', rawId: 'cred', type: 'public-key', response: { clientDataJSON: 'x' } }

    stubPasskeys({ get: asserted })
    const api = mockApi({
      'GET /v1/setup': { ...SETUP, email_sign_in: true, passkey_sign_in: true },
      'POST /v1/auth/passkey/start': { challenge_id: 'pkc_2', options: { challenge: 'AAAA', rpId: 'localhost' } },
      'POST /v1/auth/passkey/finish': { status: 201, body: { csrf_token: 'c', next: '/posts' } },
    })
    const user = userEvent.setup()

    nav.set('/login', 'next=%2Fposts')
    renderWithProviders(<SignIn />)

    await user.click(await screen.findByTestId('passkey-sign-in'))
    await waitFor(() => expect(nav.replace).toHaveBeenCalledWith('/posts', undefined))
    expect(api.find('POST', '/v1/auth/passkey/finish')[0].body).toEqual({
      challenge_id: 'pkc_2',
      response: asserted,
      next: '/posts',
    })
  })
})

const CREATE_OPTIONS = {
  challenge: 'AAAA',
  rp: { id: 'localhost', name: 'Staqly' },
  user: { id: 'dXNy', name: 'zoe@example.com', displayName: 'Zoe' },
  pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
}

const cred = (j?: object) => ({ toJSON: () => j })

/** A browser with WebAuthn: create/get resolve to a credential whose toJSON() is `create`/`get`. */
function stubPasskeys(json: { create?: object; get?: object }) {
  Object.defineProperty(window, 'PublicKeyCredential', {
    configurable: true,
    value: Object.assign(function PublicKeyCredential() {}, {
      isConditionalMediationAvailable: () => Promise.resolve(false),
    }),
  })
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    value: {
      create: vi.fn<() => Promise<object>>(() => Promise.resolve(cred(json.create))),
      get: vi.fn<() => Promise<object>>(() => Promise.resolve(cred(json.get))),
    },
  })
}
