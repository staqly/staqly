// The plans as sold, per user. Source of truth: apps/api/internal/app/plans.go (limits and prices); keep this
// file in step with it. Yearly = twelve whole-euro months at 20 % off or a little more (rounded down).
// Every member of a workspace is a seat and pays the plan's price, whatever their role.

export type Interval = 'month' | 'year'

export type SitePlan = {
  id: 'starter' | 'plus' | 'pro' | 'business'
  name: string
  month: number
  year: number
  tagline: string
  highlight?: boolean
  features: string[]
}

/** Only Starter has a free trial; Plus, Pro and Business are paid from the first day. */
export const TRIAL_DAYS = 7
export const TRIAL_PLAN = 'starter'

/** The button on a plan: the trial for Starter, buying for the others. */
export function ctaLabel(p: { id: string; name: string }) {
  return p.id === TRIAL_PLAN ? 'Start free trial' : `Get ${p.name}`
}

export const PLANS: SitePlan[] = [
  {
    id: 'starter',
    name: 'Starter',
    month: 15,
    year: 144,
    tagline: 'For one creator and a few accounts.',
    features: [
      '5 channels',
      'Per user: invite as many people as you need',
      'Unlimited posts, collections and schedules',
      '250 posts to X a month included',
      '300 LinkedIn posts a month, 30 with video',
      'MCP, REST API, SDKs and CLI',
      '3 alerts, 3 webhooks, 2 API keys',
      '10 GB storage, 25 MB per file',
      'Data export without files',
    ],
  },
  {
    id: 'plus',
    name: 'Plus',
    month: 29,
    year: 276,
    tagline: 'For a creator or a small team posting every day.',
    highlight: true,
    features: [
      '15 channels',
      'Per user: invite as many people as you need',
      'Unlimited posts, collections and schedules',
      '450 posts to X a month included',
      '600 LinkedIn posts a month, 60 with video',
      'MCP, REST API, SDKs and CLI',
      '10 alerts, 10 webhooks, 10 API keys',
      '50 GB storage, 100 MB per file',
      'Backups and full data export',
    ],
  },
  {
    id: 'pro',
    name: 'Pro',
    month: 59,
    year: 564,
    tagline: 'For teams and agents running many channels.',
    features: [
      '40 channels',
      'Per user: invite as many people as you need',
      'Unlimited posts, collections and schedules',
      '950 posts to X a month included',
      '1,200 LinkedIn posts a month, 150 with video',
      'MCP, REST API, SDKs and CLI',
      'Unlimited alerts, webhooks and API keys',
      '200 GB storage, 250 MB per file',
      'Backups and full data export',
    ],
  },
  {
    id: 'business',
    name: 'Business',
    month: 99,
    year: 948,
    tagline: 'For agencies and brands with many accounts.',
    features: [
      'Unlimited channels',
      'Per user: invite as many people as you need',
      'Unlimited posts, collections and schedules',
      '1,600 posts to X a month included',
      '2,500 LinkedIn posts a month, 300 with video',
      'MCP, REST API, SDKs and CLI',
      'Unlimited alerts, webhooks and API keys',
      '1 TB storage, 1 GB per file',
      'Backups and full data export',
      'Priority support',
    ],
  },
]

/**
 * Extra posts to X, bought once (the Stripe catalog and apps/api credits.go must match): used after the
 * plan's included ones, expiring at the end of the billing period.
 */
export const X_BUNDLES: { posts: number; price: number }[] = [
  { posts: 200, price: 10 },
  { posts: 1000, price: 40 },
  { posts: 4000, price: 150 },
]

/** €12, €15: prices are whole euros (others with two decimals). */
export function eur(n: number) {
  return Number.isInteger(n) ? `€${n}` : `€${n.toFixed(2)}`
}

/** Prices are for businesses, without VAT (added at checkout where it applies). */
export const VAT_NOTE = 'excl. VAT'

/** The price per month: billed monthly, or the yearly price spread over twelve months. */
export function perMonth(p: SitePlan, interval: Interval) {
  return interval === 'year' ? p.year / 12 : p.month
}

/** The cheapest price per month, billed yearly ("from €12 a month"). */
export const FROM = Math.min(...PLANS.map((p) => perMonth(p, 'year')))

export type Cell = string | boolean

export type CompareRow = { label: string; hint?: string; values: [Cell, Cell, Cell, Cell] }

/** The comparison table: a value per plan, in PLANS order. */
export const COMPARE: { category: string; rows: CompareRow[] }[] = [
  {
    category: 'Publishing',
    rows: [
      { label: 'Posts per month', values: ['Unlimited', 'Unlimited', 'Unlimited', 'Unlimited'] },
      { label: 'Collections and schedules', values: ['Unlimited', 'Unlimited', 'Unlimited', 'Unlimited'] },
      { label: 'Calendar, comments and per-channel text', values: [true, true, true, true] },
      { label: 'Retries and a visible job queue', values: [true, true, true, true] },
    ],
  },
  {
    category: 'Channels',
    rows: [
      {
        label: 'Connected channels',
        hint: 'Each connected account counts once, whatever the platform, your own automation included',
        values: ['5', '15', '40', 'Unlimited'],
      },
      {
        label: 'Posts to X included per month',
        hint: 'Each post and thread reply; X charges for every post, so extra ones come in bundles',
        values: ['250', '450', '950', '1,600'],
      },
      {
        label: 'Posts to LinkedIn included per month',
        hint: 'Profiles and Pages; comments are not counted',
        values: ['300', '600', '1,200', '2,500'],
      },
      {
        label: 'LinkedIn posts with a video per month',
        hint: 'Each also counts as a LinkedIn post',
        values: ['30', '60', '150', '300'],
      },
      { label: 'Your own automation as a channel', values: [true, true, true, true] },
      { label: 'Link handling per channel', values: [true, true, true, true] },
    ],
  },
  {
    category: 'Team',
    rows: [
      {
        label: 'People',
        hint: 'Each member is billed per user, whatever the role; agents are free',
        values: ['Per user', 'Per user', 'Per user', 'Per user'],
      },
      { label: 'Roles: owner, admin, editor, viewer', values: [true, true, true, true] },
      { label: 'Sign-in with Google, email code or a passkey', values: [true, true, true, true] },
    ],
  },
  {
    category: 'Automation and agents',
    rows: [
      { label: 'MCP server for Claude Code, Codex and others', values: [true, true, true, true] },
      { label: 'Agents refill a collection when it runs dry', values: [true, true, true, true] },
      {
        label: 'Alerts',
        hint: 'Telegram, Slack, Discord, ntfy or email',
        values: ['3', '10', 'Unlimited', 'Unlimited'],
      },
    ],
  },
  {
    category: 'API and integrations',
    rows: [
      { label: 'REST API, TypeScript and Python SDKs, CLI', values: [true, true, true, true] },
      { label: 'Signed webhooks', values: ['3', '10', 'Unlimited', 'Unlimited'] },
      { label: 'API keys', hint: 'Each with its own scopes', values: ['2', '10', 'Unlimited', 'Unlimited'] },
    ],
  },
  {
    category: 'Storage and data',
    rows: [
      { label: 'Media storage', values: ['10 GB', '50 GB', '200 GB', '1 TB'] },
      { label: 'File size', hint: 'Per upload', values: ['25 MB', '100 MB', '250 MB', '1 GB'] },
      { label: 'Full history of every post', values: [true, true, true, true] },
      {
        label: 'Backups and data export with files',
        hint: 'Every plan can export its data without the files',
        values: [false, true, true, true],
      },
    ],
  },
  {
    category: 'Support',
    rows: [
      { label: 'Documentation and email support', values: [true, true, true, true] },
      { label: 'Priority support', values: [false, false, false, true] },
    ],
  },
]

export const FAQ: [string, string][] = [
  [
    'Which plans have a free trial?',
    `Starter: ${TRIAL_DAYS} days free. You add a card when you start; nothing is charged until the trial ends, and you can cancel anytime before. Plus, Pro and Business are paid from the first day. Prices are per user and exclude VAT.`,
  ],
  [
    'What happens when the trial ends?',
    'If no plan is paid for, the workspace becomes read-only: you can still read and export everything, nothing is deleted, and choosing a plan turns it back on.',
  ],
  [
    'How much does yearly billing save?',
    'Yearly billing saves 20 % or a little more on twelve monthly payments, per user: Starter is €12 a month (€144 a year), Plus €23 (€276), Pro €47 (€564) and Business €79 (€948).',
  ],
  [
    'Can I change plans or cancel?',
    'Yes, any time from Settings → Plan & usage. Switching plans or between monthly and yearly is prorated: you are charged or credited the difference. Cancelling keeps the plan until the end of the paid period.',
  ],
  [
    'How does per user pricing work?',
    "Every person in the workspace pays the plan's price, whatever their role: a team of three on Plus pays 3 × €29 = €87 a month. Inviting someone adds a seat once they accept, removing someone takes it away, and both are prorated for the rest of the period. During the Starter trial seats are free. Agents and API keys are not people and cost nothing.",
  ],
  [
    'Why are posts to X counted?',
    'X charges for every post sent through its API, so each plan includes a number of posts to X a month (thread replies count too; a post with a clickable link counts 14, as X charges about 14 times more for it). Need more? Extra posts come in one-time bundles: 200 for €10, 1,000 for €40 or 4,000 for €150. They are used after the included ones and expire at the end of the billing period.',
  ],
  [
    'Why are posts to LinkedIn counted?',
    "LinkedIn limits how many posts and video uploads all of Staqly's workspaces together can send each day, so each plan includes a fair share a month: 300, 600, 1,200 or 2,500 posts to LinkedIn profiles and Pages, of which 30, 60, 150 or 300 can carry a video. Comments are not counted. Other platforms are not counted at all.",
  ],
  [
    'Is the Community Edition really free?',
    'Yes. Staqly is open source (AGPL-3.0). Run Staqly Community Edition on your own machine or server with no plan limits; you take care of backups and updates yourself.',
  ],
]
