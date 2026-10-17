import type { Plan, WorkspaceUsage } from '@/lib/api'

const GB = 1 << 30

export const PLANS: Plan[] = [
  {
    id: 'free',
    name: 'Free',
    price_usd_month: 0,
    highlight: false,
    limits: {
      items_per_month: -1,
      decisions_per_month: 50,
      webhooks: 3,
      api_keys: 1,
      storage_bytes: GB,
      max_upload_bytes: 5 << 20,
      watchers: 3,
      min_watcher_interval_seconds: 900,
      notification_channels: 2,
      data_export: false,
    },
    features: [
      '50 approvals / month',
      'Unlimited items and collections',
      '3 webhooks',
      '1 API key',
      '1 GB storage',
      'No card, no email',
    ],
  },
  {
    id: 'pro',
    name: 'Pro',
    price_usd_month: 30,
    highlight: true,
    limits: {
      items_per_month: -1,
      decisions_per_month: -1,
      webhooks: -1,
      api_keys: -1,
      storage_bytes: 100 * GB,
      max_upload_bytes: 100 << 20,
      watchers: -1,
      min_watcher_interval_seconds: 60,
      notification_channels: -1,
      data_export: true,
    },
    features: [
      'Unlimited approvals',
      'Unlimited items and collections',
      'Unlimited webhooks',
      'Unlimited API keys',
      '100 GB storage',
    ],
  },
]

export const UNLOCKED: Plan = {
  id: 'unlocked',
  name: 'Unlocked',
  price_usd_month: 0,
  highlight: false,
  limits: {
    items_per_month: -1,
    decisions_per_month: -1,
    webhooks: -1,
    api_keys: -1,
    storage_bytes: -1,
    max_upload_bytes: -1,
    watchers: -1,
    min_watcher_interval_seconds: 60,
    notification_channels: -1,
    data_export: true,
  },
  features: ['Local install', 'No limits'],
}

export function usage(plan: Plan, u: Partial<WorkspaceUsage['usage']> = {}): WorkspaceUsage {
  return {
    plan,
    period: '2026-10',
    usage: {
      items_per_month: 0,
      decisions_per_month: 0,
      webhooks: 0,
      api_keys: 0,
      storage_bytes: 0,
      watchers: 0,
      notification_channels: 0,
      ...u,
    },
    plan_expires_at: null,
    suspended: false,
    items_by_status: { pending: 2, approved: 1 },
    webhook_deliveries: { queued: 0, retrying: 0, failed: 1 },
  }
}
