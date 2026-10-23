'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useState } from 'react'

import { AccountPicker } from '@/components/channels/account-picker'
import { PageContainer } from '@/components/common/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { api, errorMessage } from '@/lib/api'
import { qk, useProviders } from '@/lib/queries'
import { clearOAuthReturn, peekOAuthReturn } from '@/lib/welcome'

/**
 * Where the platform sends the browser back after OAuth. The API already handled `code` and `state` at its own
 * callback and redirected here with `channel=<id>`, `pending=<id>` or `error=<message>`. A channel or an error lands
 * on Channels (which shows the error over itself); a pending sign-in may publish as several accounts, so this page
 * asks which ones to connect first.
 */
export function OAuthCallback() {
  const params = useSearchParams()
  const router = useRouter()
  const qc = useQueryClient()
  const error = params.get('error')
  const channel = params.get('channel')
  const pending = params.get('pending')
  const provider = usePathname().split('/').filter(Boolean)[2] ?? ''

  // Started from the welcome wizard: back there (with the error, if any), else Channels.
  const [back] = useState(peekOAuthReturn)

  const land = (err: string | null, connected: string | null) => {
    void qc.invalidateQueries({ queryKey: qk.channels })

    if (back) {
      clearOAuthReturn()
      router.replace(err ? `${back}&error=${encodeURIComponent(err)}` : back)

      return
    }

    if (err) {
      router.replace(`/channels?${new URLSearchParams({ connect_error: err, provider })}`)

      return
    }

    router.replace(connected ? `/channels?connected=${encodeURIComponent(connected)}` : '/channels')
  }

  useEffect(() => {
    if (!pending) {
      land(error, channel)
    }
    // oxlint-disable-next-line react/exhaustive-deps -- land is recreated every render; these inputs decide it
  }, [pending, error, channel])

  const pc = useQuery({
    queryKey: ['pending-connection', pending],
    queryFn: () => api.pendingConnection(pending!),
    enabled: !!pending,
    retry: false,
  })
  const providers = useProviders()
  const finish = useMutation({
    mutationFn: (ids: string[]) => api.finishPendingConnection(pending!, ids),
    onSuccess: (r) => land(null, r.data[0]?.id ?? null),
    onError: (e) => land(errorMessage(e), null),
  })

  useEffect(() => {
    if (pc.error) {
      land(errorMessage(pc.error), null)
    }
    // oxlint-disable-next-line react/exhaustive-deps -- runs when the pending sign-in failed to load
  }, [pc.error])

  if (pending && pc.data) {
    const name = providers.data?.find((p) => p.id === pc.data.provider)?.name ?? pc.data.provider

    return (
      <PageContainer>
        <div className="mt-[6vh]">
          <AccountPicker
            provider={pc.data.provider}
            name={name}
            accounts={pc.data.accounts}
            busy={finish.isPending}
            onConnect={(ids) => finish.mutate(ids)}
            onCancel={() => land(null, null)}
          />
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
