'use client'

import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from 'next-themes'
import { usePathname, useRouter } from 'next/navigation'
import { useEffect, useRef, useState, type ReactNode } from 'react'

import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api, ApiError, onUnauthorized } from '@/lib/api'
import { captureLinkToken, scrubLinkToken } from '@/lib/local-token'
import { setNavigator } from '@/lib/notify'

// zod's jitless setting (our CSP has no 'unsafe-eval') lives in lib/zod.ts, which every form imports `z`
// from: setting it here put zod/v4/core into the JavaScript of every route.

// A local install's sign-in link (#token=...): out of the address bar before any page redirects.
captureLinkToken()

// lib/queries' qk.setup and qk.session, written out: importing lib/queries would put the query hooks into the
// root layout's JavaScript.
const SETUP_KEY = ['setup']
const SESSION_KEY = ['session']

const PUBLIC_ROUTES = new Set(['/setup', '/signup', '/login', '/connect', '/invite'])

/** 401 handler of the query client; set by the mounted Providers. */
let handleAuthError = () => {}

function makeClient(onAuthError: () => void) {
  const handle = (err: unknown) => {
    if (err instanceof ApiError && err.status === 401) {
      onAuthError()
    }
  }

  return new QueryClient({
    queryCache: new QueryCache({ onError: handle }),
    mutationCache: new MutationCache({ onError: handle }),
    defaultOptions: {
      queries: {
        staleTime: 5_000,
        refetchOnWindowFocus: true,
        retry: (count, err) => {
          if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
            return false
          }

          return count < 2
        },
      },
    },
  })
}

/**
 * The server's mode, from the cache when it is fresh: a burst of 401s (every query of a page) asks the server
 * once. Null when it can't be told (the API is unreachable): the caller goes on to /login, which explains.
 */
function serverMode(client: QueryClient): Promise<'local' | 'cloud' | null> {
  return client
    .fetchQuery({ queryKey: SETUP_KEY, queryFn: api.setupStatus, staleTime: 30_000 })
    .then((s) => s.mode ?? null)
    .catch(() => null)
}

/**
 * Local mode has no /login: the auth gate shows the access-token sign-in once the session query fails. Ask it
 * again now (the session was revoked, say by rotating the token in another browser) instead of waiting up to a
 * minute with every request failing, unless the session query is the one that just failed.
 */
function checkLocalSession(client: QueryClient) {
  const session = client.getQueryState(SESSION_KEY)

  if (session && session.status !== 'error' && session.fetchStatus !== 'fetching') {
    void client.invalidateQueries({ queryKey: SESSION_KEY })
  }
}

export function Providers({ children }: { children: ReactNode }) {
  const router = useRouter()
  const pathname = usePathname()
  const pathRef = useRef(pathname)
  const redirecting = useRef(false)

  useEffect(() => {
    pathRef.current = pathname

    if (PUBLIC_ROUTES.has(pathname)) {
      redirecting.current = false
    }
  }, [pathname])

  // The client is created once, before redirectToAuth exists, and calls the latest one through
  // handleAuthError (referencing a function before its declaration makes the React Compiler skip the component).
  const [client] = useState(() => makeClient(() => handleAuthError()))

  async function redirectToAuth() {
    const path = pathRef.current

    if (redirecting.current || PUBLIC_ROUTES.has(path)) {
      return
    }

    redirecting.current = true

    if ((await serverMode(client)) === 'local') {
      redirecting.current = false
      checkLocalSession(client)

      return
    }

    client.removeQueries({ predicate: (q) => q.queryKey[0] !== SETUP_KEY[0] })
    const next = path && path !== '/' ? `?next=${encodeURIComponent(path)}` : ''

    router.replace('/login' + next)
  }

  // After hydration (Next writes its first URL back, with the fragment): the token never stays visible.
  useEffect(() => {
    const t = setTimeout(scrubLinkToken, 0)

    return () => clearTimeout(t)
  }, [])

  useEffect(() => {
    handleAuthError = () => void redirectToAuth()
    onUnauthorized(() => void redirectToAuth())
    setNavigator((href) => router.push(href))

    return () => {
      onUnauthorized(null)
      setNavigator(null)
    }
  })

  // Dark until the user picks otherwise in the theme menu (light, dark or system).
  return (
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem disableTransitionOnChange>
      <QueryClientProvider client={client}>
        <TooltipProvider delayDuration={300}>
          {children}
          <Toaster
            position="bottom-right"
            closeButton
            mobileOffset={{ bottom: 'calc(env(safe-area-inset-bottom) + 76px)' }}
          />
        </TooltipProvider>
      </QueryClientProvider>
    </ThemeProvider>
  )
}
