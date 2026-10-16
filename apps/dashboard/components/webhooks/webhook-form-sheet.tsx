'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { ActiveToggle } from '@/components/common/active-toggle'
import { MultiSelect, type MultiOption } from '@/components/common/multi-select'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { api, type Webhook, type WebhookInput } from '@/lib/api'
import { focusFirstInvalid, formApiError, useFormErrors } from '@/lib/form-errors'
import { qk, useCollections } from '@/lib/queries'

/** Wildcards at the top; a wildcard covers (and greys out) the events it matches. */
const WILDCARD_EVENTS: { value: string; hint: string }[] = [
  { value: '*', hint: 'Every event' },
  { value: 'item.*', hint: 'All item events' },
  { value: 'collection.*', hint: 'All collection events' },
]

const ITEM_EVENT_GROUPS: { title: string; events: { value: string; hint: string }[] }[] = [
  {
    title: 'Decisions',
    events: [
      { value: 'item.approved', hint: 'Reviewer approved' },
      { value: 'item.rejected', hint: 'Reviewer rejected (with feedback)' },
      { value: 'item.expired', hint: 'Nobody decided in time' },
    ],
  },
  {
    title: 'Runs',
    events: [
      { value: 'item.completed', hint: 'Your automation finished' },
      { value: 'item.failed', hint: 'It failed or timed out' },
    ],
  },
  {
    title: 'Item changes',
    events: [
      { value: 'item.created', hint: 'An agent added an item' },
      { value: 'item.updated', hint: 'Fields edited' },
      { value: 'item.status_changed', hint: 'Status set through the API' },
      { value: 'item.deleted', hint: 'Item deleted' },
    ],
  },
]

const ERROR_KEYS = ['url', 'description', 'events', 'collection_ids', 'timeout_ms', 'max_attempts'] as const

type ErrorKey = (typeof ERROR_KEYS)[number]

export function WebhookFormSheet({
  open,
  onOpenChange,
  webhook,
  onSaved,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  webhook?: Webhook | null
  onSaved?: (w: Webhook) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle>{webhook ? 'Edit webhook' : 'New webhook'}</SheetTitle>
          <SheetDescription>
            staqly POSTs signed JSON to this URL when matching events happen, retrying with backoff on failure.
          </SheetDescription>
        </SheetHeader>
        {open && (
          <WebhookForm
            key={webhook?.id ?? 'new'}
            webhook={webhook ?? null}
            onCancel={() => onOpenChange(false)}
            onSaved={(w) => {
              onOpenChange(false)
              onSaved?.(w)
            }}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

function WebhookForm({
  webhook,
  onCancel,
  onSaved,
}: {
  webhook: Webhook | null
  onCancel: () => void
  onSaved: (w: Webhook) => void
}) {
  const qc = useQueryClient()
  const collections = useCollections(true)
  const [url, setUrl] = useState(webhook?.url ?? '')
  const [description, setDescription] = useState(webhook?.description ?? '')
  const [events, setEvents] = useState<string[]>(webhook?.events ?? ['item.approved', 'item.rejected'])
  const [collectionIds, setCollectionIds] = useState<string[]>(webhook?.collection_ids ?? [])
  const [timeoutS, setTimeoutS] = useState(String((webhook?.timeout_ms ?? 10000) / 1000))
  const [maxAttempts, setMaxAttempts] = useState(String(webhook?.max_attempts ?? 8))
  const [active, setActive] = useState(webhook?.active ?? true)
  const t = Math.round(Number(timeoutS) * 1000)
  const m = Number(maxAttempts)
  const clientErrors: Partial<Record<ErrorKey, string>> = {}

  if (!url.trim()) {
    clientErrors.url = 'Endpoint URL is required'
  } else if (!/^https?:\/\/[^\s]+$/i.test(url.trim())) {
    clientErrors.url = 'Enter an absolute http(s) URL'
  }

  if (!events.length) {
    clientErrors.events = 'Subscribe to at least one event (use * for all)'
  }

  if (!timeoutS.trim() || !Number.isFinite(t) || t < 1000 || t > 60000) {
    clientErrors.timeout_ms = 'Between 1 and 60 seconds'
  }

  if (!Number.isInteger(m) || m < 1 || m > 20) {
    clientErrors.max_attempts = 'Between 1 and 20'
  }

  const form = useFormErrors(clientErrors)
  const errors = form.errors

  const save = useMutation({
    mutationFn: (body: WebhookInput) => (webhook ? api.updateWebhook(webhook.id, body) : api.createWebhook(body)),
    onSuccess: (w) => {
      void qc.invalidateQueries({ queryKey: qk.webhooks })
      toast.success(webhook ? 'Webhook updated' : 'Webhook created')
      onSaved(w)
    },
    onError: (e) =>
      form.setServerErrors(formApiError(e, (k) => (ERROR_KEYS.includes(k as ErrorKey) ? (k as ErrorKey) : null))),
  })

  function changeEvents(next: string[]) {
    setEvents(next)
    form.touch('events')
  }

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()

    if (!form.submit()) {
      focusFirstInvalid(e.currentTarget)

      return
    }

    save.mutate({
      url: url.trim(),
      description: description.trim(),
      // A wildcard already covers what it matches: send only what isn't covered.
      events: events.includes('*')
        ? ['*']
        : events.filter((ev) => !(events.includes('item.*') && ev.startsWith('item.') && ev !== 'item.*')),
      collection_ids: collectionIds,
      timeout_ms: t,
      max_attempts: m,
      active,
    })
  }

  // Action events come from the collections' actions (Publish, Regenerate...), each named after its action.
  const actionEvents = [
    ...new Map(
      (collections.data ?? [])
        .flatMap((c) => c.actions ?? [])
        .map((a) => [a.event, { value: a.event, hint: `${a.label} action` }] as const),
    ).values(),
  ]
  const groups = [...ITEM_EVENT_GROUPS.slice(0, 2), { title: 'Actions', events: actionEvents }, ITEM_EVENT_GROUPS[2]]
  const known = new Set([...WILDCARD_EVENTS, ...groups.flatMap((g) => g.events)].map((e) => e.value))
  // Patterns saved earlier (through the API) stay visible and removable.
  const extraEvents = events.filter((e) => !known.has(e))
  const all = events.includes('*')
  const allItems = all || events.includes('item.*')
  const eventOptions: MultiOption[] = [
    ...WILDCARD_EVENTS.map((ev) => ({
      value: ev.value,
      label: ev.value,
      hint: ev.hint,
      group: 'Wildcards',
      implied: all && ev.value !== '*',
    })),
    ...groups.flatMap((g) =>
      g.events.map((ev) => ({ value: ev.value, label: ev.value, hint: ev.hint, group: g.title, implied: allItems })),
    ),
    ...extraEvents.map((e) => ({ value: e, label: e, group: 'Other patterns', implied: all })),
  ]

  return (
    <form onSubmit={submit} noValidate className="flex min-h-0 flex-1 flex-col" data-testid="webhook-form">
      <div className="grid min-w-0 flex-1 grid-cols-1 content-start gap-5 overflow-y-auto overscroll-contain p-4">
        <ActiveToggle
          id="wh-active"
          checked={active}
          onChange={setActive}
          on="Matching events are delivered to this URL."
          off="Nothing is sent until you turn it back on; events in the meantime are skipped."
        />
        <Field data-invalid={!!errors.url}>
          <FieldLabel htmlFor="wh-url" required>
            Endpoint URL
          </FieldLabel>
          <Input
            id="wh-url"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value)
              form.touch('url')
            }}
            placeholder="http://host.docker.internal:5678/webhook/staqly"
            className="font-mono text-sm"
            aria-invalid={!!errors.url}
            aria-required
          />
          <FieldError>{errors.url}</FieldError>
        </Field>
        <Field data-invalid={!!errors.description}>
          <FieldLabel htmlFor="wh-desc">Description</FieldLabel>
          <Input
            id="wh-desc"
            value={description}
            onChange={(e) => {
              setDescription(e.target.value)
              form.touch('description')
            }}
            placeholder="n8n review loop"
            aria-invalid={!!errors.description}
          />
          <FieldError>{errors.description}</FieldError>
        </Field>
        <Field data-invalid={!!errors.events}>
          <FieldLabel htmlFor="wh-events" required>
            Events
          </FieldLabel>
          <MultiSelect
            id="wh-events"
            label="Events"
            className="w-full"
            mono
            placeholder="Choose events..."
            invalid={!!errors.events}
            summary={all ? 'Every event (*)' : undefined}
            value={events}
            options={eventOptions}
            onChange={changeEvents}
          />
          <FieldError>{errors.events}</FieldError>
        </Field>
        <Field data-invalid={!!errors.collection_ids}>
          <FieldLabel htmlFor="wh-collections">Collections</FieldLabel>
          <MultiSelect
            id="wh-collections"
            label="Collections"
            className="w-full"
            anyLabel="All collections, including new ones"
            value={collectionIds}
            options={(collections.data ?? []).map((c) => ({ value: c.id, label: c.name }))}
            onChange={(v) => {
              setCollectionIds(v)
              form.touch('collection_ids')
            }}
          />
          <FieldError>{errors.collection_ids}</FieldError>
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field data-invalid={!!errors.timeout_ms}>
            <FieldLabel htmlFor="wh-timeout" required>
              Timeout (seconds)
            </FieldLabel>
            <Input
              id="wh-timeout"
              type="number"
              min={1}
              max={60}
              step={1}
              value={timeoutS}
              onChange={(e) => {
                setTimeoutS(e.target.value)
                form.touch('timeout_ms')
              }}
              aria-invalid={!!errors.timeout_ms}
              aria-required
            />
            <FieldError className="text-xs">{errors.timeout_ms}</FieldError>
          </Field>
          <Field data-invalid={!!errors.max_attempts}>
            <FieldLabel htmlFor="wh-attempts" required>
              Max attempts
            </FieldLabel>
            <Input
              id="wh-attempts"
              type="number"
              min={1}
              max={20}
              value={maxAttempts}
              onChange={(e) => {
                setMaxAttempts(e.target.value)
                form.touch('max_attempts')
              }}
              aria-invalid={!!errors.max_attempts}
              aria-required
            />
            <FieldError className="text-xs">{errors.max_attempts}</FieldError>
          </Field>
        </div>
      </div>
      <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? 'Saving...' : webhook ? 'Save webhook' : 'Create webhook'}
        </Button>
      </div>
    </form>
  )
}
