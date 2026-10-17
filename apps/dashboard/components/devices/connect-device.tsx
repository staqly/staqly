'use client'

import { LogoMark } from '@staqly/brand/logo'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Check, Clock, QrCode, RefreshCw } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { useEffect, useRef, useState } from 'react'

import { PublicShell } from '@/components/auth/public-shell'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { api, ApiError, errorMessage, setCsrfToken } from '@/lib/api'
import { safeDecodeURIComponent } from '@/lib/safe'
import { rememberApiKey } from '@/lib/session-key'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

type State =
  | { kind: 'working' }
  | { kind: 'done' }
  | { kind: 'expired'; reason: 'expired' | 'used' | 'invalid' | 'missing' }
  | { kind: 'error'; message: string }

/** Reads the one-time token from the URL fragment and removes it right away. */
function takeToken(): string {
  const raw = window.location.hash.replace(/^#/, '')

  if (raw) {
    window.history.replaceState(null, '', window.location.pathname + window.location.search)
  }

  // `#token=...` (already decoded by URLSearchParams) or the bare token.
  const token = new URLSearchParams(raw).get('token') ?? safeDecodeURIComponent(raw)

  return token.trim()
}

function reasonOf(err: unknown): 'expired' | 'used' | 'invalid' | null {
  if (!(err instanceof ApiError)) {
    return null
  }

  if (err.code === 'link_used') {
    return 'used'
  }

  if (err.code === 'link_expired' || err.status === 410) {
    return 'expired'
  }

  if (err.code === 'invalid_link' || err.status === 401 || err.status === 404) {
    return 'invalid'
  }

  return null
}

const COPY: Record<'expired' | 'used' | 'invalid' | 'missing', { title: string; text: string }> = {
  expired: {
    title: 'This link expired',
    text: 'Codes are valid for 5 minutes. Generate a new QR on your computer and scan it again.',
  },
  used: {
    title: 'This link was already used',
    text: 'Each code works once. Generate a new QR on your computer and scan it again.',
  },
  invalid: {
    title: "This link isn't valid",
    text: 'It may have expired or been mistyped. Generate a new QR on your computer and scan it again.',
  },
  missing: {
    title: 'Scan a code to connect',
    text: "Open Connect a device on a browser that's already signed in, then scan the QR code with this phone.",
  },
}

export function ConnectDevice() {
  const router = useRouter()
  const qc = useQueryClient()
  const [state, setState] = useState<State>({ kind: 'working' })
  const started = useRef(false)

  useEffect(() => {
    if (started.current) {
      return
    }

    started.current = true
    const token = takeToken()

    if (!token) {
      // oxlint-disable-next-line react/set-state-in-effect -- one-shot init from the URL fragment
      setState({ kind: 'expired', reason: 'missing' })

      return
    }

    api
      .redeemDeviceLink(token)
      .then((res) => {
        setCsrfToken(res.csrf_token ?? null)
        rememberApiKey(null)
        qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'plans' })
        setState({ kind: 'done' })
        router.replace('/inbox')
      })
      .catch((err) => {
        const reason = reasonOf(err)

        setState(reason ? { kind: 'expired', reason } : { kind: 'error', message: errorMessage(err) })
      })
  }, [qc, router])

  return (
    <PublicShell className="flex max-w-md flex-col justify-center pb-16">
      <div
        className="grid gap-5 rounded-3xl border bg-card/90 p-6 text-center shadow-xl shadow-black/[0.05] backdrop-blur sm:p-8"
        data-testid="connect-device-page"
        data-state={state.kind}
      >
        {state.kind === 'working' || state.kind === 'done' ? (
          <>
            <div className="relative mx-auto">
              <LogoMark className={cn('size-16', state.kind === 'working' && 'animate-pulse')} />
              {state.kind === 'done' && (
                <span
                  className={cn(
                    'absolute -right-1.5 -bottom-1.5 flex size-7 items-center justify-center rounded-full text-white ring-4 ring-card',
                    TONES.success.dot,
                  )}
                >
                  <Check className="size-4" strokeWidth={3} />
                </span>
              )}
            </div>
            <div>
              <h1 className="text-xl font-semibold tracking-tight">
                {state.kind === 'done' ? 'Connected' : 'Connecting this device...'}
              </h1>
              <p className="mt-2 text-sm text-muted-foreground">
                {state.kind === 'done' ? 'Opening your inbox.' : 'Hold on while we sign this device in.'}
              </p>
            </div>
            <Spinner className="mx-auto size-5 text-muted-foreground" />
          </>
        ) : state.kind === 'expired' ? (
          <>
            <span className={cn('mx-auto flex size-16 items-center justify-center rounded-2xl', TONES.warning.chip)}>
              {state.reason === 'missing' ? <QrCode className="size-8" /> : <Clock className="size-8" />}
            </span>
            <div>
              <h1 className="text-xl font-semibold tracking-tight">{COPY[state.reason].title}</h1>
              <p className="mt-2 text-sm text-muted-foreground">{COPY[state.reason].text}</p>
            </div>
            <ol className="grid gap-2.5 rounded-2xl bg-muted/60 p-4 text-left text-sm">
              <li className="flex gap-3">
                <Step n={1} />
                <span className="pt-0.5">
                  On a browser that&apos;s already connected, open{' '}
                  <strong className="font-medium">Connect a device</strong> from the workspace menu.
                </span>
              </li>
              <li className="flex gap-3">
                <Step n={2} /> <span className="pt-0.5">Scan the new QR code with this phone.</span>
              </li>
              <li className="flex gap-3">
                <Step n={3} />
                <span className="pt-0.5">
                  On the computer running staqly you can also run{' '}
                  <code className="rounded bg-background px-1 py-0.5 font-mono text-[0.85em]">
                    staqly serve connect-link
                  </code>{' '}
                  (with Docker:{' '}
                  <code className="rounded bg-background px-1 py-0.5 font-mono text-[0.85em]">
                    docker exec staqly server connect-link
                  </code>
                  ).
                </span>
              </li>
            </ol>
          </>
        ) : (
          <>
            <span className={cn('mx-auto flex size-16 items-center justify-center rounded-2xl', TONES.danger.chip)}>
              <RefreshCw className="size-8" />
            </span>
            <div>
              <h1 className="text-xl font-semibold tracking-tight">Couldn&apos;t connect</h1>
              <p className="mt-2 text-sm text-muted-foreground">{state.message}</p>
            </div>
            <Button asChild size="lg" className="h-11">
              <Link href="/inbox">
                Try opening the dashboard <ArrowRight />
              </Link>
            </Button>
          </>
        )}
      </div>
    </PublicShell>
  )
}

function Step({ n }: { n: number }) {
  return (
    <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-background text-xs font-semibold tabular-nums shadow-xs">
      {n}
    </span>
  )
}
