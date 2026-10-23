'use client'

import { useQueryClient } from '@tanstack/react-query'
import { CircleCheck, CircleX, LoaderCircle, Share2 } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { ChannelStatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError, errorMessage, type Channel } from '@/lib/api'
import { ProviderTile } from '@/lib/providers'
import { qk, useChannels, useProviders, useRole } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { SettingsCard } from './settings-card'

type Result = { ok: boolean; text: string }

/** Asks the platform about one channel; never throws, so Test all goes on after a failure. */
async function testOne(c: Channel, retry = true): Promise<Result> {
  try {
    const r = await api.testChannel(c.id)

    return r.ok
      ? { ok: true, text: r.profile?.handle ? `Works, signed in as ${r.profile.handle}` : 'Works' }
      : { ok: false, text: r.error || 'The platform no longer accepts the sign-in' }
  } catch (e) {
    // Even sequential requests can exhaust the server's channel-test burst limit.
    if (retry && e instanceof ApiError && e.status === 429) {
      await new Promise((resolve) => setTimeout(resolve, (e.retryAfter || 1) * 1000))

      return testOne(c, false)
    }

    return { ok: false, text: `Not checked: ${errorMessage(e)}` }
  }
}

/** The workspace's channels: which platforms this server can connect, each channel's state and a live test. */
export function ChannelsHealthCard() {
  const channels = useChannels()
  const providers = useProviders()
  const { role } = useRole()
  const canManageChannels = role === 'owner' || role === 'admin'
  const qc = useQueryClient()
  const [results, setResults] = useState<Record<string, Result>>({})
  const [running, setRunning] = useState<string[]>([])
  const list = channels.data
  const available = (providers.data ?? []).filter((p) => p.status === 'available' && p.id !== 'custom')

  async function test(cs: Channel[]) {
    if (!canManageChannels || running.length > 0) {
      return
    }

    setRunning((r) => [...r, ...cs.map((c) => c.id)])

    // One at a time: a platform that rate-limits sign-in checks sees one request, not a burst.
    for (const c of cs) {
      const r = await testOne(c)

      setResults((all) => ({ ...all, [c.id]: r }))
      setRunning((ids) => ids.filter((id) => id !== c.id))
    }

    // A test may change a channel's state (needs reconnect, or connected again).
    void qc.invalidateQueries({ queryKey: qk.channels })
  }

  return (
    <SettingsCard
      title="Channels"
      icon={<Share2 />}
      description="The platforms this server can publish to, and whether each connected account still signs in."
      action={
        canManageChannels && list && list.length > 0 ? (
          <Button size="sm" variant="outline" onClick={() => void test(list)} disabled={running.length > 0}>
            {running.length > 0 ? 'Testing...' : 'Test all'}
          </Button>
        ) : undefined
      }
    >
      <div className="grid gap-4" data-testid="channels-health">
        <div className="grid gap-2">
          <h3 className="text-xs font-medium text-muted-foreground">Ready to connect on this server</h3>
          {providers.isLoading ? (
            <Skeleton className="h-8" />
          ) : providers.error ? (
            <ErrorState title="Could not load platforms" error={providers.error} onRetry={() => providers.refetch()} />
          ) : available.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No platform has its app keys on this server yet; your own automation always works.
            </p>
          ) : (
            <ul className="flex flex-wrap gap-2" aria-label="Platforms ready to connect">
              {available.map((p) => (
                <li key={p.id} className="flex items-center gap-2 rounded-full border py-1 pr-3 pl-1 text-sm">
                  <ProviderTile id={p.id} name={p.name} className="size-6" />
                  {p.name}
                </li>
              ))}
            </ul>
          )}
        </div>

        {channels.isLoading ? (
          <Skeleton className="h-24" />
        ) : channels.error ? (
          <ErrorState title="Could not load channels" error={channels.error} onRetry={() => channels.refetch()} />
        ) : !list?.length ? (
          <p className="text-sm text-muted-foreground">
            No channels connected yet.{' '}
            {canManageChannels && (
              <Link href="/channels?add=1" className="text-foreground underline underline-offset-4 hover:no-underline">
                Add a channel
              </Link>
            )}
          </p>
        ) : (
          <ul className="divide-y rounded-lg border" aria-label="Connected channels">
            {list.map((c) => {
              const busy = running.includes(c.id)
              const r = results[c.id]

              return (
                <li key={c.id} className="flex flex-wrap items-center gap-3 px-3 py-2.5 text-sm">
                  <ProviderTile id={c.provider} className="size-6 shrink-0" />
                  <span className="min-w-0 flex-1 basis-40">
                    <span className="block truncate font-medium" title={c.name}>
                      {c.name}
                    </span>
                    {busy ? (
                      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <LoaderCircle className="size-3.5 animate-spin" aria-hidden /> Testing...
                      </span>
                    ) : r ? (
                      <span
                        className={cn(
                          'flex items-start gap-1.5 text-xs wrap-anywhere',
                          TONES[r.ok ? 'success' : 'danger'].text,
                        )}
                        role="status"
                      >
                        {r.ok ? (
                          <CircleCheck className="mt-px size-3.5 shrink-0" aria-hidden />
                        ) : (
                          <CircleX className="mt-px size-3.5 shrink-0" aria-hidden />
                        )}
                        {r.text}
                      </span>
                    ) : (
                      c.last_error && (
                        <span className="block text-xs text-muted-foreground wrap-anywhere">{c.last_error}</span>
                      )
                    )}
                  </span>
                  <ChannelStatusBadge status={c.status} />
                  {canManageChannels && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void test([c])}
                      disabled={running.length > 0}
                      aria-label={`Test ${c.name}`}
                    >
                      Test
                    </Button>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </SettingsCard>
  )
}
