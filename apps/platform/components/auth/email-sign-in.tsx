'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Mail } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useEffect, useRef, useState } from 'react'

import { Callout } from '@/components/common/callout'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ApiError, errorMessage, setCsrfToken, type EmailCodeSent } from '@/lib/api'
import { safeNextPath } from '@/lib/safe'
import { rememberApiKey } from '@/lib/session-key'
import { cn } from '@/lib/utils'

/** Plain words for the errors of sending and checking a code. */
export function codeErrorMessage(err: unknown): string {
  if (!(err instanceof ApiError)) {
    return errorMessage(err)
  }

  switch (err.code) {
    case 'code_invalid': {
      const left = Number(err.details?.attempts_left)

      return Number.isFinite(left)
        ? `That code is not right. ${left} ${left === 1 ? 'try' : 'tries'} left.`
        : 'That code is not right.'
    }

    case 'code_expired':
      return 'This code no longer works: it expired, was already used or had too many wrong tries. Send a new one.'
    case 'code_cooldown':
      return 'A code was just sent. Wait a few seconds before asking for another.'
    case 'rate_limited':
      return 'Too many codes for now. Wait a while and try again.'
    case 'mail_failed':
      return 'The email could not be sent. Try again in a minute.'
    case 'signup_closed':
      return 'This Staqly accepts new people by invitation. Ask a workspace admin to invite this address, or enter a setup token.'
    case 'invitation_email_mismatch':
      return 'This invitation is for another address. Use the address it was sent to.'
    case 'invitation_invalid':
      return 'That invitation link no longer works. Ask for a new one.'
    case 'invitation_expired':
      return 'That invitation expired. Ask for a new one.'
    case 'identity_taken':
      return 'This address already signs in to another Staqly person. Sign in with it there and remove it first.'
    case 'email_exists':
      return 'You already sign in with an email address. Change that address instead.'
    case 'change_not_verified':
      return 'Confirm your current address first. The confirmation lasts 10 minutes, so start again if it ran out.'
    case 'validation_failed':
      return err.fields?.new_email
        ? 'That is the address you sign in with now. Enter a different one.'
        : err.fields?.email
          ? 'Enter an email address such as ana@example.com.'
          : 'Enter the 6-digit code from the email.'
    default:
      return errorMessage(err)
  }
}

/** Seconds left until `at` (a timestamp in ms), ticking every second. */
function useCountdown(at: number) {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (at <= Date.now()) {
      return
    }

    // Stops at 0, so the form doesn't re-render every second while the code step stays open.
    const id = window.setInterval(() => {
      const t = Date.now()

      setNow(t)

      if (t >= at) {
        window.clearInterval(id)
      }
    }, 1000)

    return () => window.clearInterval(id)
  }, [at])

  return Math.max(0, Math.ceil((at - now) / 1000))
}

/** The address step: "Continue with email". */
export function EmailAddressForm({
  onSent,
  send,
  label = 'Email address',
  submitLabel = 'Continue with email',
  compact = false,
  autoComplete = 'email',
}: {
  onSent: (email: string, sent: EmailCodeSent) => void
  send: (email: string) => Promise<EmailCodeSent>
  label?: string
  submitLabel?: string
  /** In a drawer: the dashboard's 32 px controls instead of the sign-in page's large ones. */
  compact?: boolean
  /** "email webauthn" on the sign-in page: the field also offers saved passkeys. */
  autoComplete?: string
}) {
  const [email, setEmail] = useState('')
  const [error, setError] = useState('')
  const start = useMutation({
    mutationFn: (v: string) => send(v),
    onSuccess: (sent, v) => onSent(v, sent),
    onError: (e) => setError(codeErrorMessage(e)),
  })

  return (
    <form
      noValidate
      className="grid gap-3"
      onSubmit={(e) => {
        e.preventDefault()

        const v = email.trim().toLowerCase()

        if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v)) {
          setError('Enter an email address such as ana@example.com.')

          return
        }

        if (!start.isPending) {
          setError('')
          start.mutate(v)
        }
      }}
    >
      <Field data-invalid={!!error}>
        <FieldLabel htmlFor="email-address" required>
          {label}
        </FieldLabel>
        <Input
          id="email-address"
          type="email"
          inputMode="email"
          autoComplete={autoComplete}
          spellCheck={false}
          placeholder="ana@example.com"
          className={compact ? undefined : 'h-11 text-base sm:text-sm'}
          value={email}
          aria-invalid={!!error}
          aria-required
          onChange={(e) => setEmail(e.target.value)}
        />
        <FieldError>{error}</FieldError>
      </Field>
      <Button
        type="submit"
        size={compact ? 'default' : 'lg'}
        variant="outline"
        className={compact ? 'justify-self-start pointer-coarse:h-11' : 'h-11'}
        disabled={start.isPending}
        data-testid="send-code"
      >
        <Mail /> {start.isPending ? 'Sending code...' : submitLabel}
      </Button>
    </form>
  )
}

/**
 * The code step: one input that takes a pasted code or the one the phone suggests (one-time-code), submits by
 * itself at 6 digits, and offers a new code once the cooldown is over.
 */
export function EmailCodeForm({
  email,
  sent,
  verify,
  resend,
  onDone,
  onBack,
  backLabel = 'Use another address',
  submitLabel = 'Sign in',
  compact = false,
}: {
  email: string
  sent: EmailCodeSent
  verify: (code: string) => Promise<unknown>
  resend: () => Promise<EmailCodeSent>
  onDone: (result: unknown) => void
  onBack: () => void
  /** The invitation has one address: its back button just goes back. */
  backLabel?: string
  submitLabel?: string
  /** In a drawer: a small heading and the dashboard's 32 px controls. */
  compact?: boolean
}) {
  const [code, setCode] = useState('')
  const [focused, setFocused] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [resendAt, setResendAt] = useState(() => Date.now() + sent.resend_in * 1000)
  const input = useRef<HTMLInputElement>(null)

  // The code field takes focus when this step opens (the address step's button had it).
  useEffect(() => input.current?.focus(), [])
  const wait = useCountdown(resendAt)
  const check = useMutation({
    mutationFn: (c: string) => verify(c),
    onSuccess: (r) => onDone(r),
    onError: (e) => {
      setError(codeErrorMessage(e))
      setCode('')
    },
  })
  const again = useMutation({
    mutationFn: resend,
    onSuccess: (s) => {
      setResendAt(Date.now() + s.resend_in * 1000)
      setError('')
      setNotice('We sent a new code. The previous one no longer works.')
    },
    onError: (e) => {
      const retry = e instanceof ApiError ? Number(e.details?.resend_in) : NaN

      if (Number.isFinite(retry)) {
        setResendAt(Date.now() + retry * 1000)
      }

      setError(codeErrorMessage(e))
    },
  })
  const submit = (c: string) => {
    if (c.length !== 6) {
      setError('Enter the 6-digit code from the email.')

      return
    }

    if (!check.isPending) {
      setError('')
      setNotice('')
      check.mutate(c)
    }
  }

  return (
    <div className="grid gap-5">
      <div>
        {compact ? (
          <h3 className="text-sm font-medium">Check your email</h3>
        ) : (
          <>
            <div className="mb-4 flex size-10 items-center justify-center rounded-xl border bg-card shadow-xs">
              <Mail className="size-5 text-brand" />
            </div>
            <h1 className="text-2xl font-semibold tracking-tight">Check your email</h1>
          </>
        )}
        <p className="mt-1 text-sm text-muted-foreground">
          We sent a 6-digit code to <strong className="break-all text-foreground">{email}</strong>. It works once, for{' '}
          {Math.round(sent.expires_in / 60)} minutes.
        </p>
      </div>
      <form
        noValidate
        className="grid gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          submit(code)
        }}
      >
        <Field data-invalid={!!error}>
          <FieldLabel htmlFor="sign-in-code" required>
            Code
          </FieldLabel>
          {/* Six boxes, one per digit, under one real input (typing, paste and the phone's code autofill all work). */}
          <div className="relative">
            <div aria-hidden className="grid grid-cols-6 gap-2">
              {Array.from({ length: 6 }, (_, i) => (
                <div
                  key={i}
                  className={cn(
                    'flex items-center justify-center rounded-lg border bg-background font-mono font-semibold tabular-nums shadow-xs transition-colors',
                    compact ? 'h-10 text-lg' : 'h-14 text-2xl',
                    error && 'border-destructive',
                    focused && i === Math.min(code.length, 5) && 'border-brand ring-3 ring-brand/25',
                  )}
                >
                  {code[i] ?? ''}
                </div>
              ))}
            </div>
            <Input
              id="sign-in-code"
              ref={input}
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]*"
              className="absolute inset-0 h-full rounded-lg border-0 bg-transparent text-transparent caret-transparent shadow-none outline-none selection:bg-transparent focus-visible:border-0 focus-visible:ring-0 aria-invalid:ring-0 dark:bg-transparent"
              onFocus={() => setFocused(true)}
              onBlur={() => setFocused(false)}
              value={code}
              aria-invalid={!!error}
              aria-required
              onChange={(e) => {
                const digits = e.target.value.replace(/\D/g, '').slice(0, 6)

                setCode(digits)

                if (digits.length === 6) {
                  submit(digits)
                }
              }}
            />
          </div>
          {notice && !error ? (
            <Callout tone="success" size="xs" role="status">
              {notice}
            </Callout>
          ) : null}
          <FieldError>{error}</FieldError>
        </Field>
        <Button
          type="submit"
          size={compact ? 'default' : 'lg'}
          className={compact ? 'justify-self-start pointer-coarse:h-11' : 'h-11'}
          disabled={check.isPending}
          data-testid="verify-code"
        >
          {check.isPending ? 'Checking...' : submitLabel}
        </Button>
      </form>
      <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground">
        <Button
          type="button"
          variant="ghost"
          className={compact ? 'px-2 pointer-coarse:h-10' : 'h-10 px-2'}
          onClick={onBack}
        >
          <ArrowLeft /> {backLabel}
        </Button>
        <Button
          type="button"
          variant="ghost"
          className={compact ? 'px-2 pointer-coarse:h-10' : 'h-10 px-2'}
          disabled={wait > 0 || again.isPending}
          onClick={() => again.mutate()}
          data-testid="resend-code"
        >
          {wait > 0 ? `Send a new code in ${wait} s` : again.isPending ? 'Sending...' : 'Send a new code'}
        </Button>
      </div>
    </div>
  )
}

/** After a successful sign-in: drop everything cached for a previous session and open the landing page. */
export function useFinishSignIn() {
  const router = useRouter()
  const qc = useQueryClient()

  return (res: { csrf_token: string; next: string }) => {
    setCsrfToken(res.csrf_token)
    rememberApiKey(null)
    qc.clear()
    router.replace(safeNextPath(res.next, window.location.origin))
  }
}
