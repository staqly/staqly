'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'

import { api, ApiError, errorMessage, setCsrfToken } from '@/lib/api'
import { qk } from '@/lib/queries'
import { rememberApiKey } from '@/lib/session-key'

/** Plain words for a sign-in with the access token that did not work. */
export function accessTokenErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401) {
      return 'This is not the access token of this computer. Copy it again from where Staqly started.'
    }

    if (err.status === 429) {
      return 'Too many attempts. Wait a minute and try again.'
    }
  }

  return errorMessage(err)
}

/** Signs this browser in with the access token; the dashboard loads right after. */
export function useAccessTokenSignIn(onDone?: () => void) {
  const qc = useQueryClient()

  return useMutation({
    mutationFn: (token: string) => api.signInWithAccessToken(token),
    onSuccess: (res) => {
      setCsrfToken(res.csrf_token ?? null)
      rememberApiKey(null)
      // Everything read before belonged to no one: start over (the gate's session query loads again).
      void qc.resetQueries({ predicate: (q) => q.queryKey[0] !== qk.setup[0] && q.queryKey[0] !== 'plans' })
      onDone?.()
    },
  })
}
