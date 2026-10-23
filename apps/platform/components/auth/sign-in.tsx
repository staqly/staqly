'use client'

import { Fingerprint } from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useEffectEvent, useRef, useState, useSyncExternalStore } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError, errorMessage, type EmailCodeSent } from '@/lib/api'
import { getPasskey, passkeyAutofillAvailable, passkeyCancelled, passkeysSupported } from '@/lib/passkeys'
import { useSetupStatus } from '@/lib/queries'
import { safeNextPath } from '@/lib/safe'

import { AuthLayout } from './auth-layout'
import { EmailAddressForm, EmailCodeForm, useFinishSignIn } from './email-sign-in'
import { GoogleMark } from './google-mark'
import { LocalSignIn } from './local-sign-in'

/**
 * The cloud sign-in and sign-up pages: Google, an email code or a passkey (a new address gets an account and a
 * first workspace). API keys are for agents only and never open the dashboard here. A local install signs in
 * with its access token instead (no accounts, no sign-up).
 */
export function SignIn({ signup = false }: { signup?: boolean }) {
  const router = useRouter()
  const status = useSetupStatus()
  const d = status.data

  if (d?.mode === 'local') {
    return <LocalSignIn onDone={() => router.replace('/schedule')} />
  }

  const accounts = !!d?.google_sign_in || !!d?.email_sign_in || !!d?.passkey_sign_in
  // Older servers did not report signup_open; treat them as open. A setup token opens a closed server.
  const canSignUp = d?.signup_open !== false || !!d?.setup_token_required

  return (
    <AuthLayout>
      {status.error ? (
        <ErrorState title="Cannot reach the Staqly API" error={status.error} onRetry={() => status.refetch()} />
      ) : !d ? (
        <Skeleton className="h-80 w-full rounded-2xl" />
      ) : !accounts ? (
        <Notice title="Sign-in is not set up on this server">
          The operator turns on sign-in with Google or with email codes in the server&apos;s settings.
        </Notice>
      ) : signup && !canSignUp ? (
        <Notice title="Sign-ups are by invitation here">
          Ask a workspace admin to invite your address, then open the link in the invitation.{' '}
          <Link href="/login" className="font-medium text-foreground underline-offset-4 underline hover:no-underline">
            Sign in
          </Link>{' '}
          if you already have an account.
        </Notice>
      ) : (
        <AccountSignIn
          signup={signup}
          canSignUp={canSignUp}
          google={!!d.google_sign_in}
          email={!!d.email_sign_in}
          passkey={!!d.passkey_sign_in}
          setupTokenRequired={!!d.setup_token_required}
        />
      )}
    </AuthLayout>
  )
}

function Notice({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2" role="status">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      <p className="text-sm text-muted-foreground">{children}</p>
    </div>
  )
}

/** Plain-words reasons for a sign-in that came back with ?error=<code>. */
const SIGN_IN_ERRORS: Record<string, string> = {
  signup_closed:
    'This Staqly accepts new people by invitation. Ask a workspace admin to invite your address, or enter a setup token below.',
  invitation_email_mismatch: 'That invitation is for another address. Sign in with the address it was sent to.',
  invitation_invalid: 'That invitation link no longer works. Ask for a new one.',
  invitation_expired: 'That invitation expired. Ask for a new one.',
  email_unverified: 'This Google account has no verified email address.',
  google_cancelled: 'The sign-in at Google was cancelled.',
  login_expired: 'The sign-in took too long or started in another browser. Try again.',
  login_state_mismatch: 'The sign-in could not be matched to this browser. Try again.',
  google_unavailable: 'Google could not be reached. Try again in a minute.',
  rate_limited: 'Too many attempts. Wait a minute and try again.',
}

const emptySubscribe = () => () => {}

/** "Use your Google account, a passkey or a code we email you." */
function signInWays(google: boolean, passkey: boolean, email: boolean) {
  const ways = [google && 'your Google account', passkey && 'a passkey', email && 'a code we email you'].filter(
    (w): w is string => !!w,
  )

  return `Use ${ways.length > 1 ? `${ways.slice(0, -1).join(', ')} or ${ways.at(-1)}` : ways[0]}.`
}

/** Plain words for a passkey sign-in that did not work. */
function passkeyErrorMessage(e: unknown) {
  if (e instanceof ApiError) {
    switch (e.code) {
      case 'passkey_invalid':
        return 'This passkey could not be checked. It may have been removed from your account. Try again or sign in another way.'
      case 'passkey_cloned':
        return 'This passkey looks copied to another device. Sign in another way and remove it in Settings, Account.'
      case 'passkey_expired':
        return 'The passkey prompt took too long. Try again.'
      case 'rate_limited':
        return 'Too many attempts. Wait a few minutes and try again.'
    }
  }

  return errorMessage(e)
}

/**
 * Signing in with a passkey: a button that opens the browser's prompt and, where the browser offers it, saved
 * passkeys in the address field's autofill (conditional mediation) while the page is open.
 */
function usePasskeySignIn(enabled: boolean, autofill: boolean, next: string) {
  const finish = useFinishSignIn()
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const pendingAutofill = useRef<AbortController | null>(null)

  const run = async (mediation?: CredentialMediationRequirement, signal?: AbortSignal) => {
    const challenge = await api.startPasskeySignIn()
    const response = await getPasskey(challenge.options, { mediation, signal })

    finish(await api.finishPasskeySignIn({ challenge_id: challenge.challenge_id, response, next }))
  }
  // The autofill request lives while the page is open: the effect runs once, with the latest run.
  const runAutofill = useEffectEvent((signal: AbortSignal) => run('conditional', signal))

  useEffect(() => {
    if (!enabled || !autofill) {
      return
    }

    const ctrl = new AbortController()

    pendingAutofill.current = ctrl
    void passkeyAutofillAvailable().then((ok) => {
      if (ok && !ctrl.signal.aborted) {
        runAutofill(ctrl.signal).catch((e: unknown) => {
          if (!passkeyCancelled(e)) {
            setError(passkeyErrorMessage(e))
          }
        })
      }
    })

    return () => ctrl.abort()
  }, [enabled, autofill])

  const prompt = async () => {
    // One WebAuthn request at a time: the button's prompt replaces the autofill's.
    pendingAutofill.current?.abort()
    setError('')
    setPending(true)

    await run().catch((e: unknown) => {
      if (!passkeyCancelled(e)) {
        setError(passkeyErrorMessage(e))
      }
    })
    setPending(false)
  }

  return { error, pending, prompt }
}

/** The sign-in link: a full page load to the API, which sends the browser to Google and back. */
export function googleStartURL(opts: { next?: string | null; invite?: string; setupToken?: string }) {
  const q = new URLSearchParams()

  if (opts.next) {
    q.set('next', opts.next)
  }

  if (opts.invite) {
    q.set('invite', opts.invite)
  }

  if (opts.setupToken) {
    q.set('setup_token', opts.setupToken)
  }

  return `/api/v1/auth/google/start?${q.toString()}`
}

function AccountSignIn({
  signup,
  canSignUp,
  google,
  email,
  passkey: passkeyOn,
  setupTokenRequired,
}: {
  signup: boolean
  canSignUp: boolean
  google: boolean
  email: boolean
  passkey: boolean
  setupTokenRequired: boolean
}) {
  const params = useSearchParams()
  const error = params.get('error')
  const [token, setToken] = useState('')
  const [codeFor, setCodeFor] = useState<{ email: string; sent: EmailCodeSent } | null>(null)
  const finish = useFinishSignIn()
  // A plan picked on the pricing page: after the first sign-in the plan picker opens with it.
  const plan = params.get('plan')
  const planNext = plan
    ? `/plans?${new URLSearchParams({ plan, ...(params.get('interval') ? { interval: params.get('interval')! } : {}) })}`
    : null
  // A new account starts in the full-screen welcome (it sends returning people on to the dashboard).
  const next = safeNextPath(
    params.get('next') ?? planNext ?? (signup ? '/welcome' : null),
    typeof window === 'undefined' ? 'http://x' : window.location.origin,
  )
  // The same query (next, plan, invitation errors) on the way between sign-in and sign-up.
  const query = params.toString() ? `?${params.toString()}` : ''
  // The browser's WebAuthn support is only known on the client. A new account has no passkey yet.
  const supported = useSyncExternalStore(emptySubscribe, passkeysSupported, () => false)
  const passkey = passkeyOn && supported && !signup
  const passkeySignIn = usePasskeySignIn(passkey, email && !codeFor, next)

  if (codeFor) {
    return (
      <EmailCodeForm
        email={codeFor.email}
        sent={codeFor.sent}
        verify={(code) =>
          api.verifyEmailSignIn({ email: codeFor.email, code, next, setup_token: token.trim() || undefined })
        }
        resend={() => api.startEmailSignIn(codeFor.email)}
        onDone={(r) => finish(r as { csrf_token: string; next: string })}
        onBack={() => setCodeFor(null)}
      />
    )
  }

  return (
    <div className="grid gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          {signup ? 'Create your account' : 'Sign in to Staqly'}
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {signup
            ? `Starter is free for 7 days, no card needed. ${signInWays(google, false, email)}`
            : signInWays(google, passkey, email)}
        </p>
      </div>
      {error && (
        <p
          role="alert"
          className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
        >
          {SIGN_IN_ERRORS[error] ?? 'The sign-in did not work. Try again.'}
        </p>
      )}
      {google && (
        <Button asChild size="lg" className="h-11">
          <a href={googleStartURL({ next, setupToken: token.trim() })} data-testid="google-sign-in">
            <GoogleMark /> Continue with Google
          </a>
        </Button>
      )}
      {passkey && (
        <Button
          size="lg"
          variant={google ? 'outline' : 'default'}
          className="h-11"
          disabled={passkeySignIn.pending}
          onClick={() => void passkeySignIn.prompt()}
          data-testid="passkey-sign-in"
        >
          <Fingerprint /> {passkeySignIn.pending ? 'Waiting for your passkey...' : 'Sign in with a passkey'}
        </Button>
      )}
      {passkeySignIn.error && (
        <p
          role="alert"
          className="rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
        >
          {passkeySignIn.error}
        </p>
      )}
      {(google || passkey) && email && (
        <div className="flex items-center gap-3 text-xs text-muted-foreground" aria-hidden>
          <span className="h-px flex-1 bg-border" /> or <span className="h-px flex-1 bg-border" />
        </div>
      )}
      {email && (
        <EmailAddressForm
          send={api.startEmailSignIn}
          autoComplete={passkey ? 'email webauthn' : 'email'}
          onSent={(e, sent) => setCodeFor({ email: e, sent })}
        />
      )}
      {setupTokenRequired && (
        <Field>
          <FieldLabel htmlFor="setup-token">
            {signup ? 'Setup token' : 'Setup token (only for a new account)'}
          </FieldLabel>
          <Input
            id="setup-token"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
          <FieldDescription>Invited people and returning members leave this empty.</FieldDescription>
        </Field>
      )}
      <p className="text-center text-sm text-muted-foreground">
        {signup ? (
          <>
            Already have an account?{' '}
            <Link
              href={`/login${query}`}
              className="inline-flex min-h-10 items-center font-medium text-foreground underline-offset-4 underline hover:no-underline"
            >
              Sign in
            </Link>
          </>
        ) : (
          canSignUp && (
            <>
              New to Staqly?{' '}
              <Link
                href={`/signup${query}`}
                className="inline-flex min-h-10 items-center font-medium text-foreground underline-offset-4 underline hover:no-underline"
              >
                Create an account
              </Link>
            </>
          )
        )}
      </p>
    </div>
  )
}
