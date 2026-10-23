'use client'

import { useQueryClient } from '@tanstack/react-query'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useState } from 'react'

import { PageContainer } from '@/components/common/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { qk } from '@/lib/queries'
import { clearOAuthReturn, peekOAuthReturn } from '@/lib/welcome'

/**
 * Where the platform sends the browser back after OAuth. The API already handled `code` and `state` at its own
 * callback and redirected here with `channel=<id>` or `error=<message>`; this page just lands on Channels, which
 * shows an error over itself.
 */
export function OAuthCallback() {
  const params = useSearchParams()
  const router = useRouter()
  const qc = useQueryClient()
  const error = params.get('error')
  const channel = params.get('channel')
  const provider = usePathname().split('/').filter(Boolean)[2] ?? ''

  // Started from the welcome wizard: back there (with the error, if any), else Channels.
  const [back] = useState(peekOAuthReturn)

  useEffect(() => {
    if (back) {
      clearOAuthReturn()
      void qc.invalidateQueries({ queryKey: qk.channels })
      router.replace(error ? `${back}&error=${encodeURIComponent(error)}` : back)

      return
    }

    if (error) {
      router.replace(`/channels?${new URLSearchParams({ connect_error: error, provider })}`)

      return
    }

    void qc.invalidateQueries({ queryKey: qk.channels })
    router.replace(channel ? `/channels?connected=${encodeURIComponent(channel)}` : '/channels')
  }, [back, error, channel, provider, qc, router])

  return (
    <PageContainer>
      <h1 className="sr-only">Connecting channel</h1>
      <Skeleton className="mx-auto mt-[10vh] h-24 max-w-md" />
    </PageContainer>
  )
}
