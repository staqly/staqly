'use client'

import {
  Bot,
  CircleHelp,
  Code2,
  FileText,
  Flame,
  FlaskConical,
  GitBranch,
  Globe,
  Mail,
  MessagesSquare,
  Newspaper,
  Package,
  Pin,
  Play,
  Radar,
  Rocket,
  Rss,
  ScrollText,
  Sparkles,
  Tag,
  Users,
  type LucideIcon,
} from 'lucide-react'

import { NativeSelect } from '@/components/common/native-select'
import { UNLIMITED, type Watcher, type WatcherKind } from '@/lib/api'
import { useMode, usePlans, useUsage } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { INTERVAL_OPTIONS, intervalOption, planForInterval } from '@/lib/watchers'

const PRESET_ICONS: Record<string, { Icon: LucideIcon; cls: string }> = {
  'hn-front-page': { Icon: Flame, cls: 'bg-orange-500/12 text-orange-600 dark:text-orange-400' },
  'hn-keyword': { Icon: Flame, cls: 'bg-orange-500/12 text-orange-600 dark:text-orange-400' },
  'reddit-subreddit': { Icon: MessagesSquare, cls: 'bg-orange-600/12 text-orange-700 dark:text-orange-400' },
  'reddit-keyword': { Icon: MessagesSquare, cls: 'bg-orange-600/12 text-orange-700 dark:text-orange-400' },
  'pinterest-board': { Icon: Pin, cls: 'bg-rose-500/12 text-rose-600 dark:text-rose-400' },
  'youtube-channel': { Icon: Play, cls: 'bg-red-500/12 text-red-600 dark:text-red-400' },
  'github-releases': { Icon: Tag, cls: 'bg-zinc-500/12 text-zinc-700 dark:text-zinc-300' },
  'github-commits': { Icon: GitBranch, cls: 'bg-zinc-500/12 text-zinc-700 dark:text-zinc-300' },
  'npm-package': { Icon: Package, cls: 'bg-red-500/12 text-red-600 dark:text-red-400' },
  'pypi-package': { Icon: Package, cls: 'bg-sky-500/12 text-sky-700 dark:text-sky-300' },
  'stackoverflow-tag': { Icon: CircleHelp, cls: 'bg-amber-500/12 text-amber-700 dark:text-amber-300' },
  'rss-feed': { Icon: Rss, cls: 'bg-amber-500/12 text-amber-700 dark:text-amber-300' },
  'blog-newsletter': { Icon: Mail, cls: 'bg-violet-500/12 text-violet-700 dark:text-violet-300' },
  'arxiv-papers': { Icon: FileText, cls: 'bg-rose-500/12 text-rose-700 dark:text-rose-300' },
  'google-news': { Icon: Newspaper, cls: 'bg-sky-500/12 text-sky-700 dark:text-sky-300' },
  'product-hunt': { Icon: Rocket, cls: 'bg-orange-500/12 text-orange-600 dark:text-orange-400' },
  'url-changed': { Icon: Globe, cls: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300' },
  'changelog-status': { Icon: ScrollText, cls: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300' },
}

export const CATEGORY_ICONS: Record<string, LucideIcon> = {
  Community: Users,
  Inspiration: Sparkles,
  Content: Rss,
  Developer: Code2,
  Research: FlaskConical,
  News: Newspaper,
  Monitoring: Radar,
}

export function sourceIcon(presetId: string | undefined, kind: WatcherKind | string) {
  return (
    (presetId && PRESET_ICONS[presetId]) ||
    (kind === 'url_diff'
      ? { Icon: Globe, cls: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300' }
      : kind === 'rss'
        ? { Icon: Rss, cls: 'bg-amber-500/12 text-amber-700 dark:text-amber-300' }
        : { Icon: Bot, cls: 'bg-muted text-muted-foreground' })
  )
}

export function SourceIcon({ presetId, kind, className }: { presetId?: string; kind: string; className?: string }) {
  const { Icon, cls } = sourceIcon(presetId, kind)

  return (
    <span className={cn('flex size-9 shrink-0 items-center justify-center rounded-lg', cls, className)} aria-hidden>
      <Icon className="size-[18px]" />
    </span>
  )
}

export const KIND_LABELS: Record<string, string> = { rss: 'Feed', url_diff: 'Page changes' }

const STATUS: Record<Watcher['status'], { label: string; badge: string; dot: string }> = {
  active: {
    label: 'Active',
    badge: 'bg-emerald-500/12 text-emerald-700 ring-emerald-500/25 dark:text-emerald-300',
    dot: 'bg-emerald-500',
  },
  degraded: {
    label: 'Degraded',
    badge: 'bg-amber-500/12 text-amber-700 ring-amber-500/30 dark:text-amber-300',
    dot: 'bg-amber-500',
  },
  paused: {
    label: 'Paused',
    badge: 'bg-zinc-500/10 text-zinc-600 ring-zinc-500/20 dark:text-zinc-300',
    dot: 'bg-zinc-400',
  },
}

export function WatcherStatusBadge({ status }: { status: Watcher['status'] }) {
  const m = STATUS[status] ?? STATUS.paused

  return (
    <span
      data-testid="watcher-status"
      data-status={status}
      className={cn(
        'inline-flex h-5 shrink-0 items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium whitespace-nowrap ring-1 ring-inset',
        m.badge,
      )}
    >
      <span className={cn('size-1.5 rounded-full', m.dot)} />
      {m.label}
    </span>
  )
}

/** Plan's fastest allowed interval (seconds). Local installs: every minute. */
export function useMinInterval() {
  const usage = useUsage()
  const { isLocal } = useMode()
  const limit = usage.data?.plan.limits.min_watcher_interval_seconds

  return {
    minSeconds: isLocal ? Math.min(limit ?? 60, 60) : (limit ?? 900),
    planName: usage.data?.plan.name,
    loaded: !!usage.data || isLocal,
    unlimitedWatchers: isLocal || usage.data?.plan.limits.watchers === UNLIMITED,
  }
}

/** Interval picker; options faster than the plan allows are disabled with the plan that unlocks them. */
export function IntervalSelect({
  id,
  value,
  onChange,
  className,
}: {
  id?: string
  value: number
  onChange: (seconds: number) => void
  className?: string
}) {
  const { isLocal } = useMode()
  const plans = usePlans({ enabled: !isLocal })
  const { minSeconds } = useMinInterval()
  const options = INTERVAL_OPTIONS.some((o) => o.seconds === value)
    ? INTERVAL_OPTIONS
    : [...INTERVAL_OPTIONS, intervalOption(value)].toSorted((a, b) => a.seconds - b.seconds)
  const locked = options.filter((o) => o.seconds < minSeconds)
  const unlock = locked.length ? planForInterval(locked[locked.length - 1].seconds, plans.data) : undefined

  return (
    <div className={cn('grid gap-1.5', className)}>
      <NativeSelect
        id={id}
        value={String(value)}
        onChange={(e) => onChange(Number(e.target.value))}
        aria-label={id ? undefined : 'Check interval'}
      >
        {options.map((o) => {
          const disabled = o.seconds < minSeconds
          const plan = disabled ? planForInterval(o.seconds, plans.data) : undefined

          return (
            <option key={o.seconds} value={o.seconds} disabled={disabled}>
              {o.label}
              {disabled ? ` — ${plan ? `${plan.name} plan` : 'upgrade'}` : ''}
            </option>
          )
        })}
      </NativeSelect>
      {locked.length > 0 && (
        <p className="text-xs text-muted-foreground" data-testid="interval-hint">
          Faster checks need {unlock ? `the ${unlock.name} plan` : 'a bigger plan'}.
        </p>
      )}
    </div>
  )
}
