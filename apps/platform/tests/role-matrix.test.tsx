import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { cleanup, screen, waitFor } from '@testing-library/react'
import type { ReactElement } from 'react'
import { describe, expect, it } from 'vitest'

import { IntegrationsPage } from '@/components/integrations/pages'
import { MediaPage } from '@/components/media/media-page'
import { PlansPage } from '@/components/plans/plans-page'
import { PostActions } from '@/components/posts/post-actions'
import { BackupCard } from '@/components/settings/backup-card'
import { DataPrivacyPage } from '@/components/settings/data-privacy-page'
import { MembersPage } from '@/components/settings/members-page'
import { PlanUsagePage } from '@/components/settings/plan-usage-page'
import { SettingsNav } from '@/components/settings/settings-nav'
import { SettingsPage } from '@/components/settings/settings-page'
import { AppShell } from '@/components/shell/app-shell'
import type { Addon, Billing, BillingDetails, Member, Role, XPosts } from '@/lib/api'

import { post, target } from './fixtures'
import { nav } from './navigation'
import { PLANS, usage } from './plans-fixtures'
import { mockApi, renderWithProviders, ROLE_SCOPES } from './utils'

/**
 * Who sees what. The server decides what a role may do (apps/api/internal/app/accounts.go `RoleScopes`, and owners
 * alone pay and delete); the app hides what a role cannot do, so nobody is offered a button that answers 403.
 * One line per action, with the roles that get it: a new role-dependent action gets a line here.
 *
 * The locked screen and the failed-payment strip are role-dependent too: tests/blocked-workspace.test.tsx.
 */
const ROLES: Role[] = ['owner', 'admin', 'operator', 'editor', 'viewer']
const NOBODY: Role[] = []
const OWNERS: Role[] = ['owner']
const MANAGERS: Role[] = ['owner', 'admin']
const OPERATORS: Role[] = ['owner', 'admin', 'operator']
const WRITERS: Role[] = ['owner', 'admin', 'operator', 'editor']

const MATRIX = {
  // Settings
  'Settings: the Members section': MANAGERS,
  'Settings: the Backups section': MANAGERS,
  'Settings: the Account section': ROLES,
  'Settings: rename the workspace and set its logo and time zone': MANAGERS,
  'Settings: the devices signed in': MANAGERS,
  // People in an account recover access with their sign-in methods: the workspace recovery key is for none of them.
  'Settings: the recovery key': NOBODY,
  'Backups: make and download backups': MANAGERS,
  'Members: the people and their roles, invitations': MANAGERS,
  // Money
  'Plan & usage: the plan and the usage': ROLES,
  'Plan & usage: Change plan': MANAGERS,
  'Plan & usage: payment method and invoices': MANAGERS,
  'Plan & usage: Manage billing (Stripe)': OWNERS,
  'Plan & usage: buy add-ons': OWNERS,
  'Plan & usage: buy more X posts': OWNERS,
  'Plans: choose or switch a plan': OWNERS,
  // Data
  'Data & privacy: export the activity log': ROLES,
  'Data & privacy: export the workspace': MANAGERS,
  'Data & privacy: delete the workspace': OWNERS,
  // Automation
  'Sidebar: Alerts': OPERATORS,
  'Sidebar: Webhooks': OPERATORS,
  'Integrations: the connected agents and their keys': OPERATORS,
  // Content
  'Post: Duplicate': ROLES,
  'Post: Unpublish': WRITERS,
  'Media: upload, new folder': WRITERS,
} satisfies Record<string, Role[]>

type Action = keyof typeof MATRIX

interface Surface {
  name: string
  path: string
  routes: Record<string, unknown>
  ui: ReactElement
  /** Resolves once the page shows what it has for this role. */
  ready: () => Promise<unknown>
  /** What is on the screen, per action. */
  seen: () => Partial<Record<Action, boolean>>
}

const shown = (el: Element | null) => el !== null
const link = (name: string | RegExp) => shown(screen.queryByRole('link', { name }))
const button = (name: string | RegExp) => shown(screen.queryByRole('button', { name }))

const PLUS = PLANS[1]!

const BILLING: Billing = {
  enabled: true,
  plan: 'plus',
  subscription_status: 'active',
  plan_expires_at: '2026-12-01T10:00:00Z',
  trial_ends_at: null,
  suspended: false,
  has_customer: true,
  seats: 3,
  trial: 'none',
  trial_review: null,
  trial_revoked: null,
  ended_for: null,
}

const DETAILS: BillingDetails = {
  enabled: true,
  subscription: {
    status: 'active',
    current_period_start: '2026-11-01T10:00:00Z',
    current_period_end: '2026-12-01T10:00:00Z',
    cancel_at_period_end: false,
    cancel_at: null,
    canceled_at: null,
    trial_end: null,
    amount: 2900,
    quantity: 3,
    currency: 'eur',
    interval: 'month',
    past_due_for: '',
    discount: null,
  },
  payment_methods: [{ brand: 'visa', last4: '4242', exp_month: 4, exp_year: 2030, default: true }],
  invoices: [],
  has_more_invoices: false,
  managed_payments: false,
}

const X_POSTS: XPosts = {
  period: '2026-11',
  period_end: '2026-12-01T00:00:00Z',
  unlimited: false,
  included: 100,
  used: 40,
  included_left: 60,
  credits_left: 0,
  credits_expire_at: null,
  left: 60,
  bundles: [{ posts: 200, price: 10 }],
  currency: 'eur',
  link_post_weight: 14,
}

const PACK: Addon = {
  id: 'channels_pack',
  name: 'Channels pack',
  description: '10 more channels',
  features: [],
  price_month: 9,
  price_year: 86,
  currency: 'eur',
  active: false,
}

const ME: Member = {
  user_id: 'usr_me',
  name: 'Ana Example',
  email: 'ana@example.com',
  avatar_url: '',
  role: 'owner',
  you: true,
  created_at: '2026-10-19T00:00:00Z',
}

const MONEY = {
  'GET /v1/plans': { data: PLANS },
  'GET /v1/workspace/usage': usage(PLUS, { x_posts_per_period: 40 }),
  'GET /v1/billing': BILLING,
  'GET /v1/billing/details': DETAILS,
  'GET /v1/billing/addons': { data: [PACK] },
  'GET /v1/billing/x-posts': X_POSTS,
  'GET /v1/billing/pending-payment': { pending: false },
}

const SURFACES: Surface[] = [
  {
    name: 'the sections of Settings',
    path: '/settings',
    routes: {},
    ui: <SettingsNav />,
    ready: () => screen.findByRole('link', { name: 'General' }),
    seen: () => ({
      'Settings: the Members section': link('Members'),
      'Settings: the Backups section': link('Backups'),
      'Settings: the Account section': link('Account'),
    }),
  },
  {
    name: 'Settings, General',
    path: '/settings',
    routes: { 'GET /v1/sessions': { data: [] }, 'GET /v1/workspace': { id: 'ws_1', name: 'Test WS' } },
    ui: <SettingsPage />,
    ready: () => screen.findByText('Appearance'),
    seen: () => ({
      'Settings: rename the workspace and set its logo and time zone': shown(screen.queryByText('Workspace')),
      'Settings: the devices signed in': shown(screen.queryByText('Devices')),
      'Settings: the recovery key': shown(screen.queryByText('Recovery key')),
    }),
  },
  {
    name: 'Settings, Backups',
    path: '/settings/backups',
    routes: { ...MONEY, 'GET /v1/workspace/exports': { data: [] } },
    ui: <BackupCard />,
    ready: () =>
      waitFor(() =>
        expect(
          screen.queryByRole('button', { name: /create backup/i }) ?? screen.getByTestId('backups-admins-only'),
        ).toBeInTheDocument(),
      ),
    seen: () => ({ 'Backups: make and download backups': button(/create backup/i) }),
  },
  {
    name: 'Settings, Members',
    path: '/settings/members',
    routes: { ...MONEY, 'GET /v1/members': { data: [ME] }, 'GET /v1/invitations': { data: [] } },
    ui: <MembersPage />,
    ready: () =>
      waitFor(() =>
        expect(
          screen.queryByTestId('members') ?? screen.getByText('Only admins and owners see and manage the people here.'),
        ).toBeInTheDocument(),
      ),
    seen: () => ({
      'Members: the people and their roles, invitations':
        shown(screen.queryByTestId('members')) && button('Invite people'),
    }),
  },
  {
    name: 'Settings, Plan & usage',
    path: '/settings/plan',
    routes: MONEY,
    ui: <PlanUsagePage />,
    ready: async () => {
      await screen.findByTestId('x-posts')
      await waitFor(() =>
        expect(
          screen.queryByTestId('payment-methods') ??
            screen.getByText('Owners and admins of this workspace change the plan.'),
        ).toBeInTheDocument(),
      )
    },
    seen: () => ({
      'Plan & usage: the plan and the usage': shown(screen.queryByRole('heading', { level: 2, name: 'Plus plan' })),
      'Plan & usage: Change plan': link('Change plan'),
      'Plan & usage: payment method and invoices': shown(screen.queryByTestId('payments')),
      'Plan & usage: Manage billing (Stripe)': button(/Manage billing/),
      'Plan & usage: buy add-ons': button(/^Add/),
      'Plan & usage: buy more X posts': button('Buy more X posts'),
    }),
  },
  {
    name: 'the plans',
    path: '/plans',
    routes: MONEY,
    ui: <PlansPage />,
    ready: () => screen.findByTestId('plan-pro'),
    seen: () => ({
      'Plans: choose or switch a plan': shown(
        screen.queryAllByRole('button', { name: 'Upgrade to Pro' }).find((b) => !(b as HTMLButtonElement).disabled) ??
          null,
      ),
    }),
  },
  {
    name: 'Settings, Data & privacy',
    path: '/settings/data',
    routes: MONEY,
    ui: <DataPrivacyPage />,
    ready: () => screen.findByTestId('export-all-csv'),
    seen: () => ({
      'Data & privacy: export the activity log': link('Download CSV'),
      'Data & privacy: export the workspace': button('Export workspace'),
      'Data & privacy: delete the workspace': button('Delete workspace...'),
    }),
  },
  {
    name: 'the sidebar',
    path: '/schedule',
    routes: MONEY,
    ui: (
      <AppShell>
        <p>the page</p>
      </AppShell>
    ),
    ready: () => screen.findByRole('link', { name: 'Integrations' }),
    seen: () => ({
      'Sidebar: Alerts': link('Alerts'),
      'Sidebar: Webhooks': link('Webhooks'),
    }),
  },
  {
    name: 'Integrations, Agents & activity',
    path: '/integrations?view=agents',
    routes: { 'GET /v1/agents': { data: [] }, 'GET /v1/events': { data: [] }, 'GET /v1/api-keys': { data: [] } },
    ui: <IntegrationsPage />,
    ready: () => screen.findByTestId('agents-tab'),
    seen: () => ({ 'Integrations: the connected agents and their keys': shown(screen.queryByTestId('agents')) }),
  },
  {
    name: 'a post that went out',
    path: '/posts',
    routes: { 'GET /v1/posts/pst_1/comments': { data: [] } },
    ui: (
      <PostActions
        post={post({ id: 'pst_1', status: 'published', targets: [target({ status: 'published' })] })}
        onDeleted={() => {}}
      />
    ),
    ready: () => screen.findByRole('button', { name: 'Duplicate' }),
    seen: () => ({ 'Post: Duplicate': button('Duplicate'), 'Post: Unpublish': button('Unpublish') }),
  },
  {
    name: 'the media library',
    path: '/media',
    routes: {
      ...MONEY,
      'GET /v1/media': { data: [] },
      'GET /v1/media/folders': { data: [], root_files: 0, total_files: 0, tags: [] },
    },
    ui: <MediaPage />,
    ready: () => screen.findByText('No media yet'),
    seen: () => ({ 'Media: upload, new folder': shown(screen.queryByTestId('media-upload-input')) }),
  },
]

describe('what each role is offered', () => {
  describe.each(SURFACES)('$name', (surface) => {
    it.each(ROLES)('%s', async (role) => {
      const [path, query] = surface.path.split('?')

      nav.set(path!, query)
      mockApi(surface.routes)
      renderWithProviders(surface.ui, { role })
      await surface.ready()

      const seen = surface.seen()
      const expected = Object.fromEntries(
        (Object.keys(seen) as Action[]).map((action) => [action, MATRIX[action].includes(role)]),
      )

      expect(seen).toEqual(expected)
      cleanup()
    })
  })

  it('every line of the matrix is looked for on some page', () => {
    // The probes read the screen, so each surface is rendered once: any role shows which lines it answers.
    const probed = new Set<string>()

    for (const surface of SURFACES) {
      mockApi({})
      renderWithProviders(surface.ui, { role: 'viewer' })
      Object.keys(surface.seen()).forEach((action) => probed.add(action))
      cleanup()
    }

    expect([...probed].toSorted()).toEqual(Object.keys(MATRIX).toSorted())
  })
})

describe('the scopes of a role', () => {
  it("are the API's: the test sessions hold what RoleScopes gives each role", () => {
    // Vitest runs in apps/platform.
    const source = readFileSync(resolve(process.cwd(), '../api/internal/app/accounts.go'), 'utf8')
    const table = /var RoleScopes = map\[string\]\[\]string\{([\s\S]*?)\n\}/.exec(source)?.[1] ?? ''
    // RoleEditor: {ScopeCollectionsRead, ...} → editor: ['collections:read', ...]
    const api = Object.fromEntries(
      [...table.matchAll(/Role(\w+):\s*\{([^}]*)\}/g)].map(([, role, scopes]) => [
        role!.toLowerCase(),
        [...scopes!.matchAll(/Scope([A-Z][a-z]+)([A-Z][a-z]+)?/g)]
          .map(([, what, verb]) => (verb ? `${what!.toLowerCase()}:${verb.toLowerCase()}` : what!.toLowerCase()))
          .toSorted(),
      ]),
    )

    expect(Object.keys(api).toSorted()).toEqual([...ROLES].toSorted())

    for (const role of ROLES) {
      // The session of an admin or owner lists every scope (currentSession in internal/httpapi/handlers.go).
      const held = api[role]!.includes('admin') ? ROLE_SCOPES.owner.toSorted() : api[role]

      expect({ role, scopes: [...ROLE_SCOPES[role]].toSorted() }).toEqual({ role, scopes: held })
    }
  })
})
