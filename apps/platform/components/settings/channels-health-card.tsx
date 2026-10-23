'use client'

import { CircleCheck, CircleX, LoaderCircle, Share2 } from 'lucide-react'
import { useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError, errorMessage, type ProviderCheck } from '@/lib/api'
import { ProviderTile } from '@/lib/providers'
import { useProviders, useRole } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { SettingsCard } from './settings-card'

type Outcome = { check?: ProviderCheck; error?: string }

/** One platform's check; never throws, so Test all goes on after a failure. A rate limit is waited out once. */
async function checkOne(id: string, retry = true): Promise<Outcome> {
  try {
    return { check: await api.checkProvider(id) }
  } catch (e) {
    if (retry && e instanceof ApiError && e.status === 429) {
      await new Promise((resolve) => setTimeout(resolve, (e.retryAfter || 1) * 1000))

      return checkOne(id, false)
    }

    return { error: `Not checked: ${errorMessage(e)}` }
  }
}

/**
 * This server's side of each platform people connect through its sign-in: app keys, whether the platform takes
 * them, the return address to register and (Instagram) media storage it can download from. Accounts are tested on
 * the Channels page.
 */
export function ChannelsHealthCard() {
  const providers = useProviders()
  // Checking calls the platforms with the server's app keys: owners and admins only (channels:manage).
  const { role } = useRole()
  const canCheck = role === 'owner' || role === 'admin'
  const [results, setResults] = useState<Record<string, Outcome>>({})
  const [running, setRunning] = useState<string[]>([])
  const oauth = (providers.data ?? []).filter((p) => p.auth === 'oauth2' && p.app_configured)

  async function test(ids: string[]) {
    if (!canCheck || running.length > 0) {
      return
    }

    setRunning(ids)

    // One at a time: each check calls the platform once.
    for (const id of ids) {
      const out = await checkOne(id)

      setResults((all) => ({ ...all, [id]: out }))
      setRunning((r) => r.filter((x) => x !== id))
    }
  }

  return (
    <SettingsCard
      title="Platforms"
      icon={<Share2 />}
      description="Is this server set up for each platform: its app keys, whether the platform takes them, and what else it needs. Accounts are tested on the Channels page."
      action={
        canCheck && oauth.length > 0 ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => void test(oauth.map((p) => p.id))}
            disabled={running.length > 0}
          >
            {running.length > 0 ? 'Testing...' : 'Test all'}
          </Button>
        ) : undefined
      }
    >
      {providers.isLoading ? (
        <Skeleton className="h-24" />
      ) : providers.error ? (
        <ErrorState title="Could not load platforms" error={providers.error} onRetry={() => providers.refetch()} />
      ) : oauth.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No platform has its app keys on this server yet; your own automation always works.
        </p>
      ) : (
        <ul
          className="divide-y rounded-lg border"
          aria-label="Platforms set up on this server"
          data-testid="platforms-health"
        >
          {oauth.map((p) => {
            const busy = running.includes(p.id)
            const r = results[p.id]

            return (
              <li key={p.id} className="grid gap-2 px-3 py-2.5 text-sm">
                <div className="flex items-center gap-3">
                  <ProviderTile id={p.id} name={p.name} className="size-6 shrink-0" />
                  <span className="min-w-0 flex-1 truncate font-medium">{p.name}</span>
                  {r?.check && !busy && (
                    <span className={cn('text-xs font-medium', TONES[r.check.ok ? 'success' : 'danger'].text)}>
                      {r.check.ok ? 'Ready' : 'Needs attention'}
                    </span>
                  )}
                  {canCheck && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => void test([p.id])}
                      disabled={running.length > 0}
                      aria-label={`Test ${p.name}`}
                    >
                      {busy ? <LoaderCircle className="animate-spin" aria-hidden /> : null}
                      Test
                    </Button>
                  )}
                </div>
                {!busy && r?.error && (
                  <p role="status" className={cn('text-xs wrap-anywhere', TONES.danger.text)}>
                    {r.error}
                  </p>
                )}
                {!busy && r?.check && (
                  <ul className="grid gap-1.5 pl-9" role="status" aria-label={`${p.name} checks`}>
                    {r.check.checks.map((c) => (
                      <li key={c.key} className="flex items-start gap-2 text-xs">
                        {c.ok ? (
                          <CircleCheck className={cn('mt-px size-3.5 shrink-0', TONES.success.text)} aria-hidden />
                        ) : (
                          <CircleX className={cn('mt-px size-3.5 shrink-0', TONES.danger.text)} aria-hidden />
                        )}
                        <span className="min-w-0">
                          <span className="font-medium">{c.label}</span>
                          <span className="text-muted-foreground">: </span>
                          {c.key === 'return_address' && c.ok ? (
                            <code className="font-mono wrap-anywhere">{c.detail}</code>
                          ) : (
                            <span className="text-muted-foreground wrap-anywhere">{c.detail}</span>
                          )}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </SettingsCard>
  )
}
