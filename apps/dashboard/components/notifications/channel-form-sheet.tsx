'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { KeyRound, RefreshCw, Undo2 } from 'lucide-react'
import { useId, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ActiveToggle } from '@/components/common/active-toggle'
import { Callout } from '@/components/common/callout'
import { ChipsInput } from '@/components/common/chips-input'
import { CopyButton } from '@/components/common/copy-button'
import { MultiSelect, type MultiOption } from '@/components/common/multi-select'
import { NativeSelect } from '@/components/common/native-select'
import { ToggleChip } from '@/components/common/toggle-chip'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RequiredMark } from '@/components/ui/label'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { api, type ChannelType, type NotificationChannel } from '@/lib/api'
import {
  CHANNEL_EVENTS,
  CHANNEL_TYPE_INFO,
  CHANNEL_TYPES,
  DIGEST_PRESETS,
  channelApiFieldMap,
  channelToValues,
  defaultChannelValues,
  digestLabel,
  maskedHint,
  randomTopic,
  storedSecrets,
  validateChannel,
  valuesToChannelInput,
  type ChannelFormValues,
  type SecretAction,
  type SecretField,
  type SecretState,
} from '@/lib/channels'
import { focusFirstInvalid, formApiError, useFormErrors } from '@/lib/form-errors'
import { qk, useCollections, useMode } from '@/lib/queries'
import { cn } from '@/lib/utils'

import { ChannelSetupGuide } from './channel-setup-guide'
import { ChannelTypeIcon } from './channel-type-icon'

export function ChannelFormSheet({
  open,
  onOpenChange,
  channel,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  channel?: NotificationChannel | null
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle>{channel ? 'Edit channel' : 'New notification channel'}</SheetTitle>
          <SheetDescription>
            {channel
              ? `${CHANNEL_TYPE_INFO[channel.type].label} · secrets are stored encrypted and never shown again.`
              : 'Get a message when something needs review — and decide from chat.'}
          </SheetDescription>
        </SheetHeader>
        {open && (
          <ChannelForm key={channel?.id ?? 'new'} channel={channel ?? null} onDone={() => onOpenChange(false)} />
        )}
      </SheetContent>
    </Sheet>
  )
}

function TypePicker({ value, onChange }: { value: ChannelType; onChange: (t: ChannelType) => void }) {
  return (
    <div role="radiogroup" aria-label="Channel type" className="grid grid-cols-2 gap-2 sm:grid-cols-3">
      {CHANNEL_TYPES.map((t) => {
        const on = t === value

        return (
          <button
            key={t}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => onChange(t)}
            className={cn(
              'flex min-h-14 items-center gap-2.5 rounded-xl border p-2.5 text-left transition-colors',
              on ? 'border-brand/60 bg-brand-soft ring-1 ring-brand/30' : 'bg-background hover:bg-muted',
            )}
          >
            <ChannelTypeIcon type={t} className="size-8" />
            <span className="min-w-0 text-sm font-medium">{CHANNEL_TYPE_INFO[t].label}</span>
          </button>
        )
      })}
    </div>
  )
}

/**
 * Secret input. A stored secret is shown as "•••• set" with Replace (and
 * optionally Remove); it is never prefilled.
 */
function SecretInput({
  field,
  label,
  optional,
  stored,
  storedHint,
  action,
  onAction,
  value,
  onChange,
  error,
  placeholder,
  description,
  url,
  required = false,
}: {
  field: SecretField
  label: string
  optional?: boolean
  required?: boolean
  stored: boolean
  storedHint?: string
  action: SecretAction
  onAction: (a: SecretAction) => void
  value: string
  onChange: (v: string) => void
  error?: string
  placeholder?: string
  description?: React.ReactNode
  url?: boolean
}) {
  const id = `ch-${field}`

  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor={stored && action !== 'set' ? undefined : id} required={required}>
        {label}
        {optional && <span className="font-normal text-muted-foreground">(optional)</span>}
      </FieldLabel>
      {stored && action === 'keep' ? (
        <div className="flex flex-wrap items-center gap-2" data-testid={`secret-${field}`}>
          <span className="inline-flex h-8 min-w-0 items-center gap-1.5 rounded-lg border bg-muted/50 px-2.5 text-sm pointer-coarse:h-11">
            <KeyRound className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
            <span>
              <span className="font-mono tracking-wider">••••</span> set
            </span>
            {storedHint && <span className="truncate text-xs text-muted-foreground">({storedHint})</span>}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onAction('set')}
            aria-label={`Replace ${label}`}
          >
            Replace
          </Button>
          {optional && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => onAction('remove')}
              aria-label={`Remove ${label}`}
            >
              Remove
            </Button>
          )}
        </div>
      ) : stored && action === 'remove' ? (
        <div className="flex flex-wrap items-center gap-2 text-sm" data-testid={`secret-${field}`}>
          <span className="text-muted-foreground">Will be removed when you save.</span>
          <Button type="button" variant="ghost" size="sm" onClick={() => onAction('keep')}>
            <Undo2 /> Undo
          </Button>
        </div>
      ) : (
        <div className="flex gap-2">
          <Input
            id={id}
            type={url ? 'url' : 'password'}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={placeholder}
            autoComplete={url ? 'off' : 'new-password'}
            spellCheck={false}
            autoCapitalize="off"
            className="min-w-0 font-mono text-sm"
            aria-invalid={!!error}
            aria-required={required || undefined}
          />
          {stored && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="shrink-0 pointer-coarse:h-11"
              onClick={() => onAction('keep')}
            >
              Keep current
            </Button>
          )}
        </div>
      )}
      {description && <FieldDescription className="text-xs">{description}</FieldDescription>}
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

function TextField({
  id,
  label,
  optional,
  value,
  onChange,
  error,
  placeholder,
  description,
  mono,
  inputMode,
  type,
  className,
  required = false,
}: {
  id: string
  label: string
  optional?: boolean
  required?: boolean
  value: string
  onChange: (v: string) => void
  error?: string
  placeholder?: string
  description?: React.ReactNode
  mono?: boolean
  inputMode?: React.HTMLAttributes<HTMLInputElement>['inputMode']
  type?: string
  className?: string
}) {
  return (
    <Field data-invalid={!!error} className={className}>
      <FieldLabel htmlFor={id} required={required}>
        {label}
        {optional && <span className="font-normal text-muted-foreground">(optional)</span>}
      </FieldLabel>
      <Input
        id={id}
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        inputMode={inputMode}
        autoComplete="off"
        spellCheck={false}
        autoCapitalize="off"
        className={cn('min-w-0', mono && 'font-mono text-sm')}
        aria-invalid={!!error}
        aria-required={required || undefined}
      />
      {description && <FieldDescription className="text-xs">{description}</FieldDescription>}
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

function Section({
  title,
  hint,
  required = false,
  invalid = false,
  children,
}: {
  title: string
  hint?: React.ReactNode
  required?: boolean
  invalid?: boolean
  children: React.ReactNode
}) {
  return (
    <section className="grid min-w-0 grid-cols-1 gap-3">
      <div>
        <h3 className={cn('flex items-center gap-2 text-sm font-semibold', invalid && 'text-destructive')}>
          {title}
          {required && <RequiredMark />}
        </h3>
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
      {children}
    </section>
  )
}

function DeliveryToggle({
  value,
  onChange,
  botLabel,
}: {
  value: 'webhook' | 'bot'
  onChange: (v: 'webhook' | 'bot') => void
  botLabel: string
}) {
  return (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label="Send with">
      <ToggleChip pressed={value === 'webhook'} onClick={() => onChange('webhook')}>
        Webhook URL
      </ToggleChip>
      <ToggleChip pressed={value === 'bot'} onClick={() => onChange('bot')}>
        {botLabel}
      </ToggleChip>
    </div>
  )
}

function ChannelForm({ channel, onDone }: { channel: NotificationChannel | null; onDone: () => void }) {
  const qc = useQueryClient()
  const { isLocal } = useMode()
  const collections = useCollections(false)
  const [type, setType] = useState<ChannelType>(channel?.type ?? 'telegram')
  const [values, setValues] = useState<ChannelFormValues>(() =>
    channel ? channelToValues(channel) : defaultChannelValues('telegram'),
  )
  const [secrets, setSecrets] = useState<SecretState>({})
  const formRef = useRef<HTMLFormElement>(null)
  const uid = useId()
  const stored = storedSecrets(channel)
  const editing = !!channel
  // Client errors follow every change; useFormErrors shows them only after the first submit.
  const form = useFormErrors(validateChannel(type, values, stored, secrets))
  const errors = form.errors

  const set = <K extends keyof ChannelFormValues>(key: K, value: ChannelFormValues[K]) => {
    setValues((prev) => ({ ...prev, [key]: value }))
    form.touch(key)
  }
  const secretAction = (f: SecretField): SecretAction => secrets[f] ?? (stored.includes(f) ? 'keep' : 'set')
  const setSecretAction = (f: SecretField, a: SecretAction) => {
    setSecrets((prev) => ({ ...prev, [f]: a }))

    if (a !== 'set') {
      setValues((prev) => ({ ...prev, [f]: '' }))
    }

    form.touch(f)
  }
  const secretProps = (f: SecretField) => ({
    field: f,
    stored: stored.includes(f),
    storedHint: maskedHint(channel?.config[f]),
    action: secretAction(f),
    onAction: (a: SecretAction) => setSecretAction(f, a),
    value: values[f],
    onChange: (v: string) => set(f, v),
    error: errors[f],
  })

  const save = useMutation({
    mutationFn: () => {
      const body = valuesToChannelInput(type, values, { editing, stored, secrets })

      return channel ? api.updateChannel(channel.id, body) : api.createChannel(body)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.channels })
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success(channel ? 'Channel saved' : 'Channel created', {
        description: channel ? undefined : 'Use Send test to check that messages arrive.',
      })
      onDone()
    },
    onError: (e) => {
      form.setServerErrors(
        formApiError(
          e,
          channelApiFieldMap(type, values.delivery),
          channel ? 'Could not save the channel' : 'Could not create the channel',
        ),
      )
      focusFirstInvalid(formRef.current)
    },
  })

  function pickType(t: ChannelType) {
    setType(t)
    form.reset()
    setValues((v) => ({
      ...defaultChannelValues(t),
      // Keep the user's name unless it is still another type's default.
      name: CHANNEL_TYPES.some((x) => CHANNEL_TYPE_INFO[x].label === v.name) ? CHANNEL_TYPE_INFO[t].label : v.name,
      events: v.events,
      collection_ids: v.collection_ids,
      digest_window_seconds: v.digest_window_seconds,
      enabled: v.enabled,
    }))
  }

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()

    if (!form.submit()) {
      focusFirstInvalid(e.currentTarget)

      return
    }

    save.mutate()
  }

  // Same picker as webhooks: grouped events, action events from the collections, patterns saved through the API.
  const allItems = values.events.includes('item.*')
  const actionEvents = [
    ...new Set((collections.data ?? []).flatMap((c) => c.actions ?? []).map((a) => a.event)),
  ].filter((e) => !CHANNEL_EVENTS.some((c) => c.value === e))
  const known = new Set([...CHANNEL_EVENTS.map((c) => c.value), ...actionEvents])
  const eventOptions: MultiOption[] = [
    ...CHANNEL_EVENTS.map((ev) => ({
      value: ev.value,
      label: ev.value,
      hint: ev.hint,
      group: ev.group,
      implied: allItems && ev.value !== 'item.*' && ev.value.startsWith('item.'),
    })),
    ...actionEvents.map((e) => ({ value: e, label: e, group: 'Actions', implied: allItems })),
    ...values.events.filter((e) => !known.has(e)).map((e) => ({ value: e, label: e, group: 'Other patterns' })),
  ]
  // A wildcard already covers the item events: keep only what it doesn't.
  const setEvents = (next: string[]) =>
    set('events', next.includes('item.*') ? next.filter((e) => e === 'item.*' || !e.startsWith('item.')) : next)
  const digestOptions = DIGEST_PRESETS.includes(values.digest_window_seconds)
    ? DIGEST_PRESETS
    : [...DIGEST_PRESETS, values.digest_window_seconds].toSorted((a, b) => a - b)
  const visibleCollections = (collections.data ?? []).filter((c) => !c.archived || values.collection_ids.includes(c.id))
  const approverHelp: Record<string, string> = {
    telegram: 'Numeric Telegram user IDs (send /chatid to the bot). Empty = only the private chat owner.',
    slack:
      'Slack member IDs (U...; Profile → ⋯ → Copy member ID). Required for buttons — empty means nobody can decide in Slack.',
    discord: "Discord user IDs (Developer Mode → Copy User ID). Only these users' button presses count.",
  }

  return (
    <form
      ref={formRef}
      onSubmit={submit}
      noValidate
      className="flex min-h-0 flex-1 flex-col"
      data-testid="channel-form"
    >
      <div className="grid min-w-0 flex-1 grid-cols-1 content-start gap-6 overflow-y-auto overscroll-contain p-4">
        <ActiveToggle
          id={`${uid}-enabled`}
          checked={values.enabled}
          onChange={(v) => set('enabled', v)}
          on="Matching events send a message here."
          off="Nothing is sent or queued until you turn it back on."
        />
        {!editing && (
          <Section title="Type">
            <TypePicker value={type} onChange={pickType} />
          </Section>
        )}
        <ChannelSetupGuide key={type} type={type} defaultOpen={!editing} />

        <TextField
          id={`${uid}-name`}
          label="Name"
          required
          value={values.name}
          onChange={(v) => set('name', v)}
          error={errors.name}
          placeholder="My phone"
        />

        <Section title="Connection">
          {type === 'telegram' && (
            <>
              <SecretInput {...secretProps('bot_token')} label="Bot token" required placeholder="123456:ABC-DEF..." />
              <TextField
                id={`${uid}-chat`}
                label="Chat ID"
                required
                mono
                value={values.chat_id}
                onChange={(v) => set('chat_id', v)}
                error={errors.chat_id}
                placeholder="123456789 or -100... for a group"
                description="Your private chat with the bot, a group (negative ID) or @channelname."
              />
              <Field data-invalid={!!errors.mode}>
                <FieldLabel htmlFor={`${uid}-mode`}>Receive button presses by</FieldLabel>
                <NativeSelect
                  id={`${uid}-mode`}
                  value={values.mode}
                  onChange={(e) => set('mode', e.target.value as ChannelFormValues['mode'])}
                  aria-invalid={!!errors.mode}
                  className="sm:max-w-72"
                >
                  <option value="polling">Polling (works on a laptop)</option>
                  <option value="webhook">Webhook (public HTTPS API)</option>
                </NativeSelect>
                <FieldError>{errors.mode}</FieldError>
                {values.mode === 'webhook' && isLocal && (
                  <Callout size="xs">
                    Webhook mode needs Telegram to reach your API over HTTPS. On a laptop, use polling.
                  </Callout>
                )}
              </Field>
            </>
          )}

          {(type === 'slack' || type === 'discord') && (
            <>
              <DeliveryToggle
                value={values.delivery}
                onChange={(d) => set('delivery', d)}
                botLabel={type === 'slack' ? 'Bot token + channel' : 'Bot (buttons)'}
              />
              {values.delivery === 'webhook' ? (
                <SecretInput
                  {...secretProps('webhook_url')}
                  label="Webhook URL"
                  required
                  url
                  placeholder={
                    type === 'slack' ? 'https://hooks.slack.com/services/...' : 'https://discord.com/api/webhooks/...'
                  }
                />
              ) : (
                <>
                  <SecretInput
                    {...secretProps('bot_token')}
                    label="Bot token"
                    required
                    placeholder={type === 'slack' ? 'xoxb-...' : 'Bot token from the Developer Portal'}
                    description={type === 'slack' ? 'Needs the chat:write scope.' : undefined}
                  />
                  <TextField
                    id={`${uid}-channel`}
                    label="Channel ID"
                    required
                    mono
                    value={values.channel}
                    onChange={(v) => set('channel', v)}
                    error={errors.channel}
                    placeholder={type === 'slack' ? 'C0123ABCD' : '123456789012345678'}
                  />
                  {type === 'discord' && (
                    <div className="grid gap-4 sm:grid-cols-2">
                      <TextField
                        id={`${uid}-app`}
                        label="Application ID"
                        optional
                        mono
                        inputMode="numeric"
                        value={values.application_id}
                        onChange={(v) => set('application_id', v)}
                        error={errors.application_id}
                      />
                      <TextField
                        id={`${uid}-pk`}
                        label="Public key"
                        optional
                        mono
                        value={values.public_key}
                        onChange={(v) => set('public_key', v)}
                        error={errors.public_key}
                        placeholder="64 hex characters"
                      />
                    </div>
                  )}
                </>
              )}
              {type === 'slack' && (
                <SecretInput
                  {...secretProps('signing_secret')}
                  label="Signing secret"
                  optional
                  description="Enables Approve / Reject buttons inside Slack (Basic Information → Signing Secret)."
                />
              )}
              {isLocal && (
                <Callout size="xs">
                  Buttons inside {CHANNEL_TYPE_INFO[type].label} need a public HTTPS URL. On a laptop, messages carry
                  one-tap links instead.
                </Callout>
              )}
            </>
          )}

          {(type === 'telegram' || type === 'slack' || type === 'discord') && (
            <Field data-invalid={!!errors.approver_user_ids}>
              <FieldLabel htmlFor={`${uid}-approvers`}>
                Who may decide{' '}
                {type !== 'slack' && <span className="font-normal text-muted-foreground">(optional)</span>}
              </FieldLabel>
              <ChipsInput
                id={`${uid}-approvers`}
                value={values.approver_user_ids}
                onChange={(v) => set('approver_user_ids', v)}
                label="Approver user IDs"
                placeholder="Add an ID and press Enter"
                invalid={!!errors.approver_user_ids}
                max={100}
                maxLength={64}
              />
              <FieldDescription className="text-xs">{approverHelp[type]}</FieldDescription>
              {errors.approver_user_ids && <FieldError>{errors.approver_user_ids}</FieldError>}
            </Field>
          )}

          {type === 'ntfy' && (
            <>
              <TextField
                id={`${uid}-server`}
                label="Server"
                mono
                type="url"
                value={values.server_url}
                onChange={(v) => set('server_url', v)}
                error={errors.server_url}
                placeholder="https://ntfy.sh"
              />
              <Field data-invalid={!!errors.topic}>
                <FieldLabel htmlFor={`${uid}-topic`} required>
                  Topic
                </FieldLabel>
                <div className="flex gap-2">
                  <Input
                    id={`${uid}-topic`}
                    value={values.topic}
                    onChange={(e) => set('topic', e.target.value)}
                    placeholder="staqly-7f3k9q2..."
                    autoComplete="off"
                    spellCheck={false}
                    autoCapitalize="off"
                    className="min-w-0 font-mono text-sm"
                    aria-invalid={!!errors.topic}
                    aria-required
                  />
                  <Button
                    type="button"
                    variant="outline"
                    className="shrink-0"
                    onClick={() => set('topic', randomTopic())}
                  >
                    <RefreshCw /> Random
                  </Button>
                </div>
                {errors.topic && <FieldError>{errors.topic}</FieldError>}
              </Field>
              <SecretInput
                {...secretProps('access_token')}
                label="Access token"
                optional
                placeholder="tk_..."
                description="For protected topics; sent as a Bearer token."
              />
              <Field data-invalid={!!errors.priority}>
                <FieldLabel htmlFor={`${uid}-prio`}>Priority</FieldLabel>
                <NativeSelect
                  id={`${uid}-prio`}
                  value={values.priority}
                  onChange={(e) => set('priority', e.target.value)}
                  aria-invalid={!!errors.priority}
                  className="sm:max-w-60"
                >
                  <option value="">Default</option>
                  <option value="1">1 · min</option>
                  <option value="2">2 · low</option>
                  <option value="3">3 · default</option>
                  <option value="4">4 · high</option>
                  <option value="5">5 · urgent</option>
                </NativeSelect>
                <FieldError>{errors.priority}</FieldError>
              </Field>
            </>
          )}

          {type === 'email' && (
            <>
              <div className="grid gap-4 sm:grid-cols-[1fr_7rem]">
                <TextField
                  id={`${uid}-host`}
                  label="SMTP host"
                  required
                  mono
                  value={values.host}
                  onChange={(v) => set('host', v)}
                  error={errors.host}
                  placeholder="smtp.example.com"
                />
                <TextField
                  id={`${uid}-port`}
                  label="Port"
                  optional
                  inputMode="numeric"
                  value={values.port}
                  onChange={(v) => set('port', v.replace(/[^0-9]/g, ''))}
                  error={errors.port}
                  placeholder={values.tls === 'tls' ? '465' : '587'}
                />
              </div>
              <Field data-invalid={!!errors.tls}>
                <FieldLabel htmlFor={`${uid}-tls`}>Security</FieldLabel>
                <NativeSelect
                  id={`${uid}-tls`}
                  value={values.tls}
                  onChange={(e) => set('tls', e.target.value as ChannelFormValues['tls'])}
                  aria-invalid={!!errors.tls}
                  className="sm:max-w-72"
                >
                  <option value="starttls">STARTTLS (port 587)</option>
                  <option value="tls">TLS (port 465)</option>
                  <option value="none">None — trusted local relay only</option>
                </NativeSelect>
                <FieldError>{errors.tls}</FieldError>
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <TextField
                  id={`${uid}-user`}
                  label="Username"
                  optional
                  value={values.username}
                  onChange={(v) => set('username', v)}
                  error={errors.username}
                />
                <SecretInput {...secretProps('password')} label="Password" optional />
              </div>
              <TextField
                id={`${uid}-from`}
                label="From"
                required
                value={values.from}
                onChange={(v) => set('from', v)}
                error={errors.from}
                placeholder="staqly <bot@example.com>"
              />
              <Field data-invalid={!!errors.to}>
                <FieldLabel htmlFor={`${uid}-to`} required>
                  To
                </FieldLabel>
                <ChipsInput
                  id={`${uid}-to`}
                  value={values.to}
                  onChange={(v) => set('to', v)}
                  label="Recipients"
                  required
                  placeholder="me@example.com"
                  invalid={!!errors.to}
                  max={20}
                  maxLength={254}
                />
                <FieldDescription className="text-xs">Up to 20 addresses.</FieldDescription>
                {errors.to && <FieldError>{errors.to}</FieldError>}
              </Field>
            </>
          )}

          {channel?.integration_url && (
            <Field>
              <FieldLabel>Integration URL</FieldLabel>
              <div className="flex min-w-0 items-center gap-1 rounded-lg border bg-muted/40 py-1 pr-1 pl-2.5">
                <code className="min-w-0 flex-1 truncate font-mono text-xs">{channel.integration_url}</code>
                <CopyButton value={channel.integration_url} label="Copy integration URL" />
              </div>
              <FieldDescription className="text-xs">
                {type === 'telegram'
                  ? 'Registered with Telegram automatically.'
                  : `Paste this into your ${CHANNEL_TYPE_INFO[type].label} app settings.`}
              </FieldDescription>
            </Field>
          )}
        </Section>

        <Section
          title="What to send"
          hint="By default: new items waiting for review."
          required
          invalid={!!errors.events}
        >
          <Field data-invalid={!!errors.events}>
            <MultiSelect
              id={`${uid}-ev`}
              label="Events"
              value={values.events}
              onChange={setEvents}
              options={eventOptions}
              placeholder="Choose events..."
              invalid={!!errors.events}
              mono
            />
            {errors.events && <FieldError>{errors.events}</FieldError>}
          </Field>
          <Field data-invalid={!!errors.collection_ids}>
            <FieldLabel htmlFor={`${uid}-col`}>Collections</FieldLabel>
            <MultiSelect
              id={`${uid}-col`}
              label="Collections"
              value={values.collection_ids}
              onChange={(v) => set('collection_ids', v)}
              options={visibleCollections.map((c) => ({ value: c.id, label: c.name }))}
              anyLabel="All collections, including new ones"
              invalid={!!errors.collection_ids}
            />
            <FieldError>{errors.collection_ids}</FieldError>
          </Field>
          <Field data-invalid={!!errors.digest_window_seconds}>
            <FieldLabel htmlFor={`${uid}-digest`}>Delivery</FieldLabel>
            <NativeSelect
              id={`${uid}-digest`}
              value={String(values.digest_window_seconds)}
              onChange={(e) => set('digest_window_seconds', Number(e.target.value))}
              aria-invalid={!!errors.digest_window_seconds}
              className="sm:max-w-72"
            >
              {digestOptions.map((s) => (
                <option key={s} value={s}>
                  {digestLabel(s)}
                </option>
              ))}
            </NativeSelect>
            <FieldDescription className="text-xs">
              A digest collects everything in the window into one message (buttons for the first 10 items).
            </FieldDescription>
            <FieldError>{errors.digest_window_seconds}</FieldError>
          </Field>
        </Section>
      </div>

      <div className="flex justify-end gap-2 border-t bg-background/95 p-4 pb-[max(1rem,env(safe-area-inset-bottom))] backdrop-blur">
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? 'Saving...' : editing ? 'Save channel' : 'Create channel'}
        </Button>
      </div>
    </form>
  )
}
