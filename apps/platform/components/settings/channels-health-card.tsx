'use client'

import { CircleCheck, CircleHelp, CircleX, LoaderCircle, RefreshCw, Share2 } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { api, ApiError, errorMessage, type ProviderCheck } from '@/lib/api'
import { ProviderTile } from '@/lib/providers'
import { useProviders, useRole } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { SettingsCard } from './settings-card'

/** `why` is the server's explanation, shown in the tooltip; the line itself says it in plain words. */
type Outcome = { ok: boolean; key?: string; why?: string }

/** What a failed check means for the people connecting, by the check's key. */
const HEADLINES: Record<string, (name: string) => string> = {
  keys: (name) => `${name} isn't set up on this server yet`,
  platform: (name) => `${name} isn't letting this server connect right now`,
  return_address: (name) => `${name} needs a secure (https) address before people can connect`,
  media: (name) => `${name} needs media storage it can reach to post pictures and videos`,
  unchecked: (name) => `We couldn't reach ${name} to check it. We'll try again soon`,
}

/** One platform's check, as one line: ready, or the first thing to fix. Never throws; a rate limit is waited out once. */
async function checkOne(id: string, retry = true): Promise<Outcome> {
  try {
    const c: ProviderCheck = await api.checkProvider(id)
    const problem = c.checks.find((x) => !x.ok)

    return problem ? { ok: false, key: problem.key, why: problem.detail } : { ok: true }
  } catch (e) {
    if (retry && e instanceof ApiError && e.status === 429) {
      await new Promise((resolve) => setTimeout(resolve, (e.retryAfter || 1) * 1000))

      return checkOne(id, false)
    }

    return { ok: false, key: 'unchecked', why: errorMessage(e) }
  }
}

/** Checks platforms one at a time (each check calls the platform once), filling in each line as it answers. */
async function runChecks(
  list: string[],
  setResults: React.Dispatch<React.SetStateAction<Record<string, Outcome>>>,
  setRunning: React.Dispatch<React.SetStateAction<string[]>>,
) {
  setRunning(list)

  for (const id of list) {
    const out = await checkOne(id)

    setResults((all) => ({ ...all, [id]: out }))
    setRunning((r) => r.filter((x) => x !== id))
  }
}

/**
 * This server's side of each platform people connect through its sign-in (app keys, the platform taking them, what
 * else it needs), shown like Health: one line each. Accounts are tested on the Channels page.
 */
export function ChannelsHealthCard() {
  const providers = useProviders()
  // Checking calls the platforms with the server's app keys: owners and admins only (channels:manage).
  const { role } = useRole()
  const canCheck = role === 'owner' || role === 'admin'
  const [results, setResults] = useState<Record<string, Outcome>>({})
  const [running, setRunning] = useState<string[]>([])
  const oauth = (providers.data ?? []).filter((p) => p.auth === 'oauth2' && p.app_configured)
  const ids = oauth.map((p) => p.id).join(',')

  const checking = useRef(false)
  const check = useCallback(
    (list: string[]) => {
      if (canCheck && list.length > 0 && !checking.current) {
        checking.current = true
        void runChecks(list, setResults, setRunning).then(() => {
          checking.current = false
        })
      }
    },
    [canCheck],
  )

  useEffect(() => {
    if (!canCheck || !ids) {
      return
    }

    const list = ids.split(',')

    check(list)
    const timer = setInterval(() => check(list), 15 * 60_000)

    return () => clearInterval(timer)
  }, [canCheck, check, ids])

  return (
    <SettingsCard
      title="Platforms"
      icon={<Share2 />}
      description="Can people connect and post? Checked again every 15 minutes."
      action={
        canCheck && oauth.length > 0 ? (
          <Button
            size="sm"
            variant="outline"
            onClick={() => check(oauth.map((p) => p.id))}
            disabled={running.length > 0}
          >
            <RefreshCw className={running.length > 0 ? 'animate-spin' : undefined} />{' '}
            {running.length > 0 ? 'Checking...' : 'Check now'}
          </Button>
        ) : undefined
      }
    >
      {providers.isLoading ? (
        <Skeleton className="h-28" />
      ) : providers.error ? (
        <ErrorState title="Could not load platforms" error={providers.error} onRetry={() => providers.refetch()} />
      ) : oauth.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No platform is set up on this server yet; your own automation always works.
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
              <li key={p.id} className="flex items-center gap-3 px-3 py-2.5 text-sm">
                {busy ? (
                  <LoaderCircle className="size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />
                ) : !r ? (
                  <span className="size-4 shrink-0 rounded-full border" aria-hidden />
                ) : r.ok ? (
                  <CircleCheck className={cn('size-4 shrink-0', TONES.success.text)} aria-hidden />
                ) : (
                  <CircleX className={cn('size-4 shrink-0', TONES.danger.text)} aria-hidden />
                )}
                <ProviderTile id={p.id} name={p.name} className="size-6 shrink-0" />
                <span className="min-w-0 flex-1">
                  <span className="block font-medium">{p.name}</span>
                  {!busy && !r?.ok && (
                    <span className="flex items-center gap-1 text-xs text-muted-foreground">
                      {r ? (HEADLINES[r.key ?? ''] ?? HEADLINES.platform!)(p.name) : 'Set up on this server'}
                      {r?.why && (
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              type="button"
                              aria-label={`Why ${p.name} needs attention`}
                              className="inline-flex size-6 shrink-0 items-center justify-center rounded-full hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                            >
                              <CircleHelp className="size-3.5" aria-hidden />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent className="wrap-anywhere">{r.why}</TooltipContent>
                        </Tooltip>
                      )}
                    </span>
                  )}
                </span>
                <span className="shrink-0 font-mono text-xs text-muted-foreground">
                  {busy ? 'checking' : r ? (r.ok ? 'ok' : 'needs attention') : '—'}
                </span>
              </li>
            )
          })}
        </ul>
      )}
    </SettingsCard>
  )
}
