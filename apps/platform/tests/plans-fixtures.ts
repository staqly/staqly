import type { Plan, WorkspaceUsage } from '@/lib/api'

const GB = 1 << 30

const MB = 1 << 20

// The plans as the API sells them (apps/api/internal/app/plans.go).
export const PLANS: Plan[] = [
  {
    id: 'starter',
    name: 'Starter',
    price_month: 15,
    price_year: 144,
    currency: 'eur',
    highlight: false,
    limits: {
      posts_per_month: -1,
      channels: 5,
      alerts: 3,
      webhooks: 3,
      api_keys: 2,
      storage_bytes: 10 * GB,
      max_upload_bytes: 25 * MB,
      data_export: false,
      x_posts_per_period: 250,
    },
    features: ['5 channels', '1 seat', '3 alerts, 3 webhooks, 2 API keys', '10 GB storage, 25 MB per file'],
  },
  {
    id: 'plus',
    name: 'Plus',
    price_month: 29,
    price_year: 276,
    currency: 'eur',
    highlight: true,
    limits: {
      posts_per_month: -1,
      channels: 15,
      alerts: 10,
      webhooks: 10,
      api_keys: 10,
      storage_bytes: 50 * GB,
      max_upload_bytes: 100 * MB,
      data_export: true,
      x_posts_per_period: 450,
    },
    features: ['15 channels', '3 seats', 'Backups and full data export with files'],
  },
  {
    id: 'pro',
    name: 'Pro',
    price_month: 59,
    price_year: 564,
    currency: 'eur',
    highlight: false,
    limits: {
      posts_per_month: -1,
      channels: 40,
      alerts: -1,
      webhooks: -1,
      api_keys: -1,
      storage_bytes: 200 * GB,
      max_upload_bytes: 250 * MB,
      data_export: true,
      x_posts_per_period: 950,
    },
    features: ['40 channels', '10 seats', 'Unlimited alerts, webhooks and API keys'],
  },
  {
    id: 'business',
    name: 'Business',
    price_month: 99,
    price_year: 948,
    currency: 'eur',
    highlight: false,
    limits: {
      posts_per_month: -1,
      channels: -1,
      alerts: -1,
      webhooks: -1,
      api_keys: -1,
      storage_bytes: 1024 * GB,
      max_upload_bytes: 1024 * MB,
      data_export: true,
      x_posts_per_period: 1600,
    },
    features: ['Unlimited channels', 'Unlimited seats', 'Priority support'],
  },
]

export const UNLOCKED: Plan = {
  id: 'unlocked',
  name: 'Community Edition',
  price_month: 0,
  price_year: 0,
  currency: 'eur',
  highlight: false,
  limits: {
    posts_per_month: -1,
    channels: -1,
    alerts: -1,
    webhooks: -1,
    api_keys: -1,
    storage_bytes: -1,
    max_upload_bytes: -1,
    data_export: true,
    x_posts_per_period: -1,
  },
  features: ['Self-hosted', 'No plan limits'],
}

export function usage(plan: Plan, u: Partial<WorkspaceUsage['usage']> = {}): WorkspaceUsage {
  return {
    plan,
    period: '2026-10',
    usage: {
      posts_per_month: 0,
      channels: 0,
      seats: 0,
      x_posts_per_period: 0,
      alerts: 0,
      webhooks: 0,
      api_keys: 0,
      storage_bytes: 0,
      ...u,
    },
    plan_expires_at: null,
    trial_ends_at: null,
    suspended: false,
    posts_by_status: { scheduled: 2, published: 1 },
    webhook_deliveries: { queued: 0, retrying: 0, failed: 1 },
  }
}
