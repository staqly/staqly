'use client'

import { LogoMark } from '@staqly/brand/logo'
import { AlertTriangle, CloudOff, RotateCw, WifiOff } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useEffect, useRef, useState, type ReactNode } from 'react'

import { accessTokenErrorMessage, useAccessTokenSignIn } from '@/components/auth/access-token-sign-in'
import { Button } from '@/components/ui/button'
import { ApiError, errorMessage } from '@/lib/api'
import { clearLinkToken, peekLinkToken } from '@/lib/local-token'
import { useSession, useSetupStatus } from '@/lib/queries'

// Only a browser that is not signed in needs the form: out of every page's first load.
const LocalSignIn = dynamic(() => import('@/components/auth/local-sign-in').then((m) => m.LocalSignIn))

/** The wait before the dashboard: invisible for a quick load (no flash), shown when it is slow (appear-when-slow). */
function Splash({ label = 'Loading workspace...' }: { label?: string }) {
  return (
    <div className="appear-when-slow flex min-h-svh items-center justify-center" role="status">
      <div className="flex flex-col items-center gap-3 text-sm text-muted-foreground">
        <LogoMark className="size-9 animate-pulse" />
        {label}
      </div>
    </div>
  )
}

/** Why the dashboard can't open, full screen and without a card: offline, the server not answering, or an error. */
function Unavailable({
  icon,
  title,
  text,
  onRetry,
}: {
  icon: ReactNode
  title: string
  text: ReactNode
  onRetry?: () => void
}) {
  return (
    <div role="alert" className="flex min-h-svh flex-col items-center justify-center gap-5 p-6 text-center">
      <span className="flex size-16 items-center justify-center rounded-2xl bg-muted text-muted-foreground [&_svg]:size-8">
        {icon}
      </span>
      <div className="grid max-w-md gap-2">
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        <p className="text-sm text-balance text-muted-foreground">{text}</p>
      </div>
      {onRetry && (
        <Button variant="outline" onClick={onRetry}>
          <RotateCw /> Try again
        </Button>
      )}
    </div>
  )
}

/**
 * Blocks the dashboard until the session is confirmed. In the cloud a 401 is turned into a redirect to /login by
 * the global handler in <Providers>; a local install shows its sign-in with the access token right here. The
 * link a local install prints (#token=..., taken out of the URL by <Providers>) signs in first.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const [linkToken] = useState(peekLinkToken)
  const [linkError, setLinkError] = useState('')
  const [linkDone, setLinkDone] = useState(!linkToken)
  const { mutate: signIn } = useAccessTokenSignIn()
  const started = useRef(false)

  useEffect(() => {
    if (!linkToken || started.current) {
      return
    }

    started.current = true
    clearLinkToken()
    signIn(linkToken, {
      onError: (err) => setLinkError(accessTokenErrorMessage(err)),
      onSettled: () => setLinkDone(true),
    })
  }, [linkToken, signIn])

  if (!linkDone) {
    return <Splash label="Signing in..." />
  }

  return <SessionGate linkError={linkError}>{children}</SessionGate>
}

function SessionGate({ children, linkError }: { children: ReactNode; linkError: string }) {
  const session = useSession()
  const setup = useSetupStatus()

  // Offline the session request waits (paused) and runs again once the browser is back online.
  if (session.fetchStatus === 'paused') {
    return (
      <Unavailable
        icon={<WifiOff />}
        title="You're offline"
        text="Check your internet connection. staqly opens again by itself once you're back online."
      />
    )
  }

  if (session.isPending) {
    return <Splash />
  }

  if (session.error) {
    const unauthorized = session.error instanceof ApiError && session.error.status === 401
    // status 0: the API did not answer at all (stopped, restarting, network); 502-504 and Cloudflare's 530: a proxy
    // in front of it could not reach it.
    const unreachable = session.error instanceof ApiError && [0, 502, 503, 504, 530].includes(session.error.status)

    if (unauthorized && setup.data?.mode === 'local') {
      return <LocalSignIn initialError={linkError} />
    }

    // In cloud mode a 401 becomes a redirect to /login (see <Providers>); the mode may still be loading.
    if (unauthorized) {
      return <Splash />
    }

    const retry = () => void session.refetch()

    if (unreachable) {
      return (
        <Unavailable
          icon={<CloudOff />}
          title="Can't reach staqly"
          text="The server isn't answering. It may be restarting: try again in a moment."
          onRetry={retry}
        />
      )
    }

    const serverSide = session.error instanceof ApiError && session.error.status >= 500

    return (
      <Unavailable
        icon={<AlertTriangle />}
        title="Cannot open the workspace"
        text={serverSide ? 'Something went wrong on our side. Try again in a moment.' : errorMessage(session.error)}
        onRetry={retry}
      />
    )
  }

  return <>{children}</>
}
