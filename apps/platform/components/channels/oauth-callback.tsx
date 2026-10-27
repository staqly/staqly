'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useState } from 'react'

import { AccountPicker } from '@/components/channels/account-picker'
import { PageContainer } from '@/components/common/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError, errorMessage } from '@/lib/api'
import { qk, useProviders } from '@/lib/queries'
import { openTrialUsed, TRIAL_USED } from '@/lib/trial-used'
import { clearOAuthReturn, peekOAuthReturn } from '@/lib/welcome'

/**
 * Where the platform sends the browser back after OAuth. The API already handled `code` and `state` at its own
 * callback and redirected here with `channel=<id>`, `pending=<id>` or `error=<message>` (with `code` when the
 * accounts were used in an earlier unpaid free trial: the shell's dialog explains that instead). A channel or an
 * error lands on Channels (which shows the error over itself); a pending sign-in may publish as several accounts, so
 * this page asks which ones to connect first.
 */
export function OAuthCallback() {
  const params = useSearchParams()
  const router = useRouter()
  const qc = useQueryClient()
  const error = params.get('error')
  const errorCode = params.get('code')
  const channel = params.get('channel')
  const pending = params.get('pending')
  const provider = usePathname().split('/').filter(Boolean)[2] ?? ''

  // Started from the welcome wizard: back there (with the error, if any), else Channels.
  const [back] = useState(peekOAuthReturn)

  const land = (failure: string | null, connected: string | null, code: string | null = null) => {
    void qc.invalidateQueries({ queryKey: qk.channels })
    // Accounts an earlier unpaid free trial used: the shell explains it (with the way to pay) over the next page.
    const trialUsed = code === TRIAL_USED
    const err = trialUsed ? null : failure

    if (trialUsed) {
      void qc.invalidateQueries({ queryKey: qk.billing })
      void qc.invalidateQueries({ queryKey: qk.usage })
      openTrialUsed()
    }

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
      land(error, channel, errorCode)
    }
    // oxlint-disable-next-line react/exhaustive-deps -- land is recreated every render; these inputs decide it
  }, [pending, error, channel, errorCode])

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
    onError: (e) => land(errorMessage(e), null, e instanceof ApiError ? e.code : null),
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
