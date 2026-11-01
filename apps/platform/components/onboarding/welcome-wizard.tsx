'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  CalendarClock,
  Check,
  Loader2,
  PartyPopper,
  PenLine,
  Sparkles,
  Users,
} from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useRef, useState, type ReactNode } from 'react'

import { PublicShell } from '@/components/auth/public-shell'
import { ConnectForm } from '@/components/channels/add-channel-sheet'
import { ProviderGrid } from '@/components/channels/provider-grid'
import { Callout } from '@/components/common/callout'
import { CodeBlock } from '@/components/common/code-block'
import { NativeSelect } from '@/components/common/native-select'
import { SecretField } from '@/components/common/secret-field'
import { SkeletonList } from '@/components/common/skeleton-list'
import { IntervalToggle, monthly, money, TAGLINES } from '@/components/plans/plan-cards'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, errorMessage, type BillingInterval, type Plan, type Role } from '@/lib/api'
import { usePublicApiUrl } from '@/lib/config'
import { formatBytes } from '@/lib/format'
import { ProviderTile } from '@/lib/providers'
import { qk, useBilling, useChannels, useMode, usePlans, useProviders, useRole, useSession } from '@/lib/queries'
import { ROLE_LABELS } from '@/lib/roles'
import { SCOPE_PRESETS } from '@/lib/scope-presets'
import { rememberApiKey, useRememberedApiKey, type RememberedKey } from '@/lib/session-key'
import { browserTimeZone } from '@/lib/time-zone'
import { cn } from '@/lib/utils'
import { readWelcomeDraft, writeWelcomeDraft, type WelcomeDraft, type WelcomeGoal } from '@/lib/welcome'

type StepId = 'you' | 'workspace' | 'channel' | 'agent' | 'team' | 'plan' | 'joined' | 'done'

const GOALS: { id: WelcomeGoal; title: string; text: string; Icon: typeof Sparkles }[] = [
  {
    id: 'creator',
    title: 'Post as a creator',
    text: 'Plan reels, shorts and posts across your channels.',
    Icon: Sparkles,
  },
  { id: 'team', title: 'Run a team', text: 'Write together, plan on one calendar, invite people.', Icon: Users },
  { id: 'agents', title: 'Automate with agents', text: 'Let Claude Code, Codex or n8n write and schedule.', Icon: Bot },
]

const ROLE_TEXT: Record<Role, string> = {
  owner: 'You can do everything here, including people, channels and billing.',
  admin: 'You can do everything in this workspace: posts, channels, keys and people.',
  editor: 'You write, schedule and publish posts. Admins look after channels and people.',
  operator: 'You manage posts, channels and automation. Admins look after workspace settings and people.',
  viewer: 'You see the calendar, posts and activity. You can ask an admin for more.',
}

/** A name worth greeting someone with: not an email address, not the workspace's. */
function personName(...candidates: (string | undefined)[]) {
  return candidates.find((c) => c && !c.includes('@') && c.trim().length > 0)?.trim() ?? ''
}

/**
 * The welcome: a full-screen wizard outside the app shell, shown once after sign-up, for every new workspace and
 * reopened from the user menu. Its steps only collect (the draft, lib/welcome.ts); nothing is written until the
 * end. With billing and no plan yet, the last step is the plan (Stripe Checkout), and the workspace is set up
 * once the payment is in: renamed, people invited, the agent's key made, then the chosen platforms connected.
 */
export function WelcomeWizard() {
  const router = useRouter()
  const params = useSearchParams()
  const qc = useQueryClient()
  const session = useSession()
  const { isLocal } = useMode()
  const { accountsEnabled, isOwner, role, canManagePeople } = useRole()
  const s = session.data
  const hasPerson = !!s?.user?.id
  const member = accountsEnabled && hasPerson && !isOwner
  const canInvite = !isLocal && accountsEnabled && canManagePeople
  const [stored, setDraft] = useState(readWelcomeDraft)
  // A draft from another workspace (one set up in this tab before) starts over.
  const draft = stored.workspace && s && stored.workspace !== s.workspace.id ? {} : stored
  const goal = draft.goal ?? (isLocal ? 'agents' : 'creator')
  // Connect an agent and Invite your team are separate, skippable steps; a team goal asks for people first.
  const extra: StepId[] = !canInvite ? ['agent'] : goal === 'team' ? ['team', 'agent'] : ['agent', 'team']
  // With billing, a workspace has no plan until its owner pays (or starts Starter's trial with a card): the plan
  // is the last step, and the workspace is set up only after it.
  const billing = useBilling({ enabled: !isLocal && isOwner })
  const needsPlan = !!billing.data?.enabled && !billing.data.subscription_status
  // Until billing has answered, nothing is written and nothing is skipped.
  const billingKnown = isLocal || !isOwner || billing.isFetched
  const requested = params.get('step') as StepId | null
  const steps: StepId[] = member
    ? ['you', 'joined']
    : [
        ...(hasPerson ? (['you'] as const) : []),
        ...(draft.named ? [] : (['workspace'] as const)),
        'channel',
        ...extra,
        ...(needsPlan || requested === 'plan' ? (['plan'] as const) : []),
        'done',
      ]
  const [step, setStep] = useState<StepId>(requested && steps.includes(requested) ? requested : steps[0]!)
  const heading = useRef<HTMLHeadingElement>(null)
  const index = steps.indexOf(step)

  const update = (next: Partial<WelcomeDraft>) => {
    const d = { ...draft, ...next, workspace: s?.workspace.id }

    setDraft(d)
    writeWelcomeDraft(d)
  }

  // Each step announces itself: focus moves to its heading once it has rendered.
  const go = (to: StepId) => {
    setStep(to)
    router.replace(`/welcome?step=${to}`, { scroll: true })
    requestAnimationFrame(() => heading.current?.focus())
  }

  const next = () => go(steps[Math.min(index + 1, steps.length - 1)]!)
  const back = index > 0 ? () => go(steps[index - 1]!) : undefined

  const finish = useMutation({
    mutationFn: (to: string) => api.finishOnboarding(draft.name?.trim() || undefined).then(() => to),
    onSuccess: (to) => {
      writeWelcomeDraft(null)
      // Done in the cache before leaving, so the calendar doesn't send the person straight back here.
      qc.setQueryData(qk.session, (old: typeof s) => (old ? { ...old, onboarded: true } : old))
      void qc.invalidateQueries({ queryKey: qk.session })
      router.replace(to)
    },
  })

  if (!s) {
    return null
  }

  return (
    <PublicShell
      className="flex flex-col items-center pt-6 sm:pt-12"
      nav={
        // Without a plan there is nothing to skip to: the app opens once the workspace has one. The last step
        // ends the wizard itself.
        billingKnown &&
        !needsPlan &&
        index < steps.length - 1 && (
          <Button variant="ghost" size="sm" disabled={finish.isPending} onClick={() => finish.mutate('/schedule')}>
            Skip for now
          </Button>
        )
      }
    >
      <div className="w-full max-w-2xl">
        <Progress index={index} total={steps.length} />
        {finish.error && (
          <p role="alert" className="mt-4 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm">
            {errorMessage(finish.error)}
          </p>
        )}
        <div className="mt-8" data-testid={`welcome-step-${step}`}>
          {step === 'you' && (
            <YouStep
              heading={heading}
              initialName={draft.name ?? personName(s.user?.name)}
              goal={goal}
              showGoals={!member}
              onDone={(name, g) => {
                update({ name, goal: g })
                next()
              }}
            />
          )}
          {step === 'workspace' && (
            <WorkspaceStep
              heading={heading}
              name={draft.workspaceName ?? s.workspace.name}
              canRename={canManagePeople || isLocal}
              onBack={back}
              onDone={(workspaceName) => {
                update({ workspaceName })
                next()
              }}
            />
          )}
          {step === 'channel' && (
            <ChannelStep
              heading={heading}
              chosen={draft.providers ?? []}
              onBack={back}
              onDone={(providers) => {
                update({ providers })
                next()
              }}
            />
          )}
          {step === 'agent' && (
            <AgentStep
              heading={heading}
              isLocal={isLocal}
              canMakeKey={billingKnown && !needsPlan}
              tool={draft.agent}
              keyMade={draft.keyMade}
              onBack={back}
              onKeyMade={(agent) => update({ agent, keyMade: true })}
              onDone={(agent) => {
                update({ agent })
                next()
              }}
            />
          )}
          {step === 'team' && (
            <TeamStep
              heading={heading}
              invites={draft.invites}
              onBack={back}
              onDone={(invites) => {
                update({ invites })
                next()
              }}
            />
          )}
          {step === 'plan' && <PlanStep heading={heading} draft={draft} onBack={back} />}
          {step === 'joined' && (
            <JoinedStep
              heading={heading}
              workspace={s.workspace.name}
              role={(role as Role) ?? 'viewer'}
              busy={finish.isPending}
              onBack={back}
              onDone={() => finish.mutate('/schedule')}
            />
          )}
          {step === 'done' && (
            <DoneStep
              heading={heading}
              draft={draft}
              workspace={s.workspace}
              isLocal={isLocal}
              waitForPlan={needsPlan || !billingKnown}
              onApplied={(applied) => update({ applied })}
              busy={finish.isPending}
              onFinish={(to) => finish.mutate(to)}
              onChoosePlan={() => go('plan')}
            />
          )}
        </div>
      </div>
    </PublicShell>
  )
}

function Progress({ index, total }: { index: number; total: number }) {
  return (
    <div className="grid gap-2">
      <p className="text-xs font-medium text-muted-foreground">
        Step {index + 1} of {total}
      </p>
      <div
        role="progressbar"
        aria-label="Welcome progress"
        aria-valuemin={1}
        aria-valuemax={total}
        aria-valuenow={index + 1}
        className="flex gap-1.5"
      >
        {Array.from({ length: total }, (_, i) => (
          <span
            key={i}
            className={cn(
              'h-1.5 flex-1 rounded-full transition-colors motion-reduce:transition-none',
              i <= index ? 'bg-brand' : 'bg-muted',
            )}
          />
        ))}
      </div>
    </div>
  )
}

function StepHeader({
  heading,
  title,
  children,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  title: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="grid gap-2">
      <h1 ref={heading} tabIndex={-1} className="text-3xl font-semibold tracking-tight outline-none sm:text-4xl">
        {title}
      </h1>
      {children && <p className="text-base text-pretty text-muted-foreground">{children}</p>}
    </div>
  )
}

function Actions({ onBack, children, className }: { onBack?: () => void; children: ReactNode; className?: string }) {
  return (
    <div className={cn('mt-8 flex flex-wrap items-center justify-between gap-3', className)}>
      {onBack ? (
        <Button type="button" variant="ghost" onClick={onBack}>
          <ArrowLeft /> Back
        </Button>
      ) : (
        <span />
      )}
      <div className="flex flex-wrap items-center gap-2">{children}</div>
    </div>
  )
}

const bigButton = 'h-10 px-5 text-sm pointer-coarse:h-11'

function YouStep({
  heading,
  initialName,
  goal,
  showGoals,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  initialName: string
  goal: WelcomeGoal
  showGoals: boolean
  onDone: (name: string, goal: WelcomeGoal) => void
}) {
  const [name, setName] = useState(initialName)
  const [picked, setPicked] = useState<WelcomeGoal>(goal)

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        onDone(name.trim(), picked)
      }}
    >
      <StepHeader heading={heading} title="Welcome to Staqly">
        A few quick questions so Staqly fits the way you work. It takes about a minute.
      </StepHeader>
      <Field className="mt-8">
        <FieldLabel htmlFor="welcome-name">What should we call you?</FieldLabel>
        <Input
          id="welcome-name"
          autoComplete="name"
          maxLength={100}
          placeholder="Your name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <FieldDescription>Shown in the sidebar and to the people you work with.</FieldDescription>
      </Field>
      {showGoals && (
        <fieldset className="mt-8 grid gap-3">
          <legend className="mb-3 text-sm font-medium">What brings you here?</legend>
          <div className="grid gap-3 sm:grid-cols-3">
            {GOALS.map(({ id, title, text, Icon }) => (
              <label
                key={id}
                htmlFor={`welcome-goal-${id}`}
                className="relative grid cursor-pointer gap-3 rounded-2xl border bg-card p-4 transition-colors hover:border-foreground/20 has-checked:border-brand has-checked:bg-brand-soft/40 has-focus-visible:border-ring has-focus-visible:ring-3 has-focus-visible:ring-ring/50"
              >
                <input
                  id={`welcome-goal-${id}`}
                  type="radio"
                  name="welcome-goal"
                  value={id}
                  checked={picked === id}
                  onChange={() => setPicked(id)}
                  className="peer sr-only"
                />
                <span className="flex size-10 items-center justify-center rounded-xl bg-muted text-muted-foreground peer-checked:bg-brand peer-checked:text-[#052817]">
                  <Icon className="size-5" />
                </span>
                <span className="grid gap-1">
                  <span className="font-semibold tracking-tight">{title}</span>
                  <span className="text-sm text-muted-foreground">{text}</span>
                </span>
                <Check
                  aria-hidden
                  className="absolute top-4 right-4 size-4 text-brand opacity-0 transition-opacity peer-checked:opacity-100"
                />
              </label>
            ))}
          </div>
        </fieldset>
      )}
      <Actions>
        <Button type="submit" className={bigButton}>
          Continue <ArrowRight />
        </Button>
      </Actions>
    </form>
  )
}

function WorkspaceStep({
  heading,
  name: initial,
  canRename,
  onBack,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  name: string
  canRename: boolean
  onBack?: () => void
  onDone: (name: string) => void
}) {
  const [name, setName] = useState(initial)
  const [tried, setTried] = useState(false)

  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        setTried(true)

        if (name.trim() || !canRename) {
          onDone(name.trim() || initial)
        }
      }}
    >
      <StepHeader heading={heading} title="Name your workspace">
        Posts, channels and people live in a workspace. Most people name it after their brand or client.
      </StepHeader>
      <Field className="mt-8" data-invalid={tried && !name.trim()}>
        <FieldLabel htmlFor="welcome-workspace" required>
          Workspace name
        </FieldLabel>
        <Input
          id="welcome-workspace"
          maxLength={100}
          value={name}
          disabled={!canRename}
          placeholder="Acme Studio"
          aria-required
          aria-invalid={tried && !name.trim()}
          onChange={(e) => setName(e.target.value)}
        />
        <FieldDescription>You can rename it later in Settings.</FieldDescription>
        <FieldError>{tried && !name.trim() ? 'Give the workspace a name.' : null}</FieldError>
      </Field>
      <Actions onBack={onBack}>
        <Button type="submit" className={bigButton}>
          Continue <ArrowRight />
        </Button>
      </Actions>
    </form>
  )
}

function ChannelStep({
  heading,
  chosen,
  onBack,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  chosen: string[]
  onBack?: () => void
  onDone: (providers: string[]) => void
}) {
  const providers = useProviders()
  const [picked, setPicked] = useState(() => new Set(chosen))

  const toggle = (id: string) =>
    setPicked((prev) => {
      const next = new Set(prev)

      if (!next.delete(id)) {
        next.add(id)
      }

      return next
    })

  return (
    <div>
      <StepHeader heading={heading} title="Where do you post?">
        Pick the platforms your posts go to. You connect them in the last step, once the workspace is ready.
      </StepHeader>
      <div className="mt-6 rounded-2xl border bg-card p-4 sm:p-5">
        <ProviderGrid providers={providers.data ?? []} selected={picked} onPick={(p) => toggle(p.id)} />
      </div>
      <Actions onBack={onBack}>
        {picked.size === 0 && (
          <Button type="button" variant="ghost" onClick={() => onDone([])} className={bigButton}>
            I&apos;ll do it later
          </Button>
        )}
        <Button
          type="button"
          className={bigButton}
          variant={picked.size ? 'default' : 'outline'}
          onClick={() => onDone([...picked])}
        >
          Continue <ArrowRight />
        </Button>
      </Actions>
    </div>
  )
}

/**
 * Connect an agent: only an explicit "Make a key" makes one. A workspace that can already write gets it here, at
 * once; one that still needs its plan gets it right after the payment, on a step of its own (DoneStep).
 */
function AgentStep({
  heading,
  isLocal,
  canMakeKey,
  tool: initial,
  keyMade,
  onBack,
  onKeyMade,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  isLocal: boolean
  /** The workspace can write now (a plan, or no billing): the key is made in this step. */
  canMakeKey: boolean
  tool?: 'claude' | 'codex'
  keyMade?: boolean
  onBack?: () => void
  onKeyMade: (tool: 'claude' | 'codex') => void
  onDone: (tool: 'claude' | 'codex' | undefined) => void
}) {
  const [tool, setTool] = useState<'claude' | 'codex'>(initial ?? 'claude')
  const key = useRememberedApiKey()
  const make = useMutation({
    mutationFn: (t: 'claude' | 'codex') => createAgentKey(t),
    onSuccess: (made, t) => {
      if (made) {
        rememberApiKey(made)
      }

      onKeyMade(t)
    },
  })
  const label = tool === 'claude' ? 'Claude Code' : 'Codex'

  return (
    <div>
      <StepHeader heading={heading} title="Connect an agent">
        Let Claude Code or Codex write posts into your collections, put them on the calendar and refill them when they
        run dry.
        {!isLocal && !keyMade && !canMakeKey && ' Its key is made right after the payment, ready to paste.'}
      </StepHeader>
      <AgentPanel
        isLocal={isLocal}
        tool={tool}
        onTool={keyMade ? undefined : setTool}
        apiKey={keyMade ? key?.key : undefined}
      />
      {make.error && (
        <p role="alert" className="mt-4 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm">
          {errorMessage(make.error)}
        </p>
      )}
      <Actions onBack={onBack}>
        {isLocal || keyMade ? (
          <>
            {!keyMade && (
              <Button type="button" variant="ghost" className={bigButton} onClick={() => onDone(undefined)}>
                Not now
              </Button>
            )}
            <Button type="button" className={bigButton} onClick={() => onDone(tool)}>
              Continue <ArrowRight />
            </Button>
          </>
        ) : (
          <>
            <Button type="button" variant="ghost" className={bigButton} onClick={() => onDone(undefined)}>
              Not now
            </Button>
            <Button
              type="button"
              className={bigButton}
              disabled={make.isPending}
              onClick={() => (canMakeKey ? make.mutate(tool) : onDone(tool))}
              data-testid="make-agent-key"
            >
              {make.isPending ? 'Making the key...' : `Make a key for ${label}`} <ArrowRight />
            </Button>
          </>
        )}
      </Actions>
    </div>
  )
}

/** The agent's key: named after the tool, with the first scope preset. */
async function createAgentKey(tool: 'claude' | 'codex'): Promise<RememberedKey | undefined> {
  const k = await api.createApiKey({
    name: tool === 'claude' ? 'Claude Code' : 'Codex',
    scopes: SCOPE_PRESETS[0]!.scopes,
  })

  return k.key ? { id: k.id, name: k.name, key: k.key } : undefined
}

function TeamStep({
  heading,
  invites,
  onBack,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  invites?: WelcomeDraft['invites']
  onBack?: () => void
  onDone: (invites: WelcomeDraft['invites']) => void
}) {
  const [emails, setEmails] = useState(invites?.emails.join(', ') ?? '')
  const [role, setRole] = useState<'admin' | 'operator' | 'editor' | 'viewer'>(invites?.role ?? 'editor')
  const list = emails
    .split(/[\s,;]+/)
    .map((e) => e.trim())
    .filter(Boolean)

  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        onDone(list.length ? { emails: list, role } : undefined)
      }}
    >
      <StepHeader heading={heading} title="Invite your team">
        They get an email once the workspace is ready, sign in as that address and join with the role you pick.
      </StepHeader>
      <div className="mt-6 grid gap-4 rounded-2xl border bg-card p-4 sm:p-5">
        <Field>
          <FieldLabel htmlFor="welcome-emails">Email addresses</FieldLabel>
          <Textarea
            id="welcome-emails"
            rows={3}
            placeholder="ana@example.com, sam@example.com"
            value={emails}
            onChange={(e) => setEmails(e.target.value)}
          />
          <FieldDescription>Separate them with commas or new lines.</FieldDescription>
        </Field>
        <Field className="w-44">
          <FieldLabel htmlFor="welcome-role">Role</FieldLabel>
          <NativeSelect
            id="welcome-role"
            value={role}
            onChange={(e) => setRole(e.target.value as 'admin' | 'operator' | 'editor' | 'viewer')}
          >
            {(['editor', 'operator', 'viewer', 'admin'] as const).map((r) => (
              <option key={r} value={r}>
                {ROLE_LABELS[r]}
              </option>
            ))}
          </NativeSelect>
        </Field>
      </div>
      <Actions onBack={onBack}>
        <Button type="button" variant="ghost" className={bigButton} onClick={() => onDone(undefined)}>
          Skip
        </Button>
        <Button type="submit" className={bigButton}>
          {list.length ? `Invite ${list.length} when ready` : 'Continue'} <ArrowRight />
        </Button>
      </Actions>
    </form>
  )
}

/** How to add staqly to the agent: the command, with the key once there is one. */
function AgentPanel({
  isLocal,
  tool,
  onTool,
  apiKey,
}: {
  isLocal: boolean
  tool: 'claude' | 'codex'
  onTool?: (tool: 'claude' | 'codex') => void
  apiKey?: string
}) {
  const mcpUrl = `${usePublicApiUrl()}/mcp`
  const auth = !isLocal
  const shownKey = apiKey ?? '<YOUR_API_KEY>'
  const code =
    tool === 'claude'
      ? `claude mcp add --transport http staqly ${mcpUrl}${auth ? ` --header "Authorization: Bearer ${shownKey}"` : ''}`
      : `${auth ? `export STAQLY_API_KEY=${shownKey}\n` : ''}codex mcp add staqly --url ${mcpUrl}${auth ? ' --bearer-token-env-var STAQLY_API_KEY' : ''}`

  return (
    <div className="mt-6 grid gap-4 rounded-2xl border bg-card p-4 sm:p-5">
      {onTool && (
        <div role="tablist" aria-label="Agent" className="inline-flex w-fit rounded-lg border bg-muted/50 p-0.5">
          {(
            [
              ['claude', 'Claude Code'],
              ['codex', 'Codex'],
            ] as const
          ).map(([id, label]) => (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={tool === id}
              onClick={() => onTool(id)}
              className={cn(
                'h-7 rounded-md px-3 text-sm font-medium transition-colors pointer-coarse:h-10',
                tool === id ? 'bg-background text-foreground shadow-xs' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {label}
            </button>
          ))}
        </div>
      )}
      {apiKey && (
        <>
          <Callout tone="warning" role="status">
            Your API key is shown only now. Copy it and keep it somewhere safe; it&apos;s already in the command below.
          </Callout>
          <SecretField value={apiKey} label="Your API key" />
        </>
      )}
      <CodeBlock code={code} title="Run this in your terminal" />
      <p className="text-xs text-muted-foreground">
        Then ask it something like: write three posts about our launch and schedule one per day.
      </p>
    </div>
  )
}

/**
 * The last step without a plan: one choice among the four plans (Plus picked to start with), monthly or yearly,
 * then one button that says what happens next. Starter starts with its 7-day trial when the workspace still has
 * it (card required, nothing charged until it ends). "Compare all plans" opens the full comparison. Stripe
 * Checkout comes back to the end of the wizard.
 */
function PlanStep({
  heading,
  draft,
  onBack,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  draft: WelcomeDraft
  onBack?: () => void
}) {
  const plans = usePlans()
  const billing = useBilling()
  const trial = billing.data?.trial === 'available'
  const [interval, setInterval] = useState<BillingInterval>('month')
  const [chosen, setChosen] = useState<string | null>(null)
  const list = plans.data ?? NO_PLANS
  const picked = list.find((p) => p.id === chosen) ?? list.find((p) => p.highlight) ?? list[0]
  const withTrial = trial && picked?.id === 'starter'
  const start = useMutation({
    mutationFn: (plan: string) => api.billingCheckout(plan, interval, 'welcome'),
    onSuccess: ({ url }) => window.location.assign(url),
  })
  // Busy until the browser has left for Stripe, not only until the API answered.
  const leaving = start.isPending || start.isSuccess

  return (
    <div>
      <StepHeader heading={heading} title="Choose a plan">
        {draft.workspaceName ? `${draft.workspaceName} opens` : 'Your workspace opens'} as soon as the payment is
        through. Every plan includes editors, analytics, MCP and the REST API; cancel anytime.
      </StepHeader>
      <div className="mt-8 flex flex-wrap items-center justify-between gap-3">
        <IntervalToggle value={interval} onChange={setInterval} />
        <span className="text-xs text-muted-foreground">Per user a month, excl. VAT</span>
      </div>
      <div role="radiogroup" aria-label="Plan" className="mt-4 grid gap-2.5">
        {plans.isPending && <SkeletonList count={4} className="h-20 rounded-2xl" />}
        {list.map((p) => {
          const on = p.id === picked?.id
          const l = p.limits

          return (
            <button
              key={p.id}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => setChosen(p.id)}
              data-testid={`welcome-plan-${p.id}`}
              className={cn(
                'grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-4 rounded-2xl border bg-card px-4 py-3.5 text-left transition-colors outline-none hover:border-foreground/20 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 sm:px-5',
                on && 'border-brand bg-brand-soft/30 ring-1 ring-brand hover:border-brand',
              )}
            >
              <span
                aria-hidden
                className={cn(
                  'flex size-5 items-center justify-center rounded-full border-2 transition-colors',
                  on ? 'border-brand bg-brand text-[#052817]' : 'border-muted-foreground/40',
                )}
              >
                {on && <Check className="size-3" strokeWidth={3} />}
              </span>
              <span className="grid min-w-0 gap-0.5">
                <span className="flex flex-wrap items-center gap-2">
                  <span className="font-semibold">{p.name}</span>
                  {p.highlight && (
                    <span className="rounded-full bg-brand-gradient px-2 py-0.5 text-[11px] font-semibold text-[#052817]">
                      Most popular
                    </span>
                  )}
                  {trial && p.id === 'starter' && (
                    <span className="rounded-full border border-brand/40 px-2 py-0.5 text-[11px] font-medium text-brand-strong">
                      7 days free
                    </span>
                  )}
                </span>
                <span className="text-sm text-muted-foreground">{TAGLINES[p.id]}</span>
                <span className="text-xs text-muted-foreground">
                  {[
                    l.channels < 0 ? 'Unlimited channels' : `${l.channels} channels`,
                    l.x_posts_per_period ? `${l.x_posts_per_period.toLocaleString('en-US')} X posts` : '',
                    l.storage_bytes < 0 ? 'Unlimited storage' : `${formatBytes(l.storage_bytes)} storage`,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              </span>
              <span className="text-right">
                <span className="block text-2xl font-semibold tracking-tight tabular-nums">
                  {money(monthly(p, interval), p.currency)}
                </span>
                <span className="block text-xs text-muted-foreground">
                  {interval === 'year' ? `${money(p.price_year, p.currency)} a year` : 'a month'}
                </span>
              </span>
            </button>
          )
        })}
      </div>
      {start.error && (
        <p role="alert" className="mt-4 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm">
          {errorMessage(start.error)}
        </p>
      )}
      {withTrial && (
        <p className="mt-4 text-sm text-muted-foreground">
          Add a card at Stripe; nothing is charged until the 7 days end, and you can cancel before.
        </p>
      )}
      <Actions onBack={onBack}>
        <Button type="button" variant="ghost" className={bigButton} asChild>
          <Link href="/plans?from=welcome">Compare all plans</Link>
        </Button>
        <Button
          type="button"
          className={cn(bigButton, 'bg-brand-gradient text-[#052817] hover:brightness-110')}
          disabled={!picked || leaving}
          onClick={() => picked && start.mutate(picked.id)}
          data-testid={withTrial ? 'start-trial' : 'choose-plan'}
        >
          {leaving
            ? 'Opening checkout...'
            : withTrial
              ? 'Start 7-day free trial'
              : picked
                ? `Continue with ${picked.name}`
                : 'Continue'}{' '}
          <ArrowRight />
        </Button>
      </Actions>
    </div>
  )
}

const NO_PLANS: Plan[] = []

type Applied = NonNullable<WelcomeDraft['applied']>

/** Writes what the wizard collected: the workspace's name and zone, the invitations and the agent's key. */
async function applyDraft(
  draft: WelcomeDraft,
  workspace: { name: string; timezone: string },
  isLocal: boolean,
): Promise<{ applied: Applied; key?: RememberedKey }> {
  // A new cloud workspace starts in UTC: it takes this browser's zone (Settings can change it later).
  const zone = workspace.timezone === 'UTC' && browserTimeZone() !== 'UTC' ? browserTimeZone() : undefined
  const name = draft.workspaceName?.trim()

  if ((name && name !== workspace.name) || zone) {
    await api.updateWorkspace({ name: name || undefined, timezone: zone })
  }

  const failed: string[] = []
  let invited = 0

  for (const email of draft.invites?.emails ?? []) {
    // One at a time: each invitation is its own request and its own error.
    await api.invite(email, draft.invites!.role).then(
      () => invited++,
      // Already invited (a retry after a later step failed, or a reload mid-way) counts as invited.
      (e: unknown) =>
        e instanceof ApiError && e.code === 'already_invited' ? invited++ : failed.push(`${email}: ${errorMessage(e)}`),
    )
  }

  let key: RememberedKey | undefined

  // Made in the agent step already when the workspace could write then.
  if (draft.agent && !draft.keyMade && !isLocal) {
    key = await createAgentKey(draft.agent)
  }

  return { applied: { keyCreated: !!key, invited, failed }, key }
}

/**
 * The end: waits for the plan when the workspace just came back from Stripe (its webhook lands a moment later),
 * sets the workspace up once, then offers the chosen platforms to connect and the way into the app.
 */
function DoneStep({
  heading,
  draft,
  workspace,
  isLocal,
  waitForPlan,
  onApplied,
  busy,
  onFinish,
  onChoosePlan,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  draft: WelcomeDraft
  workspace: { id: string; name: string; timezone: string }
  isLocal: boolean
  waitForPlan: boolean
  onApplied: (applied: Applied) => void
  busy: boolean
  onFinish: (to: string) => void
  /** Back to the plan step (a soft navigation alone keeps this step on screen). */
  onChoosePlan: () => void
}) {
  const qc = useQueryClient()
  const channels = useChannels({ enabled: !waitForPlan })
  const providers = useProviders()
  const key = useRememberedApiKey()
  const [connecting, setConnecting] = useState<string | null>(null)
  const [keySeen, setKeySeen] = useState(false)
  const [waited, setWaited] = useState(0)
  const started = useRef(false)
  const apply = useMutation({
    mutationFn: () => applyDraft(draft, workspace, isLocal),
    onSuccess: ({ applied, key: made }) => {
      if (made) {
        rememberApiKey(made)
      }

      onApplied(applied)
      void qc.invalidateQueries({ queryKey: qk.session })
    },
  })
  const { mutate } = apply
  const timedOut = waitForPlan && waited >= 30

  // Back from Stripe: every 2 s (a minute at most) the server reads the subscription from Stripe, so the workspace
  // opens even when Stripe's webhook is late or lost.
  useEffect(() => {
    if (!waitForPlan || timedOut) {
      return
    }

    // One sync at a time: a slow answer from Stripe must not stack requests or land after a newer one.
    let syncing = false
    const t = setInterval(() => {
      setWaited((n) => n + 1)

      if (syncing) {
        return
      }

      syncing = true
      void api
        .syncBilling()
        .then(
          (b) => {
            qc.setQueryData(qk.billing, b)
            void qc.invalidateQueries({ queryKey: qk.usage })
          },
          () => void qc.invalidateQueries({ queryKey: qk.billing }),
        )
        .finally(() => {
          syncing = false
        })
    }, 2000)

    return () => clearInterval(t)
  }, [waitForPlan, timedOut, qc])

  useEffect(() => {
    if (!waitForPlan && !draft.applied && !started.current) {
      started.current = true
      mutate()
    }
  }, [waitForPlan, draft.applied, mutate])

  const applied = draft.applied

  if (waitForPlan || (!applied && !apply.error)) {
    return (
      <div>
        <StepHeader heading={heading} title={timedOut ? 'Still waiting for the payment' : 'Setting up your workspace'}>
          {timedOut
            ? 'Stripe has not confirmed the payment yet. If you paid, it shows up in a moment; otherwise choose a plan again.'
            : 'Your workspace opens as soon as the payment is confirmed. This takes a few seconds.'}
        </StepHeader>
        {timedOut ? (
          <Actions>
            <Button type="button" variant="outline" className={bigButton} onClick={onChoosePlan}>
              Choose a plan
            </Button>
            <Button type="button" className={bigButton} onClick={() => setWaited(0)}>
              Check again
            </Button>
          </Actions>
        ) : (
          <div role="status" className="mt-8 flex items-center gap-3 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" aria-hidden /> Setting up...
          </div>
        )}
      </div>
    )
  }

  if (apply.error) {
    return (
      <div>
        <StepHeader heading={heading} title="Something went wrong">
          {errorMessage(apply.error)}
        </StepHeader>
        <Actions>
          <Button type="button" className={bigButton} onClick={() => mutate()} disabled={apply.isPending}>
            Try again
          </Button>
        </Actions>
      </div>
    )
  }

  // A key made after the payment gets its own step first (the key lives in memory: after a reload it's gone).
  if (draft.agent && applied?.keyCreated && key?.key && !keySeen) {
    return (
      <div>
        <StepHeader heading={heading} title={`Connect ${draft.agent === 'claude' ? 'Claude Code' : 'Codex'}`}>
          Your workspace is ready. Add staqly to your agent, then ask it to write and schedule posts.
        </StepHeader>
        <AgentPanel isLocal={isLocal} tool={draft.agent} apiKey={key.key} />
        <Actions>
          <Button type="button" className={bigButton} onClick={() => setKeySeen(true)}>
            Continue <ArrowRight />
          </Button>
        </Actions>
      </div>
    )
  }

  const connected = channels.data ?? []
  const toConnect = (providers.data ?? []).filter(
    (p) => draft.providers?.includes(p.id) && !connected.some((c) => c.provider === p.id),
  )
  const provider = toConnect.find((p) => p.id === connecting) ?? null
  const items: { done: boolean; text: ReactNode; key: string }[] = [
    {
      done: true,
      key: 'workspace',
      text: (
        <>
          <span className="font-medium text-foreground">{draft.workspaceName || workspace.name}</span> is ready
        </>
      ),
    },
    ...(applied?.invited
      ? [
          {
            done: true,
            key: 'invited',
            text: `${applied.invited} ${applied.invited === 1 ? 'invitation' : 'invitations'} sent`,
          },
        ]
      : []),
    ...(applied?.failed.length ? [{ done: false, key: 'failed', text: `Not sent: ${applied.failed.join('; ')}` }] : []),
    ...(connected.length
      ? [
          {
            done: true,
            key: 'channels',
            text: `${connected.length} ${connected.length === 1 ? 'channel' : 'channels'} connected`,
          },
        ]
      : []),
  ]

  return (
    <div>
      <div className="grid justify-items-center text-center">
        <div className="relative flex size-16 items-center justify-center rounded-3xl bg-brand-soft text-brand-strong ring-8 ring-brand-soft/40">
          <PartyPopper className="size-8 motion-safe:animate-[bounce_1s_ease-in-out_2]" />
        </div>
        <div className="mt-6 [&_p]:mx-auto [&_p]:max-w-md">
          <StepHeader heading={heading} title="You are all set">
            {toConnect.length
              ? 'Connect the platforms you picked, then write your first post.'
              : 'Your workspace is ready. Write your first post, or open the calendar and plan the week.'}
          </StepHeader>
        </div>
      </div>
      <ul className="mx-auto mt-8 grid max-w-md divide-y rounded-2xl border bg-card" aria-label="What is set up">
        {items.map((it) => (
          <li key={it.key} className="flex items-center gap-3 px-4 py-3 text-sm text-muted-foreground">
            <span
              className={cn(
                'flex size-6 shrink-0 items-center justify-center rounded-full',
                it.done ? 'bg-brand text-[#052817]' : 'bg-muted text-muted-foreground',
              )}
            >
              {it.done ? <Check className="size-3.5" /> : <CalendarClock className="size-3.5" />}
            </span>
            <span className="min-w-0 wrap-anywhere">{it.text}</span>
          </li>
        ))}
      </ul>
      {toConnect.length > 0 && (
        <div className="mt-6 rounded-2xl border bg-card p-4 sm:p-5">
          {provider ? (
            <div className="grid gap-4">
              <div className="flex items-center gap-2">
                <Button
                  size="icon"
                  variant="ghost"
                  className="-ml-2"
                  aria-label="Back to platforms"
                  onClick={() => setConnecting(null)}
                >
                  <ArrowLeft />
                </Button>
                <ProviderTile id={provider.id} name={provider.name} className="size-7" />
                <h2 className="font-semibold">Connect {provider.name}</h2>
              </div>
              <ConnectForm
                provider={provider}
                reconnect={null}
                oauthReturnTo="/welcome?step=done"
                onDone={() => setConnecting(null)}
              />
            </div>
          ) : (
            <ul className="grid gap-2" aria-label="Platforms to connect">
              {toConnect.map((p) => (
                <li key={p.id} className="flex items-center gap-3 text-sm">
                  <ProviderTile id={p.id} name={p.name} className="size-7" />
                  <span className="min-w-0 flex-1 truncate font-medium">{p.name}</span>
                  <Button type="button" variant="outline" size="sm" onClick={() => setConnecting(p.id)}>
                    Connect
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <div className="mt-10 grid gap-3 sm:grid-cols-2">
        <NextStep
          icon={<PenLine />}
          title="Write your first post"
          description="Draft it now, then publish it or schedule it."
          primary
          disabled={busy}
          onClick={() => onFinish('/posts?compose=1')}
        />
        <NextStep
          icon={<CalendarClock />}
          title="Go to the calendar"
          description="See the week and plan what goes out when."
          disabled={busy}
          onClick={() => onFinish('/schedule')}
        />
      </div>
    </div>
  )
}

/** One way out of the wizard: a big card with what it opens. */
function NextStep({
  icon,
  title,
  description,
  primary,
  disabled,
  onClick,
}: {
  icon: ReactNode
  title: string
  description: string
  primary?: boolean
  disabled?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={cn(
        'group flex items-start gap-3 rounded-2xl border bg-card p-4 text-left transition-colors outline-none hover:border-brand/60 focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-60 sm:p-5',
        primary && 'border-brand/50 bg-brand-soft/30',
      )}
    >
      <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand-soft text-brand-strong [&_svg]:size-5">
        {icon}
      </span>
      <span className="grid min-w-0 flex-1 gap-1">
        <span className="font-semibold">{title}</span>
        <span className="text-sm text-muted-foreground">{description}</span>
      </span>
      <ArrowRight className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
    </button>
  )
}

function JoinedStep({
  heading,
  workspace,
  role,
  busy,
  onBack,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  workspace: string
  role: Role
  busy: boolean
  onBack?: () => void
  onDone: () => void
}) {
  return (
    <div>
      <StepHeader heading={heading} title={`You joined ${workspace}`}>
        Posts here go out on a schedule to the workspace&apos;s channels.
      </StepHeader>
      <div className="mt-8 flex items-start gap-3 rounded-2xl border bg-card p-4 sm:p-5">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand-soft text-brand-strong">
          <Users className="size-5" />
        </span>
        <div>
          <p className="font-semibold">
            {`You are ${/^[aeiou]/i.test(role) ? 'an' : 'a'} ${ROLE_LABELS[role].toLowerCase()}`}
          </p>
          <p className="mt-1 text-sm text-muted-foreground">{ROLE_TEXT[role]}</p>
        </div>
      </div>
      <Actions onBack={onBack}>
        <Button type="button" className={bigButton} disabled={busy} onClick={onDone}>
          Go to the calendar <ArrowRight />
        </Button>
      </Actions>
    </div>
  )
}
