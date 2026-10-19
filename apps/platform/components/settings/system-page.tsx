'use client'

import { useQuery } from '@tanstack/react-query'
import { BookOpen, CircleCheck, CircleX, HeartPulse, LoaderCircle, RefreshCw } from 'lucide-react'
import { useState } from 'react'

import { InlineCode } from '@/components/common/code-block'
import { DocsLink } from '@/components/common/docs-link'
import { ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Readiness } from '@/lib/api'
import { usePublicApiUrl } from '@/lib/config'
import { timeAgo } from '@/lib/format'
import { qk, useMode } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { QueueCard } from './queue-card'
import { SettingsCard } from './settings-card'
import { SettingsNav } from './settings-nav'

const CHECKS: { key: keyof Readiness['checks']; label: string; hint: string }[] = [
  { key: 'database', label: 'Database', hint: 'SQLite answers a ping' },
  { key: 'migrations', label: 'Migrations', hint: 'Every schema migration is applied' },
  { key: 'workers', label: 'Background workers', hint: 'The job queue, webhooks and alerts are running' },
]

/** `not_started` is not a failure (processes without workers, e.g. tests). */
function checkOk(key: keyof Readiness['checks'], value?: string) {
  if (!value) {
    return null
  }

  if (key === 'workers' && value === 'not_started') {
    return true
  }

  return value === 'ok'
}

function ReadinessCard() {
  const ready = useQuery({ queryKey: qk.readiness, queryFn: api.readyz, refetchInterval: 30_000, staleTime: 0 })
  const health = useQuery({ queryKey: qk.health, queryFn: api.health, staleTime: 60_000 })
  const ok = ready.data?.status === 'ok'
  // The checks answer in milliseconds: a manual check stays visible for a moment so the click reads as done.
  const [checking, setChecking] = useState(false)

  async function checkNow() {
    setChecking(true)
    await Promise.all([ready.refetch(), new Promise((r) => setTimeout(r, 700))])
    setChecking(false)
  }

  return (
    <SettingsCard
      title="Health"
      icon={<HeartPulse />}
      description="Is everything running? Checked again every 30 seconds."
      action={
        <Button size="sm" variant="outline" onClick={() => void checkNow()} disabled={checking}>
          <RefreshCw className={checking ? 'animate-spin' : undefined} /> {checking ? 'Checking...' : 'Check now'}
        </Button>
      }
    >
      {ready.isLoading ? (
        <Skeleton className="h-28" />
      ) : ready.error ? (
        <ErrorState title="API not reachable" error={ready.error} onRetry={() => ready.refetch()} />
      ) : (
        <div className="grid gap-3" data-testid="readiness" data-status={ready.data?.status}>
          <div
            className={cn(
              'flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium',
              TONES[ok ? 'success' : 'danger'].chip,
            )}
          >
            {ok ? <CircleCheck className="size-4" aria-hidden /> : <CircleX className="size-4" aria-hidden />}
            {ok ? 'Ready' : 'Not ready'}
            <span className="text-xs font-normal text-muted-foreground" data-testid="readiness-checked">
              {checking ? 'Checking...' : `checked ${timeAgo(new Date(ready.dataUpdatedAt).toISOString())}`}
            </span>
            {health.data?.version && (
              <span className="ml-auto font-mono text-xs font-normal text-muted-foreground">
                {/^\d/.test(health.data.version) ? `v${health.data.version}` : health.data.version}
              </span>
            )}
          </div>
          <ul className="divide-y rounded-lg border">
            {CHECKS.map((c) => {
              const value = ready.data?.checks?.[c.key]
              const good = checkOk(c.key, value)

              return (
                <li key={c.key} className="flex items-center gap-3 px-3 py-2.5 text-sm">
                  {checking ? (
                    <LoaderCircle className="size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />
                  ) : good === null ? (
                    <span className="size-4 shrink-0 rounded-full border" aria-hidden />
                  ) : good ? (
                    <CircleCheck className={cn('size-4 shrink-0', TONES.success.text)} aria-hidden />
                  ) : (
                    <CircleX className={cn('size-4 shrink-0', TONES.danger.text)} aria-hidden />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium">{c.label}</span>
                    <span className="block text-xs text-muted-foreground">{c.hint}</span>
                  </span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">
                    {checking ? 'checking' : (value ?? '—')}
                  </span>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </SettingsCard>
  )
}

export function SystemPage() {
  const apiUrl = usePublicApiUrl()
  const { isLocal } = useMode()

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        <ReadinessCard />
        <QueueCard />
        <SettingsCard title="Monitoring" icon={<BookOpen />} description="For uptime checks, Prometheus and alerts.">
          <ul className="grid gap-2 text-sm">
            <li>
              <InlineCode>GET {apiUrl}/livez</InlineCode>{' '}
              <span className="text-muted-foreground">— liveness, never touches the database</span>
            </li>
            <li>
              <InlineCode>GET {apiUrl}/readyz</InlineCode>{' '}
              <span className="text-muted-foreground">— readiness (what this page shows)</span>
            </li>
            <li>
              <InlineCode>GET {apiUrl}/metrics</InlineCode>{' '}
              <span className="text-muted-foreground">
                — Prometheus; {isLocal ? 'allowed for curl on this machine, otherwise needs' : 'needs'}{' '}
                <InlineCode>METRICS_TOKEN</InlineCode>
              </span>
            </li>
            <li className="text-muted-foreground">
              Request IDs, rate limits and Grafana tips:{' '}
              <DocsLink path="/self-hosting/logs-and-tracing">Logs &amp; tracing</DocsLink>
            </li>
          </ul>
        </SettingsCard>
      </div>
    </PageContainer>
  )
}
