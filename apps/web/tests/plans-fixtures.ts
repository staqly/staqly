import type { Plan, WorkspaceUsage } from '@/lib/api'

const GB = 1 << 30

export const PLANS: Plan[] = [
  {
    id: 'free',
    name: 'Free',
    price_usd_month: 0,
    highlight: false,
    limits: {
      items_per_month: 100,
      webhooks: 3,
      api_keys: 3,
      storage_bytes: GB,
      max_upload_bytes: 25 << 20,
      rules: 5,
      watchers: 3,
      min_watcher_interval_seconds: 900,
      notification_channels: 2,
    },
    features: ['100 items / month', '3 webhooks', '3 API keys', '1 GB storage', 'No card, no email'],
  },
  {
    id: 'indie',
    name: 'Indie',
    price_usd_month: 9,
    highlight: true,
    limits: {
      items_per_month: 2000,
      webhooks: 20,
      api_keys: 20,
      storage_bytes: 20 * GB,
      max_upload_bytes: 100 << 20,
      rules: 50,
      watchers: 20,
      min_watcher_interval_seconds: 300,
      notification_channels: 10,
    },
    features: ['2,000 items / month', '20 webhooks', '20 API keys', '20 GB storage'],
  },
  {
    id: 'team',
    name: 'Team',
    price_usd_month: 29,
    highlight: false,
    limits: {
      items_per_month: -1,
      webhooks: -1,
      api_keys: -1,
      storage_bytes: 200 * GB,
      max_upload_bytes: 500 << 20,
      rules: -1,
      watchers: -1,
      min_watcher_interval_seconds: 60,
      notification_channels: -1,
    },
    features: ['Unlimited items', 'Unlimited webhooks', 'Unlimited API keys', '200 GB storage'],
  },
]

export const UNLOCKED: Plan = {
  id: 'unlocked',
  name: 'Unlocked',
  price_usd_month: 0,
  highlight: false,
  limits: {
    items_per_month: -1,
    webhooks: -1,
    api_keys: -1,
    storage_bytes: -1,
    max_upload_bytes: -1,
    rules: -1,
    watchers: -1,
    min_watcher_interval_seconds: 60,
    notification_channels: -1,
  },
  features: ['Local install', 'No limits'],
}

export function usage(plan: Plan, u: Partial<WorkspaceUsage['usage']> = {}): WorkspaceUsage {
  return {
    plan,
    period: '2026-10',
    usage: {
      items_per_month: 0,
      webhooks: 0,
      api_keys: 0,
      storage_bytes: 0,
      rules: 0,
      watchers: 0,
      notification_channels: 0,
      ...u,
    },
  }
}
