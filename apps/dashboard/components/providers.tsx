'use client'

import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemeProvider } from 'next-themes'
import { usePathname, useRouter } from 'next/navigation'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { z } from 'zod'

import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api, ApiError, onUnauthorized } from '@/lib/api'
import { setNavigator } from '@/lib/notify'

// Zod probes for `new Function` to JIT-compile object schemas; our CSP has no
// 'unsafe-eval', so the probe would be reported as a CSP violation on every page.
z.config({ jitless: true })

const PUBLIC_ROUTES = new Set(['/setup', '/login', '/connect'])

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
    const target = '/login'

    try {
      const status = await api.setupStatus()

      if (status.mode === 'local') {
        // No login exists in local mode; the auth gate shows the error instead.
        redirecting.current = false

        return
      }
      // Cloud servers host many workspaces; /login links to "create a workspace".
    } catch {
      // Keep /login; the page itself explains if the API is unreachable.
    }

    client.removeQueries({ predicate: (q) => q.queryKey[0] !== 'setup' })
    const next = target === '/login' && path && path !== '/' ? `?next=${encodeURIComponent(path)}` : ''

    router.replace(target + next)
  }

  useEffect(() => {
    handleAuthError = () => void redirectToAuth()
    onUnauthorized(() => void redirectToAuth())
    setNavigator((href) => router.push(href))

    return () => {
      onUnauthorized(null)
      setNavigator(null)
    }
  })

  return (
    <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
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
