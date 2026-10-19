'use client'

import { toast } from 'sonner'

import { ApiError, errorMessage, type PlanLimitDetails, sentence } from './api'
import { formatBytes } from './format'

const PLANS_HREF = '/settings/plan'

/** Navigation used by toast actions; replaced by the app router in <Providers>. */
let navigate: (href: string) => void = (href) => {
  if (typeof window !== 'undefined') {
    window.location.assign(href)
  }
}

export function setNavigator(fn: ((href: string) => void) | null) {
  navigate =
    fn ??
    ((href) => {
      if (typeof window !== 'undefined') {
        window.location.assign(href)
      }
    })
}

const LIMIT_LABELS: Record<string, string> = {
  posts_per_month: 'posts this month',
  channels: 'channels',
  alerts: 'alerts',
  webhooks: 'webhooks',
  api_keys: 'API keys',
  storage_bytes: 'storage',
  max_upload_bytes: 'maximum upload size',
  notification_channels: 'notification channels',
}

const PLAN_NAMES: Record<string, string> = {
  starter: 'Starter',
  plus: 'Plus',
  pro: 'Pro',
  business: 'Business',
  unlocked: 'Unlocked',
}

export function planName(id?: string) {
  if (!id) {
    return ''
  }

  return PLAN_NAMES[id] ?? id.charAt(0).toUpperCase() + id.slice(1)
}

/** Human description of a plan limit, e.g. "Your Starter plan includes 3 webhooks (3 used)." */
export function describePlanLimit(d: PlanLimitDetails | undefined, fallback: string): string {
  if (!d?.limit || d.max === undefined) {
    return fallback
  }

  const plan = d.plan ? `Your ${planName(d.plan)} plan` : 'Your plan'

  if (d.limit === 'max_upload_bytes') {
    return `${plan} accepts files up to ${formatBytes(d.max)}.`
  }

  if (d.limit === 'storage_bytes') {
    return `${plan} includes ${formatBytes(d.max)} of storage${d.used !== undefined ? ` (${formatBytes(d.used)} used)` : ''}.`
  }

  const label = LIMIT_LABELS[d.limit] ?? d.limit.replace(/_/g, ' ')
  const what = d.limit === 'posts_per_month' ? `${d.max.toLocaleString('en-US')} posts per month` : `${d.max} ${label}`

  return `${plan} includes ${what}${d.used !== undefined ? ` (${d.used.toLocaleString('en-US')} used)` : ''}.`
}

/**
 * Toast for a failed API call. Plan limits (402) get a friendly explanation and
 * a "View plans" action; rate limits (429) a calm retry hint.
 */
export function toastApiError(err: unknown, fallbackTitle?: string) {
  if (err instanceof ApiError && err.code === 'plan_expired') {
    toast.warning('This workspace is read-only', {
      id: 'plan-expired',
      description: sentence(err.message),
      duration: 10_000,
      action: { label: 'View plan', onClick: () => navigate(PLANS_HREF) },
    })

    return
  }

  if (err instanceof ApiError && err.isPlanLimit) {
    toast.warning('Plan limit reached', {
      id: 'plan-limit',
      description: sentence(describePlanLimit(err.planLimit, err.message)),
      duration: 10_000,
      action: { label: 'View plans', onClick: () => navigate(PLANS_HREF) },
    })

    return
  }

  if (err instanceof ApiError && err.isRateLimited) {
    toast.warning('Slow down a little', { id: 'rate-limited', description: errorMessage(err) })

    return
  }

  if (fallbackTitle) {
    toast.error(fallbackTitle, { description: errorMessage(err) })
  } else {
    toast.error(errorMessage(err))
  }
}

export const quoted = (title?: string) => `“${title?.trim() || 'Untitled'}”`

/** "“Name” was archived" style toast for anything with a name. */
export function namedToast(name: string | undefined, what: string, description?: string) {
  toast.success(`${quoted(name)} ${what}`, description ? { description } : undefined)
}

/** The same wording as an error toast: "“Name” could not be reached". */
export function namedErrorToast(name: string | undefined, what: string, description?: string) {
  toast.error(`${quoted(name)} ${what}`, description ? { description } : undefined)
}
