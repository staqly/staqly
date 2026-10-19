'use client'

import { useMutation, useQuery } from '@tanstack/react-query'
import { Mail, MailCheck } from 'lucide-react'
import Link from 'next/link'
import { useSearchParams } from 'next/navigation'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type EmailCodeSent } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { useSetupStatus } from '@/lib/queries'
import { ROLE_LABELS } from '@/lib/roles'

import { AuthLayout } from './auth-layout'
import { codeErrorMessage, EmailCodeForm, useFinishSignIn } from './email-sign-in'
import { googleStartURL } from './sign-in'

const CLOSED: Record<string, string> = {
  accepted: 'This invitation was already accepted. Sign in to open the workspace.',
  revoked: 'This invitation was revoked. Ask for a new one.',
  expired: 'This invitation expired. Ask for a new one.',
}

/** The invited address gets a code; entering it signs in (or signs up) and accepts the invitation. */
function AcceptWithCode({
  email,
  token,
  onCode,
}: {
  email: string
  token: string
  onCode: (s: EmailCodeSent) => void
}) {
  const [error, setError] = useState('')
  const send = useMutation({
    mutationFn: () => api.startEmailSignIn(email),
    onSuccess: onCode,
    onError: (e) => setError(codeErrorMessage(e)),
  })

  return (
    <div className="grid gap-2">
      <Button
        size="lg"
        variant="outline"
        className="h-11"
        disabled={send.isPending || !token}
        onClick={() => send.mutate()}
        data-testid="accept-with-code"
      >
        <Mail /> {send.isPending ? 'Sending code...' : `Email a code to ${email}`}
      </Button>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}

/** The page an invitation link opens: what it is for, then sign in as the invited address to accept it. */
export function AcceptInvitation() {
  const token = useSearchParams().get('token') ?? ''
  const status = useSetupStatus()
  const finish = useFinishSignIn()
  const [codeSent, setCodeSent] = useState<EmailCodeSent | null>(null)
  const preview = useQuery({
    queryKey: ['invitation', token],
    queryFn: () => api.lookupInvitation(token),
    enabled: !!token,
    retry: false,
  })

  if (codeSent && preview.data) {
    const email = preview.data.email

    return (
      <AuthLayout>
        <EmailCodeForm
          email={email}
          sent={codeSent}
          submitLabel="Accept invitation"
          verify={(code) => api.verifyEmailSignIn({ email, code, invite: token })}
          resend={() => api.startEmailSignIn(email)}
          onDone={(r) => finish(r as { csrf_token: string; next: string })}
          onBack={() => setCodeSent(null)}
          backLabel="Back"
        />
      </AuthLayout>
    )
  }

  return (
    <AuthLayout>
      <div className="grid gap-6">
        <div>
          <div className="mb-4 flex size-10 items-center justify-center rounded-xl border bg-card shadow-xs">
            <MailCheck className="size-5 text-brand" />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">
            {preview.data ? `Join ${preview.data.workspace}` : 'Your invitation'}
          </h1>
        </div>
        {preview.isPending && token ? (
          <Skeleton className="h-20" />
        ) : !preview.data ? (
          <>
            <p className="text-sm text-muted-foreground">This invitation link is not valid. Ask for a new one.</p>
            <Button asChild variant="outline" className="h-11">
              <Link href="/login">Go to sign in</Link>
            </Button>
          </>
        ) : preview.data.status !== 'open' ? (
          <>
            <p className="text-sm text-muted-foreground">{CLOSED[preview.data.status]}</p>
            <Button asChild variant="outline" className="h-11">
              <Link href="/login">Go to sign in</Link>
            </Button>
          </>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              {preview.data.invited_by} invited <strong className="text-foreground">{preview.data.email}</strong> to
              join as {ROLE_LABELS[preview.data.role].toLowerCase()}. Sign in as that address
              {status.data?.google_sign_in && status.data?.email_sign_in
                ? ': with its Google account or a code sent to it'
                : status.data?.google_sign_in
                  ? ' with its Google account'
                  : ' with a code sent to it'}
              . The link works until {formatDateTime(preview.data.expires_at)}.
            </p>
            {status.data?.google_sign_in && (
              <Button asChild size="lg" className="h-11">
                <a href={googleStartURL({ invite: token })} data-testid="accept-invitation">
                  Accept with Google
                </a>
              </Button>
            )}
            {status.data?.email_sign_in && (
              <AcceptWithCode email={preview.data.email} token={token} onCode={setCodeSent} />
            )}
          </>
        )}
      </div>
    </AuthLayout>
  )
}
