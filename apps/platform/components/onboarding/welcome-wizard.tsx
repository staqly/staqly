'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  CalendarClock,
  Check,
  KeyRound,
  PartyPopper,
  PenLine,
  Sparkles,
  UserPlus,
  Users,
} from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useRef, useState, type ReactNode } from 'react'

import { PublicShell } from '@/components/auth/public-shell'
import { ConnectForm } from '@/components/channels/add-channel-sheet'
import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { ProviderGrid } from '@/components/channels/provider-grid'
import { Callout } from '@/components/common/callout'
import { CodeBlock } from '@/components/common/code-block'
import { NativeSelect } from '@/components/common/native-select'
import { SecretField } from '@/components/common/secret-field'
import { money } from '@/components/plans/plan-cards'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { api, errorMessage, type Role } from '@/lib/api'
import { usePublicApiUrl } from '@/lib/config'
import { ProviderTile } from '@/lib/providers'
import { qk, useBilling, useChannels, useMode, usePlans, useProviders, useRole, useSession } from '@/lib/queries'
import { ROLE_LABELS } from '@/lib/roles'
import { SCOPE_PRESETS } from '@/lib/scope-presets'
import { rememberApiKey } from '@/lib/session-key'
import { cn } from '@/lib/utils'
import { readWelcomeDraft, writeWelcomeDraft, type WelcomeGoal } from '@/lib/welcome'

type StepId = 'you' | 'trial' | 'workspace' | 'channel' | 'agent' | 'team' | 'joined' | 'done'

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
  viewer: 'You see the calendar, posts and activity. You can ask an admin for more.',
}

/** A name worth greeting someone with: not an email address, not the workspace's. */
function personName(...candidates: (string | undefined)[]) {
  return candidates.find((c) => c && !c.includes('@') && c.trim().length > 0)?.trim() ?? ''
}

/**
 * The welcome: a full-screen wizard outside the app shell, shown once after sign-up (and on a fresh local
 * install), reopened from the user menu. Each step is optional past the first; Skip for now ends it.
 */
export function WelcomeWizard() {
  const router = useRouter()
  const params = useSearchParams()
  const qc = useQueryClient()
  const session = useSession()
  const { isLocal } = useMode()
  const { accountsEnabled, accountOwner, role, canManagePeople } = useRole()
  const s = session.data
  const hasPerson = !!s?.user?.id
  const member = accountsEnabled && hasPerson && !accountOwner
  const canInvite = !isLocal && accountsEnabled && canManagePeople
  const [draft, setDraft] = useState(readWelcomeDraft)
  const goal = draft.goal ?? (isLocal ? 'agents' : 'creator')
  // Connect an agent and Invite your team are separate, skippable steps; a team goal asks for people first.
  const extra: StepId[] = !canInvite ? ['agent'] : goal === 'team' ? ['team', 'agent'] : ['agent', 'team']
  // With billing, a new workspace has no plan until its owner starts one: the trial comes right after the first
  // step, before anything that writes to the workspace.
  const billing = useBilling({ enabled: !isLocal && accountOwner })
  const needsPlan = !!billing.data?.enabled && !billing.data.subscription_status && billing.data.suspended
  const steps: StepId[] = member
    ? ['you', 'joined']
    : [
        ...(hasPerson ? (['you'] as const) : []),
        ...(needsPlan || params.get('step') === 'trial' ? (['trial'] as const) : []),
        'workspace',
        'channel',
        ...extra,
        'done',
      ]
  const requested = params.get('step') as StepId | null
  const [step, setStep] = useState<StepId>(requested && steps.includes(requested) ? requested : steps[0]!)
  const [summary, setSummary] = useState({ key: false, invites: 0 })
  const heading = useRef<HTMLHeadingElement>(null)
  const index = steps.indexOf(step)

  const update = (next: Partial<typeof draft>) => {
    const d = { ...draft, ...next }

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
      void qc.invalidateQueries({ queryKey: qk.account })
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
        <Button variant="ghost" size="sm" disabled={finish.isPending} onClick={() => finish.mutate('/schedule')}>
          Skip for now
        </Button>
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
          {step === 'trial' && <TrialStep heading={heading} onBack={back} onSkip={next} started={!needsPlan} />}
          {step === 'workspace' && (
            <WorkspaceStep
              heading={heading}
              name={s.workspace.name}
              canRename={canManagePeople || isLocal}
              onBack={back}
              onDone={next}
            />
          )}
          {step === 'channel' && (
            <ChannelStep heading={heading} error={params.get('error')} onBack={back} onDone={next} />
          )}
          {step === 'agent' && (
            <AgentStep
              heading={heading}
              isLocal={isLocal}
              onBack={back}
              onDone={(key) => {
                setSummary((x) => ({ ...x, key: x.key || key }))
                next()
              }}
            />
          )}
          {step === 'team' && (
            <TeamStep
              heading={heading}
              onBack={back}
              onDone={(invites) => {
                setSummary((x) => ({ ...x, invites: x.invites + invites }))
                next()
              }}
            />
          )}
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
              workspace={s.workspace.name}
              summary={summary}
              busy={finish.isPending}
              onFinish={(to) => finish.mutate(to)}
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
                className="relative grid cursor-pointer gap-3 rounded-2xl border bg-card p-4 transition-colors hover:border-foreground/20 has-checked:border-brand has-checked:bg-brand-soft/40 has-focus-visible:ring-3 has-focus-visible:ring-ring/50"
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
  onDone: () => void
}) {
  const qc = useQueryClient()
  const [name, setName] = useState(initial)
  const rename = useMutation({
    mutationFn: () => api.renameWorkspace(name.trim()),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.session })
      onDone()
    },
  })

  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault()

        if (!canRename || name.trim() === initial) {
          onDone()

          return
        }

        if (name.trim()) {
          rename.mutate()
        }
      }}
    >
      <StepHeader heading={heading} title="Name your workspace">
        Posts, channels and people live in a workspace. Most people name it after their brand or client.
      </StepHeader>
      <Field className="mt-8" data-invalid={!!rename.error || !name.trim()}>
        <FieldLabel htmlFor="welcome-workspace" required>
          Workspace name
        </FieldLabel>
        <Input
          id="welcome-workspace"
          maxLength={100}
          value={name}
          disabled={!canRename}
          placeholder="Acme Studio"
          onChange={(e) => setName(e.target.value)}
        />
        <FieldDescription>You can rename it later in Settings.</FieldDescription>
        <FieldError>
          {!name.trim() ? 'Give the workspace a name.' : rename.error && errorMessage(rename.error)}
        </FieldError>
      </Field>
      <Actions onBack={onBack}>
        <Button type="submit" className={bigButton} disabled={rename.isPending}>
          {rename.isPending ? 'Saving...' : 'Continue'} <ArrowRight />
        </Button>
      </Actions>
    </form>
  )
}

function ChannelStep({
  heading,
  error,
  onBack,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  error: string | null
  onBack?: () => void
  onDone: () => void
}) {
  const providers = useProviders()
  const channels = useChannels()
  const [picked, setPicked] = useState<string | null>(null)
  const provider = providers.data?.find((p) => p.id === picked) ?? null
  const connected = channels.data ?? []

  return (
    <div>
      <StepHeader heading={heading} title="Connect your first channel">
        Where your posts go. Connect one now and add the rest whenever you like.
      </StepHeader>
      {error && (
        <p role="alert" className="mt-6 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm">
          The channel was not connected: {error}
        </p>
      )}
      {connected.length > 0 && (
        <ul className="mt-6 grid gap-2" aria-label="Connected channels">
          {connected.map((c) => (
            <li key={c.id} className="flex items-center gap-3 rounded-xl border bg-card px-3 py-2.5 text-sm">
              <ChannelAvatar channel={c} className="size-7" />
              <span className="min-w-0 flex-1 truncate font-medium">{c.name}</span>
              <span className="inline-flex items-center gap-1 text-xs text-brand-strong">
                <Check className="size-3.5" /> Connected
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className="mt-6 rounded-2xl border bg-card p-4 sm:p-5">
        {provider ? (
          <div className="grid gap-4">
            <div className="flex items-center gap-2">
              <Button
                size="icon"
                variant="ghost"
                className="-ml-2"
                aria-label="Back to platforms"
                onClick={() => setPicked(null)}
              >
                <ArrowLeft />
              </Button>
              <ProviderTile id={provider.id} name={provider.name} className="size-7" />
              <h2 className="font-semibold">Connect {provider.name}</h2>
            </div>
            <ConnectForm
              provider={provider}
              reconnect={null}
              oauthReturnTo="/welcome?step=channel"
              onDone={() => setPicked(null)}
            />
          </div>
        ) : (
          <ProviderGrid providers={providers.data ?? []} onPick={(p) => setPicked(p.id)} />
        )}
      </div>
      <Actions onBack={onBack}>
        {connected.length === 0 && (
          <Button type="button" variant="ghost" onClick={onDone} className={bigButton}>
            I&apos;ll do it later
          </Button>
        )}
        <Button type="button" className={bigButton} variant={connected.length ? 'default' : 'outline'} onClick={onDone}>
          Continue <ArrowRight />
        </Button>
      </Actions>
    </div>
  )
}

function AgentStep({
  heading,
  isLocal,
  onBack,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  isLocal: boolean
  onBack?: () => void
  onDone: (key: boolean) => void
}) {
  const [key, setKey] = useState(false)

  return (
    <div>
      <StepHeader heading={heading} title="Connect an agent">
        Let Claude Code or Codex write posts into your collections, put them on the calendar and refill them when they
        run dry.
      </StepHeader>
      <AgentPanel isLocal={isLocal} onKey={() => setKey(true)} />
      <Actions onBack={onBack}>
        <Button type="button" variant="ghost" className={bigButton} onClick={() => onDone(false)}>
          Skip
        </Button>
        <Button type="button" className={bigButton} onClick={() => onDone(key)}>
          Continue <ArrowRight />
        </Button>
      </Actions>
    </div>
  )
}

function TeamStep({
  heading,
  onBack,
  onDone,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  onBack?: () => void
  onDone: (invites: number) => void
}) {
  const [invites, setInvites] = useState(0)

  return (
    <div>
      <StepHeader heading={heading} title="Invite your team">
        They get an email, sign in as that address and join with the role you pick.
      </StepHeader>
      <InvitePanel onSent={(n) => setInvites((x) => x + n)} />
      <Actions onBack={onBack}>
        <Button type="button" variant="ghost" className={bigButton} onClick={() => onDone(0)}>
          Skip
        </Button>
        <Button type="button" className={bigButton} onClick={() => onDone(invites)}>
          Continue <ArrowRight />
        </Button>
      </Actions>
    </div>
  )
}

function AgentPanel({ isLocal, onKey }: { isLocal: boolean; onKey: () => void }) {
  const mcpUrl = `${usePublicApiUrl()}/mcp`
  const [tool, setTool] = useState<'claude' | 'codex'>('claude')
  const [key, setKey] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: () =>
      api.createApiKey({ name: tool === 'claude' ? 'Claude Code' : 'Codex', scopes: SCOPE_PRESETS[0]!.scopes }),
    onSuccess: (k) => {
      if (k.key) {
        rememberApiKey({ id: k.id, name: k.name, key: k.key })
        setKey(k.key)
        onKey()
      }
    },
  })
  const auth = !isLocal
  const shownKey = key ?? '<YOUR_API_KEY>'
  const code =
    tool === 'claude'
      ? `claude mcp add --transport http staqly ${mcpUrl}${auth ? ` --header "Authorization: Bearer ${shownKey}"` : ''}`
      : `${auth ? `export STAQLY_API_KEY=${shownKey}\n` : ''}codex mcp add staqly --url ${mcpUrl}${auth ? ' --bearer-token-env-var STAQLY_API_KEY' : ''}`

  return (
    <div className="mt-6 grid gap-4 rounded-2xl border bg-card p-4 sm:p-5">
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
            onClick={() => setTool(id)}
            className={cn(
              'h-7 rounded-md px-3 text-sm font-medium transition-colors pointer-coarse:h-10',
              tool === id ? 'bg-background text-foreground shadow-xs' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {label}
          </button>
        ))}
      </div>
      {auth && !key && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed p-3">
          <p className="text-sm text-muted-foreground">
            Your agent signs in with its own key that can write and publish posts.
          </p>
          <Button type="button" variant="outline" disabled={create.isPending} onClick={() => create.mutate()}>
            <KeyRound /> {create.isPending ? 'Creating...' : 'Create a key for it'}
          </Button>
        </div>
      )}
      {create.error && <p className="text-sm text-destructive">{errorMessage(create.error)}</p>}
      {key && (
        <>
          <Callout tone="warning" role="status">
            Your API key is shown only now. Copy it and keep it somewhere safe; it&apos;s already in the command below.
          </Callout>
          <SecretField value={key} label="Your API key" />
        </>
      )}
      <CodeBlock code={code} title="Run this in your terminal" />
      <p className="text-xs text-muted-foreground">
        Then ask it something like: write three posts about our launch and schedule one per day.
      </p>
    </div>
  )
}

function InvitePanel({ onSent }: { onSent: (n: number) => void }) {
  const [emails, setEmails] = useState('')
  const [role, setRole] = useState<Role>('editor')
  const [message, setMessage] = useState<{ ok: boolean; text: string } | null>(null)
  const send = useMutation({
    mutationFn: async () => {
      const list = emails
        .split(/[\s,;]+/)
        .map((e) => e.trim())
        .filter(Boolean)
      const failed: string[] = []
      let sent = 0

      for (const email of list) {
        try {
          // One at a time: each invitation is its own request and its own error.
          // eslint-disable-next-line no-await-in-loop
          await api.invite(email, role)
          sent++
        } catch (e) {
          failed.push(`${email}: ${errorMessage(e)}`)
        }
      }

      return { sent, failed }
    },
    onSuccess: ({ sent, failed }) => {
      onSent(sent)
      setEmails(failed.map((f) => f.split(':')[0]).join(', '))
      setMessage(
        failed.length
          ? { ok: false, text: `${sent} sent. Not sent: ${failed.join('; ')}` }
          : { ok: true, text: `${sent} ${sent === 1 ? 'invitation' : 'invitations'} sent.` },
      )
    },
  })

  return (
    <form
      noValidate
      className="mt-6 grid gap-4 rounded-2xl border bg-card p-4 sm:p-5"
      onSubmit={(e) => {
        e.preventDefault()

        if (emails.trim()) {
          send.mutate()
        }
      }}
    >
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
      <div className="flex flex-wrap items-end justify-between gap-3">
        <Field className="w-44">
          <FieldLabel htmlFor="welcome-role">Role</FieldLabel>
          <NativeSelect id="welcome-role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {(['editor', 'viewer', 'admin'] as const).map((r) => (
              <option key={r} value={r}>
                {ROLE_LABELS[r]}
              </option>
            ))}
          </NativeSelect>
        </Field>
        <Button type="submit" variant="outline" disabled={send.isPending || !emails.trim()}>
          <UserPlus /> {send.isPending ? 'Sending...' : 'Send invitations'}
        </Button>
      </div>
      {message && (
        <p role="status" className={cn('text-sm', message.ok ? 'text-brand-strong' : 'text-destructive')}>
          {message.text}
        </p>
      )}
    </form>
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

function DoneStep({
  heading,
  workspace,
  summary,
  busy,
  onFinish,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  workspace: string
  summary: { key: boolean; invites: number }
  busy: boolean
  onFinish: (to: string) => void
}) {
  const channels = useChannels()
  const n = channels.data?.length ?? 0
  const items: { done: boolean; text: string }[] = [
    { done: true, text: `Workspace ${workspace} is ready` },
    {
      done: n > 0,
      text: n > 0 ? `${n} ${n === 1 ? 'channel' : 'channels'} connected` : 'No channel yet: add one from Channels',
    },
    { done: summary.key, text: summary.key ? 'An agent key is ready' : 'Agents can join later from Integrations' },
    ...(summary.invites
      ? [{ done: true, text: `${summary.invites} ${summary.invites === 1 ? 'invitation' : 'invitations'} sent` }]
      : []),
  ]

  return (
    <div>
      <div className="flex size-12 items-center justify-center rounded-2xl bg-brand-soft text-brand-strong">
        <PartyPopper className="size-6 motion-safe:animate-[bounce_1s_ease-in-out_2]" />
      </div>
      <div className="mt-6">
        <StepHeader heading={heading} title="You are all set">
          Write your first post or open the calendar and plan the week.
        </StepHeader>
      </div>
      <ul className="mt-8 grid gap-2">
        {items.map((it) => (
          <li key={it.text} className="flex items-center gap-3 rounded-xl border bg-card px-4 py-3 text-sm">
            <span
              className={cn(
                'flex size-6 shrink-0 items-center justify-center rounded-full',
                it.done ? 'bg-brand text-[#052817]' : 'bg-muted text-muted-foreground',
              )}
            >
              {it.done ? <Check className="size-3.5" /> : <CalendarClock className="size-3.5" />}
            </span>
            {it.text}
          </li>
        ))}
      </ul>
      <Actions>
        <Button
          type="button"
          variant="outline"
          className={bigButton}
          disabled={busy}
          onClick={() => onFinish('/posts?compose=1')}
        >
          <PenLine /> Write your first post
        </Button>
        <Button type="button" className={bigButton} disabled={busy} onClick={() => onFinish('/schedule')}>
          Go to the calendar <ArrowRight />
        </Button>
      </Actions>
    </div>
  )
}

/**
 * Billing on, no subscription yet: Starter's 7-day trial (card required, nothing charged until it ends) through
 * Stripe Checkout, which comes back to the next step. Other plans from the plan page.
 */
function TrialStep({
  heading,
  onBack,
  onSkip,
  started,
}: {
  heading: React.RefObject<HTMLHeadingElement | null>
  onBack?: () => void
  onSkip: () => void
  started: boolean
}) {
  const plans = usePlans()
  const starter = plans.data?.find((p) => p.id === 'starter')
  const start = useMutation({
    mutationFn: () => api.billingCheckout('starter', 'month', 'welcome'),
    onSuccess: ({ url }) => window.location.assign(url),
  })
  const price = starter ? money(starter.price_month, starter.currency) : ''

  return (
    <div>
      <StepHeader heading={heading} title={started ? 'Your trial has started' : 'Start your 7-day free trial'}>
        {started
          ? 'Starter is yours for 7 days. Nothing is charged until the trial ends; cancel anytime in Billing.'
          : `Try Starter for 7 days. Add a card now; nothing is charged until the trial ends${price ? `, then ${price} per user a month, excl. VAT` : ''}. Cancel anytime. Until a plan starts, the workspace can be read but not changed.`}
      </StepHeader>
      {start.error && (
        <p role="alert" className="mt-4 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm">
          {errorMessage(start.error)}
        </p>
      )}
      <Actions onBack={onBack}>
        {started ? (
          <Button type="button" className={bigButton} onClick={onSkip}>
            Continue <ArrowRight />
          </Button>
        ) : (
          <>
            <Button type="button" variant="outline" className={bigButton} asChild>
              <Link href="/plans">See all plans</Link>
            </Button>
            <Button
              type="button"
              className={bigButton}
              disabled={start.isPending}
              onClick={() => start.mutate()}
              data-testid="start-trial"
            >
              {start.isPending ? 'Opening checkout...' : 'Start free trial'} <ArrowRight />
            </Button>
          </>
        )}
      </Actions>
    </div>
  )
}
