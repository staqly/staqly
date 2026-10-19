'use client'

import { useQueryClient } from '@tanstack/react-query'
import { useRouter, useSearchParams } from 'next/navigation'
import { useEffect } from 'react'

import { PageContainer } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { qk } from '@/lib/queries'

/**
 * Where the platform sends the browser back after OAuth. The API already handled `code` and `state` at its own
 * callback and redirected here with `channel=<id>` or `error=<message>`; this page just lands on Channels.
 */
export function OAuthCallback() {
  const params = useSearchParams()
  const router = useRouter()
  const qc = useQueryClient()
  const error = params.get('error')
  const channel = params.get('channel')

  useEffect(() => {
    if (!error) {
      void qc.invalidateQueries({ queryKey: qk.channels })
      router.replace(channel ? `/channels?connected=${encodeURIComponent(channel)}` : '/channels')
    }
  }, [error, channel, qc, router])

  if (error) {
    return (
      <PageContainer>
        <div className="mx-auto mt-[10vh] max-w-md rounded-xl border p-6 text-center">
          <h1 className="text-lg font-semibold">The channel was not connected</h1>
          <p className="mt-2 text-sm text-muted-foreground wrap-anywhere">{error}</p>
          <Button className="mt-4" onClick={() => router.replace('/channels')}>
            Back to Channels
          </Button>
        </div>
      </PageContainer>
    )
  }

  return (
    <PageContainer>
      <h1 className="sr-only">Connecting channel</h1>
      <Skeleton className="mx-auto mt-[10vh] h-24 max-w-md" />
    </PageContainer>
  )
}
