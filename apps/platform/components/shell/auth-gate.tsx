'use client'

import { LogoMark } from '@staqly/brand/logo'
import dynamic from 'next/dynamic'
import { useEffect, useRef, useState, type ReactNode } from 'react'

import { accessTokenErrorMessage, useAccessTokenSignIn } from '@/components/auth/access-token-sign-in'
import { ErrorState } from '@/components/common/error-state'
import { ApiError } from '@/lib/api'
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

  if (session.isPending) {
    return <Splash />
  }

  if (session.error) {
    const unauthorized = session.error instanceof ApiError && session.error.status === 401
    // status 0: the API did not answer at all (stopped, restarting, network).
    const unreachable = session.error instanceof ApiError && session.error.status === 0

    if (unauthorized && setup.data?.mode === 'local') {
      return <LocalSignIn initialError={linkError} />
    }

    // In cloud mode a 401 becomes a redirect to /login (see <Providers>); the mode may still be loading.
    if (unauthorized) {
      return <Splash />
    }

    return (
      <div className="flex min-h-svh items-center justify-center p-6">
        <ErrorState
          className="max-w-md"
          title={unreachable ? 'Cannot reach Staqly' : 'Cannot open the workspace'}
          error={session.error}
          onRetry={() => session.refetch()}
        />
      </div>
    )
  }

  return <>{children}</>
}
