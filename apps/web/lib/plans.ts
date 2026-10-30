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
    tagline: 'For creators building a consistent publishing habit.',
    features: [
      '5 connected channels',
      '300 new posts a month',
      'Calendar, queues and recurring schedules',
      'Design, video and audio editors',
      'Publishing and performance analytics',
      '$1 shared AI allowance a month',
      '50 X credits a month',
      '100 LinkedIn posts a month, 5 with video',
      'MCP, REST API, SDKs and CLI',
      '2 GB storage, 25 MB per file',
      'Data export without files',
    ],
  },
  {
    id: 'plus',
    name: 'Plus',
    month: 29,
    year: 276,
    tagline: 'For daily publishing across brands and channels.',
    highlight: true,
    features: [
      '15 connected channels',
      '1,500 new posts a month',
      'Calendar, queues and recurring schedules',
      'Design, video and audio editors',
      'Publishing and performance analytics',
      '$2 shared AI allowance a month',
      '100 X credits a month',
      '300 LinkedIn posts a month, 15 with video',
      'MCP, REST API, SDKs and CLI',
      '10 GB storage, 100 MB per file',
      'Backups and full data export with files',
    ],
  },
  {
    id: 'pro',
    name: 'Pro',
    month: 59,
    year: 564,
    tagline: 'For teams automating a growing content operation.',
    features: [
      '40 connected channels',
      '5,000 new posts a month',
      'Calendar, queues and recurring schedules',
      'Design, video and audio editors',
      'Publishing and performance analytics',
      '$5 shared AI allowance a month',
      '250 X credits a month',
      '600 LinkedIn posts a month, 30 with video',
      'MCP, REST API, SDKs and CLI',
      '30 GB storage, 100 MB per file',
      'Backups and full data export with files',
    ],
  },
  {
    id: 'business',
    name: 'Business',
    month: 99,
    year: 948,
    tagline: 'For agencies managing a larger channel portfolio.',
    features: [
      '100 connected channels',
      '15,000 new posts a month',
      'Calendar, queues and recurring schedules',
      'Design, video and audio editors',
      'Publishing and performance analytics',
      '$10 shared AI allowance a month',
      '500 X credits a month',
      '900 LinkedIn posts a month, 60 with video',
      'MCP, REST API, SDKs and CLI',
      '100 GB storage, 100 MB per file',
      'Backups and full data export with files',
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
      {
        label: 'New posts per month',
        hint: 'Created posts, including drafts, duplicates and agent-created posts; UTC calendar month',
        values: ['300', '1,500', '5,000', '15,000'],
      },
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
        values: ['5', '15', '40', '100'],
      },
      {
        label: 'X credits per month',
        hint: 'One credit per post or reply; 14 with a clickable link. Shared across all X channels',
        values: ['50', '100', '250', '500'],
      },
      {
        label: 'Posts to LinkedIn included per month',
        hint: 'Profiles and Pages; comments are not counted',
        values: ['100', '300', '600', '900'],
      },
      {
        label: 'LinkedIn posts with a video per month',
        hint: 'Each also counts as a LinkedIn post',
        values: ['5', '15', '30', '60'],
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
      {
        label: 'Connect your own agent to refill collections',
        hint: 'Requires your agent or automation; its provider fees are separate',
        values: [true, true, true, true],
      },
      {
        label: 'Alerts',
        hint: 'Telegram, Slack, Discord, ntfy or email',
        values: ['3', '10', '25', '50'],
      },
    ],
  },
  {
    category: 'API and integrations',
    rows: [
      { label: 'REST API, TypeScript and Python SDKs, CLI', values: [true, true, true, true] },
      { label: 'Signed webhooks', values: ['3', '10', '25', '50'] },
      { label: 'API keys', hint: 'Each with its own scopes', values: ['2', '10', '25', '50'] },
    ],
  },
  {
    category: 'Storage and data',
    rows: [
      { label: 'Media storage', values: ['2 GB', '10 GB', '30 GB', '100 GB'] },
      { label: 'File size', hint: 'Per upload', values: ['25 MB', '100 MB', '100 MB', '100 MB'] },
      { label: 'Full history of every post', values: [true, true, true, true] },
      {
        label: 'Backups and data export with files',
        hint: 'Every plan can export its data without the files',
        values: [false, true, true, true],
      },
    ],
  },
  {
    category: 'Analytics',
    rows: [
      { label: 'Publishing, channel and automation analytics', values: [true, true, true, true] },
      {
        label: 'Top posts and posting-time insights',
        hint: 'Based on collected metrics; availability varies by platform',
        values: [true, true, true, true],
      },
      {
        label: 'Fresh X statistics reads per month',
        hint: 'Shared workspace limit; viewing cached analytics does not use it',
        values: ['100', '200', '500', '1,000'],
      },
    ],
  },
  {
    category: 'Create and edit',
    rows: [
      { label: 'Design editor: templates, text and photos', values: [true, true, true, true] },
      { label: 'Video and audio timeline editors', values: [true, true, true, true] },
      { label: 'On-device background removal', values: [true, true, true, true] },
      {
        label: 'AI assistant, images and video',
        hint: 'Available tools depend on the configured providers; all draw from one allowance',
        values: [true, true, true, true],
      },
      {
        label: 'Shared AI allowance per month',
        hint: 'Estimated provider cost in USD. Resets monthly; no rollover or automatic overage charges',
        values: ['$1', '$2', '$5', '$10'],
      },
    ],
  },
  {
    category: 'Coming soon — not available yet',
    rows: [
      {
        label: 'AI performance summaries and content recommendations',
        hint: 'Planned for Plus and above; intended to use the same AI allowance',
        values: [false, 'Planned', 'Planned', 'Planned'],
      },
      { label: 'Scheduled performance reports', values: [false, false, 'Planned', 'Planned'] },
      { label: 'Client-ready report exports', values: [false, false, false, 'Planned'] },
    ],
  },
]

export const FAQ: [string, string][] = [
  [
    'Are the limits per user or per workspace?',
    'Prices are per user; channels, new posts, storage, X credits, LinkedIn allowances and AI are shared per workspace. Extra seats add collaborators, not extra usage. Monthly allowances reset on the 1st in UTC, including on annual subscriptions, and do not roll over.',
  ],
  [
    'How many AI images or videos are included?',
    'AI uses one shared monthly allowance: $1 on Starter, $2 on Plus, $5 on Pro and $10 on Business, measured at estimated provider cost in USD. Text, images and video use different amounts, so there is no fixed image or video count. New requests stop when the remaining allowance cannot cover them. Upgrade or wait for the monthly reset; we do not automatically bill overages. Manual editing stays available.',
  ],
  [
    'Are AI reports available now?',
    'Publishing analytics, top posts and posting-time insights are available today. Automatic AI insights, scheduled reports and client-ready exports are Coming soon. The proposed plan allocation may change before release; choose a plan for the features available today.',
  ],
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
    "Every person in the workspace pays the plan's price, whatever their role: a team of three on Plus pays 3 × €29 = €87 a month. Inviting someone adds a seat once they accept, removing someone takes it away, and both are prorated for the rest of the period. During the Starter trial seats are free. Agents and API keys do not add seats. All usage limits are shared by the workspace and do not multiply with seats; external agent subscriptions are separate.",
  ],
  [
    'Why are posts to X counted?',
    'X charges for each API post, so each plan includes X credits a month (thread replies count too; a post with a clickable link counts 14, as X charges about 14 times more for it). Need more? Extra posts come in one-time bundles: 200 for €10, 1,000 for €40 or 4,000 for €150. They are used after the included ones and expire at the end of the billing period.',
  ],
  [
    'Why are posts to LinkedIn counted?',
    'LinkedIn has shared app quotas as well as account limits. Your monthly allowance covers profile and Page posts; videos also count towards the video allowance. Daily safeguards protect shared capacity, so a monthly allowance is not a promise that it can all be published in one day. Comments are not counted as monthly posts, but still use API capacity.',
  ],
  [
    'Is the Community Edition really free?',
    'Yes. Staqly is open source (AGPL-3.0). Run staqly Community Edition on your own machine or server with no staqly plan limits. Hosting, backups, updates and any paid AI or social APIs are your responsibility; provider limits still apply.',
  ],
]
