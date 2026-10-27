'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CalendarClock, Ellipsis, Pencil, Play, Plus, Repeat, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { ChannelStack } from '@/components/channels/channel-avatars'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { DateTimePicker } from '@/components/common/date-time-picker'
import { ErrorState } from '@/components/common/error-state'
import { MultiSelect } from '@/components/common/multi-select'
import { SecretField } from '@/components/common/secret-field'
import { SkeletonList } from '@/components/common/skeleton-list'
import { TimeZoneSelect } from '@/components/common/time-zone-select'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { useIsPast } from '@/hooks/use-is-past'
import { api, type Collection, type Schedule, type ScheduleInput } from '@/lib/api'
import { apiFieldErrors } from '@/lib/form-errors'
import { formatDateTime, timeAgo } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { qk, refetchPostListsFromStart, useChannels, useSchedules, useSession } from '@/lib/queries'
import { formatSchedule } from '@/lib/schedule'
import { cn } from '@/lib/utils'

const WEEKDAYS = [
  { value: 1, label: 'Mon' },
  { value: 2, label: 'Tue' },
  { value: 3, label: 'Wed' },
  { value: 4, label: 'Thu' },
  { value: 5, label: 'Fri' },
  { value: 6, label: 'Sat' },
  // The API numbers days 0 (Sunday) to 6; the picker still shows Sunday last.
  { value: 0, label: 'Sun' },
]

/** "Mon to Fri at 09:00 and 17:00", "every 4 h", "once Tue 21 Oct, 09:00". */
export function describeSchedule(s: Pick<Schedule, 'kind' | 'run_at' | 'rule'>) {
  if (s.kind === 'once') {
    return s.run_at ? `Once, ${formatSchedule(s.run_at)}` : 'Once'
  }

  const r = s.rule ?? {}

  if (r.every_minutes) {
    return r.every_minutes % 60 === 0 ? `Every ${r.every_minutes / 60} h` : `Every ${r.every_minutes} min`
  }

  const days = r.weekdays ?? []
  const dayText =
    days.length === 7 || days.length === 0
      ? 'Every day'
      : days.length === 5 && !days.includes(0) && !days.includes(6)
        ? 'Mon to Fri'
        : WEEKDAYS.filter((w) => days.includes(w.value))
            .map((w) => w.label)
            .join(', ')

  return `${dayText} at ${(r.times ?? []).join(', ') || '09:00'}`
}

const PICK_LABELS: Record<string, string> = {
  next: 'the next waiting post',
  all: 'every waiting post',
  selected: 'the chosen posts',
}

interface FormState {
  name: string
  kind: 'once' | 'recurring'
  runAt: Date | null
  mode: 'times' | 'every'
  times: string[]
  weekdays: number[]
  tz: string
  everyHours: string
  channels: string[]
  pick: 'next' | 'all' | 'selected'
  postIds: string
  source: 'none' | 'agent' | 'endpoint'
  endpointUrl: string
  endpointSecret: string
}

/** zone: the workspace's, what a new rule starts in. */
function fromSchedule(s: Schedule | null, collection: Collection, zone: string): FormState {
  const r = s?.rule ?? {}

  return {
    name: s?.name ?? `${collection.name} schedule`,
    kind: s?.kind ?? 'recurring',
    runAt: s?.run_at ? new Date(s.run_at) : null,
    mode: r.every_minutes ? 'every' : 'times',
    times: r.times?.length ? r.times : ['09:00'],
    weekdays: r.weekdays?.length ? r.weekdays : [1, 2, 3, 4, 5],
    tz: r.tz || zone,
    everyHours: r.every_minutes ? String(Math.max(1, Math.round(r.every_minutes / 60))) : '4',
    channels: s?.channels ?? [],
    pick: s?.pick ?? 'next',
    postIds: (s?.post_ids ?? []).join(', '),
    source: s?.source ?? 'none',
    endpointUrl: s?.endpoint_url ?? '',
    endpointSecret: '',
  }
}

function toInput(f: FormState, collectionId: string): ScheduleInput {
  return {
    collection: collectionId,
    name: f.name.trim(),
    kind: f.kind,
    run_at: f.kind === 'once' && f.runAt ? f.runAt.toISOString() : undefined,
    rule:
      f.kind === 'recurring'
        ? f.mode === 'every'
          ? { every_minutes: Math.max(1, Number(f.everyHours) || 1) * 60 }
          : { times: f.times, weekdays: f.weekdays, tz: f.tz }
        : undefined, // a one-time schedule has only its time
    channels: f.channels,
    pick: f.pick,
    post_ids: f.pick === 'selected' ? f.postIds.split(/[\s,]+/).filter(Boolean) : [],
    source: f.source,
    endpoint_url: f.source === 'endpoint' ? f.endpointUrl.trim() : '',
    ...(f.source === 'endpoint' && f.endpointSecret ? { endpoint_secret: f.endpointSecret } : {}),
    enabled: true,
  }
}

/** Times of day as chips with an add field: "09:00", "17:30". */
function TimesInput({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const [draft, setDraft] = useState('')
  const add = () => {
    const m = /^(\d{1,2}):(\d{2})$/.exec(draft.trim())

    if (!m) {
      return
    }

    const t = `${m[1]!.padStart(2, '0')}:${m[2]}`

    if (Number(m[1]) < 24 && Number(m[2]) < 60 && !value.includes(t)) {
      onChange([...value, t].toSorted())
    }

    setDraft('')
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {value.map((t) => (
        <span
          key={t}
          className="inline-flex h-8 items-center gap-1 rounded-full border bg-muted/50 pr-1 pl-2.5 text-sm tabular-nums"
        >
          {t}
          <button
            type="button"
            aria-label={`Remove ${t}`}
            onClick={() => onChange(value.filter((x) => x !== t))}
            className="flex size-6 items-center justify-center rounded-full hover:bg-muted pointer-coarse:size-10"
          >
            ×
          </button>
        </span>
      ))}
      <Input
        type="time"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={add}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            add()
          }
        }}
        aria-label="Add a time"
        className="h-8 w-28"
      />
    </div>
  )
}

/** New schedule / Edit schedule drawer. */
function ScheduleSheet({
  collection,
  schedule,
  open,
  onOpenChange,
}: {
  collection: Collection
  schedule: Schedule | null
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const qc = useQueryClient()
  const channels = useChannels()
  const zone = useSession().data?.workspace.timezone || 'UTC'
  const [f, setF] = useState<FormState>(() => fromSchedule(schedule, collection, zone))
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((s) => ({ ...s, [k]: v }))
  // The endpoint secret the server generated: the API shows it in full only in this response.
  const [freshSecret, setFreshSecret] = useState<string | null>(null)
  const close = (o: boolean) => {
    if (!o) {
      setFreshSecret(null)
    }

    onOpenChange(o)
  }
  const save = useMutation({
    mutationFn: () => {
      const body = toInput(f, collection.id)

      return schedule ? api.updateSchedule(schedule.id, body) : api.createSchedule(body)
    },
    onSuccess: (s) => {
      void qc.invalidateQueries({ queryKey: qk.schedulesAll })
      namedToast(
        s.name,
        schedule ? 'was updated' : 'was created',
        s.next_run_at ? `Next run ${formatSchedule(s.next_run_at)}` : undefined,
      )

      if (s.source === 'endpoint' && s.endpoint_secret && !s.endpoint_secret.includes('*') && !f.endpointSecret) {
        setFreshSecret(s.endpoint_secret)

        return
      }

      onOpenChange(false)
    },
    onError: (e) => {
      const { errors: fields, complete } = apiFieldErrors<string>(e, (k) => k)

      setErrors(fields)

      if (!complete) {
        toastApiError(e)
      }
    },
  })
  // One timer until the picked time, not a clock re-rendering the form every second.
  const runAtPast = useIsPast(f.kind === 'once' ? f.runAt : null)
  const connected = (channels.data ?? []).filter((c) => c.status !== 'disabled')
  const inPast = f.kind === 'once' && f.runAt !== null && runAtPast

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle>{schedule ? 'Edit schedule' : 'New schedule'}</SheetTitle>
          <SheetDescription>
            A schedule publishes the waiting posts of this collection for you: once, or at fixed times every week.
          </SheetDescription>
        </SheetHeader>
        {freshSecret && (
          <div className="grid content-start gap-3 p-4">
            <div className="grid gap-2 rounded-lg border border-brand/40 bg-brand-soft/40 p-3" role="status">
              <div className="text-xs font-medium">Copy the signing secret now. It is not shown again.</div>
              <SecretField value={freshSecret} label="Signing secret" />
              <p className="text-xs text-muted-foreground">
                Your endpoint checks the signature of each request with it, the same way as for webhooks.
              </p>
              <Button size="xs" variant="outline" className="justify-self-end" onClick={() => close(false)}>
                Done
              </Button>
            </div>
          </div>
        )}
        <form
          className={cn('flex min-h-0 flex-1 flex-col', freshSecret && 'hidden')}
          noValidate
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
          data-testid="schedule-form"
        >
          <div className="grid min-h-0 flex-1 content-start gap-5 overflow-y-auto overscroll-contain p-4">
            <Field data-invalid={!!errors.name}>
              <FieldLabel htmlFor="schedule-name" required>
                Name
              </FieldLabel>
              <Input
                id="schedule-name"
                value={f.name}
                onChange={(e) => set('name', e.target.value)}
                aria-invalid={!!errors.name}
                aria-required
              />
              <FieldError>{errors.name}</FieldError>
            </Field>

            <Field data-invalid={!!errors.run_at || !!errors.rule || inPast}>
              <FieldLabel>When</FieldLabel>
              <RadioGroup
                value={f.kind}
                onValueChange={(v) => set('kind', v as FormState['kind'])}
                className="grid gap-2"
              >
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="recurring" /> At fixed times
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="once" /> Once, at a time
                </label>
              </RadioGroup>
              {f.kind === 'once' ? (
                <div className="mt-2">
                  <DateTimePicker id="schedule-run-at" value={f.runAt} onChange={(v) => set('runAt', v)} />
                </div>
              ) : (
                <div className="mt-2 grid gap-3 rounded-xl border p-3">
                  <RadioGroup
                    value={f.mode}
                    onValueChange={(v) => set('mode', v as FormState['mode'])}
                    className="grid gap-2"
                  >
                    <label className="flex items-center gap-2 text-sm">
                      <RadioGroupItem value="times" /> Times of day
                    </label>
                    <label className="flex items-center gap-2 text-sm">
                      <RadioGroupItem value="every" /> Every few hours
                    </label>
                  </RadioGroup>
                  {f.mode === 'times' ? (
                    <>
                      <TimesInput value={f.times} onChange={(v) => set('times', v)} />
                      <div className="flex flex-wrap gap-1.5" role="group" aria-label="Weekdays">
                        {WEEKDAYS.map((d) => {
                          const on = f.weekdays.includes(d.value)

                          return (
                            <button
                              key={d.value}
                              type="button"
                              role="checkbox"
                              aria-checked={on}
                              onClick={() =>
                                set(
                                  'weekdays',
                                  on
                                    ? f.weekdays.filter((x) => x !== d.value)
                                    : [...f.weekdays, d.value].toSorted((x, y) => x - y),
                                )
                              }
                              className={cn(
                                'h-8 min-w-11 rounded-full border px-2.5 text-sm transition-colors pointer-coarse:h-10',
                                on ? 'border-foreground bg-foreground text-background' : 'bg-background hover:bg-muted',
                              )}
                            >
                              {d.label}
                            </button>
                          )
                        })}
                      </div>
                      <div className="grid gap-1.5">
                        <Label htmlFor="schedule-tz" className="text-xs text-muted-foreground">
                          Time zone
                        </Label>
                        <TimeZoneSelect
                          id="schedule-tz"
                          value={f.tz}
                          onChange={(v) => set('tz', v)}
                          className="w-full sm:w-72"
                        />
                      </div>
                    </>
                  ) : (
                    <div className="flex items-center gap-2 text-sm">
                      Every
                      <Input
                        type="number"
                        min={1}
                        max={168}
                        value={f.everyHours}
                        onChange={(e) => set('everyHours', e.target.value)}
                        aria-label="Hours between runs"
                        className="h-8 w-20"
                      />
                      hours
                    </div>
                  )}
                </div>
              )}
              <FieldError>
                {errors.run_at ?? errors.rule ?? (inPast ? 'Pick a time in the future' : undefined)}
              </FieldError>
            </Field>

            <Field data-invalid={!!errors.channels}>
              <FieldLabel>Channels</FieldLabel>
              <MultiSelect
                label="Channels"
                value={f.channels}
                onChange={(v) => set('channels', v)}
                options={connected.map((c) => ({ value: c.id, label: c.name }))}
                anyLabel="Pick channels"
                className="w-full"
              />
              <FieldDescription className="text-xs">
                Posts without channels of their own go to these. A post that already has channels keeps them.
              </FieldDescription>
              <FieldError>{errors.channels}</FieldError>
            </Field>

            <Field data-invalid={!!errors.pick || !!errors.post_ids}>
              <FieldLabel>What to publish</FieldLabel>
              <RadioGroup
                value={f.pick}
                onValueChange={(v) => set('pick', v as FormState['pick'])}
                className="grid gap-2"
              >
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="next" /> The next waiting post (oldest first)
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="all" /> Every waiting post
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="selected" /> Chosen posts
                </label>
              </RadioGroup>
              {f.pick === 'selected' && (
                <Input
                  value={f.postIds}
                  onChange={(e) => set('postIds', e.target.value)}
                  placeholder="Post ids, separated by commas"
                  aria-label="Post ids"
                  className="mt-2 font-mono text-xs"
                />
              )}
              <FieldDescription className="text-xs">
                A waiting post is a draft of this collection without a time of its own.
              </FieldDescription>
              <FieldError>{errors.pick ?? errors.post_ids}</FieldError>
            </Field>

            <Field data-invalid={!!errors.endpoint_url}>
              <FieldLabel>When nothing is waiting</FieldLabel>
              <RadioGroup
                value={f.source}
                onValueChange={(v) => set('source', v as FormState['source'])}
                className="grid gap-2"
              >
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="none" /> Do nothing
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="agent" /> Ask my agent for new posts
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <RadioGroupItem value="endpoint" /> Call an endpoint that returns posts
                </label>
              </RadioGroup>
              {f.source === 'agent' && (
                <FieldDescription className="text-xs">
                  Staqly sends a request to your webhooks and lists it for agents connected through MCP. The posts they
                  add to this collection go out on the next run.
                </FieldDescription>
              )}
              {f.source === 'endpoint' && (
                <div className="mt-2 grid gap-3">
                  <div className="grid gap-1.5">
                    <Label htmlFor="schedule-endpoint-url" required>
                      Endpoint URL
                    </Label>
                    <Input
                      id="schedule-endpoint-url"
                      type="url"
                      value={f.endpointUrl}
                      onChange={(e) => set('endpointUrl', e.target.value)}
                      placeholder="https://example.com/posts"
                      aria-required
                      aria-invalid={!!errors.endpoint_url}
                    />
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="schedule-endpoint-secret">
                      Secret <span className="font-normal text-muted-foreground">(optional)</span>
                    </Label>
                    <Input
                      id="schedule-endpoint-secret"
                      type="password"
                      value={f.endpointSecret}
                      onChange={(e) => set('endpointSecret', e.target.value)}
                      placeholder={schedule?.endpoint_url ? 'Unchanged if empty' : 'Generated if empty'}
                      autoComplete="off"
                    />
                  </div>
                  <FieldDescription className="text-xs">
                    Staqly sends a signed request with the collection, the channels and the slot; the reply is a list of
                    posts, which are created and published in the same run.
                  </FieldDescription>
                </div>
              )}
              <FieldError>{errors.endpoint_url}</FieldError>
            </Field>
          </div>
          <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
            <Button type="button" variant="outline" onClick={() => close(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={save.isPending || !f.name.trim() || inPast || (f.kind === 'once' && !f.runAt)}
            >
              {save.isPending ? 'Saving...' : schedule ? 'Save changes' : 'Create schedule'}
            </Button>
          </div>
        </form>
      </SheetContent>
    </Sheet>
  )
}

function ScheduleRow({
  schedule,
  collection,
  onEdit,
}: {
  schedule: Schedule
  collection: Collection
  onEdit: () => void
}) {
  const qc = useQueryClient()
  const channels = useChannels()
  const [remove, setRemove] = useState(false)
  const refresh = () => qc.invalidateQueries({ queryKey: qk.schedulesAll })
  const toggle = useMutation({
    mutationFn: () => api.updateSchedule(schedule.id, { enabled: !schedule.enabled }),
    onSuccess: refresh,
    onError: (e) => toastApiError(e),
  })
  const run = useMutation({
    mutationFn: () => api.runSchedule(schedule.id),
    onSuccess: (s) => {
      void refresh()
      // Every list from its first page: invalidating would reload each loaded page one by one.
      refetchPostListsFromStart(qc)
      namedToast(s.name, 'ran', s.last_result || undefined)
    },
    onError: (e) => toastApiError(e),
  })
  const del = useMutation({
    mutationFn: () => api.deleteSchedule(schedule.id),
    onSuccess: () => {
      void refresh()
      namedToast(schedule.name, 'was deleted')
      setRemove(false)
    },
    onError: (e) => toastApiError(e),
  })
  const picked = (channels.data ?? []).filter((c) => schedule.channels.includes(c.id))

  return (
    <li className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 px-3 py-2.5" data-testid="schedule-row">
      <Switch
        checked={schedule.enabled}
        onCheckedChange={() => toggle.mutate()}
        aria-label={`${schedule.name} on`}
        disabled={toggle.isPending}
      />
      <span className="min-w-0">
        <span className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium">{schedule.name}</span>
          <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
            {schedule.kind === 'recurring' ? (
              <Repeat className="size-3" aria-hidden />
            ) : (
              <CalendarClock className="size-3" aria-hidden />
            )}
            {describeSchedule(schedule)}
          </span>
        </span>
        <span className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
          <span>Publishes {PICK_LABELS[schedule.pick]}</span>
          {schedule.source !== 'none' && (
            <span>· asks {schedule.source === 'agent' ? 'your agent' : 'an endpoint'} when nothing waits</span>
          )}
          {schedule.enabled && schedule.next_run_at && (
            <span title={formatDateTime(schedule.next_run_at)}>· next {timeAgo(schedule.next_run_at)}</span>
          )}
          {schedule.last_run_at && (
            <span title={formatDateTime(schedule.last_run_at)}>· last ran {timeAgo(schedule.last_run_at)}</span>
          )}
        </span>
        {schedule.last_result && (
          <span className="mt-0.5 block text-xs text-muted-foreground">{schedule.last_result}</span>
        )}
      </span>
      <div className="flex items-center gap-1.5">
        <ChannelStack channels={picked} size="size-6" />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" aria-label={`Actions for ${schedule.name}`}>
              <Ellipsis />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={onEdit}>
              <Pencil /> Edit
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => run.mutate()} disabled={run.isPending}>
              <Play /> Run now
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => setRemove(true)}>
              <Trash2 /> Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <ConfirmDialog
        open={remove}
        onOpenChange={setRemove}
        title={`Delete ${schedule.name}?`}
        description={`Posts in ${collection.name} stay as they are; nothing publishes them on this schedule any more.`}
        confirmLabel={del.isPending ? 'Deleting...' : 'Delete'}
        variant="destructive"
        onConfirm={() => del.mutate()}
      />
    </li>
  )
}

/** The collection's schedules, in its settings drawer. */
export function SchedulesSection({ collection }: { collection: Collection }) {
  const schedules = useSchedules(collection.id)
  const [editing, setEditing] = useState<{ open: boolean; schedule: Schedule | null; key: number }>({
    open: false,
    schedule: null,
    key: 0,
  })
  const openNew = () => setEditing((e) => ({ open: true, schedule: null, key: e.key + 1 }))
  const openEdit = (s: Schedule) => setEditing((e) => ({ open: true, schedule: s, key: e.key + 1 }))

  return (
    <div className="grid gap-3" data-testid="schedules">
      {schedules.isLoading ? (
        <SkeletonList count={2} className="h-12 rounded-lg" />
      ) : schedules.error && !schedules.data ? (
        <ErrorState
          title="Could not load the schedules"
          error={schedules.error}
          onRetry={() => void schedules.refetch()}
        />
      ) : (schedules.data?.length ?? 0) === 0 ? (
        <p className="text-sm text-muted-foreground">
          No schedule yet. Posts go out at their own time, or when you publish them. A schedule can publish the waiting
          posts for you, every day at 09:00 for example, and ask your agent for more when the queue is empty.
        </p>
      ) : (
        <ul className="divide-y rounded-xl border">
          {schedules.data!.map((s) => (
            <ScheduleRow key={s.id} schedule={s} collection={collection} onEdit={() => openEdit(s)} />
          ))}
        </ul>
      )}
      <div>
        <Button size="sm" variant="outline" onClick={openNew} data-testid="new-schedule">
          <Plus /> New schedule
        </Button>
      </div>
      {editing.key > 0 && (
        <ScheduleSheet
          key={editing.key}
          collection={collection}
          schedule={editing.schedule}
          open={editing.open}
          onOpenChange={(o) => setEditing((e) => ({ ...e, open: o }))}
        />
      )}
    </div>
  )
}
