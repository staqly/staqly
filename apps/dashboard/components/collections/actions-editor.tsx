'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, type ActionDef, type Collection, type ItemStatus } from '@/lib/api'
import { focusFirstInvalid, formApiError } from '@/lib/form-errors'
import { qk } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'
import { cn } from '@/lib/utils'

import { StatusMultiSelect } from './status-multi-select'

const NAME_RE = /^[a-z][a-z0-9_]{0,39}$/
const EVENT_RE = /^item\.[a-z0-9_.]{1,60}$/
/** Custom actions may not decide items; approve/reject are built in. */

const BEFORE_DECISION: ItemStatus[] = ['pending', 'draft']
const NONE: ItemStatus[] = []

interface Draft extends ActionDef {
  rowKey: string
}

let keySeq = 0
const withKey = (a: ActionDef): Draft => ({ ...a, rowKey: `a${++keySeq}` })

export interface ActionError {
  /** The input that is wrong, so the editor can mark it. */
  field: 'name' | 'event'
  message: string
}

function validateAction(a: ActionDef, all: ActionDef[]): ActionError | null {
  if (!a.name) {
    return { field: 'name', message: 'Name is required' }
  }

  if (!NAME_RE.test(a.name)) {
    return { field: 'name', message: 'Name must be lowercase letters, digits or _ and start with a letter' }
  }

  if (a.name === 'approve' || a.name === 'reject') {
    return { field: 'name', message: `“${a.name}” is a built-in action` }
  }

  if (all.filter((x) => x.name === a.name).length > 1) {
    return { field: 'name', message: `Duplicate action name “${a.name}”` }
  }

  if (a.event && !EVENT_RE.test(a.event)) {
    return { field: 'event', message: 'Event must look like item.something' }
  }

  return null
}

/** What an action does: starts a run your automation works on, only notifies, or archives. */
type Effect = 'run' | 'notify' | 'archive' | 'other'

function effectOf(a: ActionDef): Effect {
  return a.set_status === 'processing'
    ? 'run'
    : !a.set_status
      ? 'notify'
      : a.set_status === 'archived'
        ? 'archive'
        : 'other'
}

/** One-line summary of an action for its collapsed row. */
function summarize(a: ActionDef): string {
  const effect = effectOf(a)
  const to =
    effect === 'run'
      ? `starts a run (${a.run_labels?.running || 'Processing'})`
      : effect === 'notify'
        ? 'only notifies'
        : effect === 'archive'
          ? 'archives'
          : `sets ${STATUS_META[a.set_status!].label}`
  const from = a.allowed_statuses?.length
    ? `from ${a.allowed_statuses.map((s) => STATUS_META[s].label).join(', ')}`
    : 'from any status'

  return [to, from, a.requires_feedback ? 'feedback required' : ''].filter(Boolean).join(' · ')
}

export function ActionsEditor({ collection }: { collection: Collection }) {
  const qc = useQueryClient()
  const [drafts, setDrafts] = useState<Draft[]>(() => (collection.actions ?? []).map(withKey))
  const [dirty, setDirty] = useState(false)
  // Errors show after the first save attempt and then follow every edit.
  const [submitted, setSubmitted] = useState(false)
  // A 422 from the API names only "actions", not the row: shown under the list.
  const [serverError, setServerError] = useState<string | undefined>()
  // Rows show a one-line summary; only the ones being edited are expanded.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set())

  function setOpen(key: string, open: boolean) {
    setExpanded((prev) => {
      const next = new Set(prev)

      if (open) {
        next.add(key)
      } else {
        next.delete(key)
      }

      return next
    })
  }

  const save = useMutation({
    mutationFn: (actions: ActionDef[]) => api.updateCollection(collection.id, { actions }),
    onSuccess: (c) => {
      qc.setQueryData(qk.collection(c.id), c)
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      void qc.invalidateQueries({ queryKey: ['item'] })
      setDrafts((c.actions ?? []).map(withKey))
      setDirty(false)
      setSubmitted(false)
      toast.success('Actions saved')
    },
    onError: (e) => setServerError(formApiError(e, (k) => (k === 'actions' ? k : null)).actions),
  })

  function update(key: string, patch: Partial<ActionDef>) {
    setDrafts((ds) => ds.map((d) => (d.rowKey === key ? { ...d, ...patch } : d)))
    setDirty(true)
    setServerError(undefined)
  }

  const clean: ActionDef[] = drafts.map((d) => {
    const out: ActionDef = {
      name: d.name.trim(),
      label: d.label.trim(),
      event: d.event.trim(),
      requires_feedback: d.requires_feedback,
    }

    if (d.set_status) {
      out.set_status = d.set_status
    }

    const labels = d.run_labels

    if (d.set_status === 'processing' && (labels?.running || labels?.done || labels?.failed)) {
      out.run_labels = { running: labels.running?.trim(), done: labels.done?.trim(), failed: labels.failed?.trim() }
    }

    if (d.allowed_statuses?.length) {
      out.allowed_statuses = d.allowed_statuses
    }

    return out
  })
  const rowErrors = clean.map((a) => validateAction(a, clean))

  function submit(root: HTMLElement | null) {
    setSubmitted(true)
    setServerError(undefined)

    if (rowErrors.some(Boolean)) {
      // Open the rows with a problem so the message under the field is visible.
      setExpanded((prev) => new Set([...prev, ...drafts.filter((_, i) => rowErrors[i]).map((d) => d.rowKey)]))
      focusFirstInvalid(root)

      return
    }

    save.mutate(clean)
  }

  return (
    <div className="grid gap-3" data-testid="actions-editor">
      <p className="text-sm text-muted-foreground">
        Custom actions appear in each item&apos;s menu. Running one sends its event to your webhooks; an action that
        starts a run then waits for your automation to report it done or failed, under the names you give it below.
      </p>
      {drafts.length === 0 && (
        <div className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
          No custom actions. Approve and reject are always available.
        </div>
      )}
      {drafts.map((d, i) => {
        const shown = submitted ? rowErrors[i] : null
        const open = expanded.has(d.rowKey)

        return (
          <Collapsible
            key={d.rowKey}
            open={open}
            onOpenChange={(o) => setOpen(d.rowKey, o)}
            className="rounded-lg border bg-card"
          >
            <div className="flex items-center gap-1 py-1.5 pr-1.5 pl-2">
              <CollapsibleTrigger asChild>
                <button
                  type="button"
                  className="group/action flex min-w-0 flex-1 items-center gap-2 rounded-md p-1 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
                  aria-label={`Edit action ${d.label || d.name || 'unnamed'}`}
                >
                  <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]/action:rotate-90" />
                  <span className="min-w-0 flex-1">
                    <span className="flex min-w-0 items-baseline gap-2">
                      <span className="truncate text-sm font-medium">{d.label || 'Untitled action'}</span>
                      <code className="truncate text-xs text-muted-foreground">{d.name}</code>
                    </span>
                    {/* Collapsed rows carry the message; open rows show it under the field instead. */}
                    <span
                      className={cn(
                        'block truncate text-xs',
                        shown && !open ? 'text-destructive' : 'text-muted-foreground',
                      )}
                    >
                      {shown && !open ? shown.message : !d.name ? 'Give it a name' : summarize(clean[i])}
                    </span>
                  </span>
                </button>
              </CollapsibleTrigger>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                className="shrink-0 text-destructive"
                onClick={() => {
                  setDrafts((ds) => ds.filter((x) => x.rowKey !== d.rowKey))
                  setDirty(true)
                  setServerError(undefined)
                }}
                aria-label={`Remove action ${d.name || 'unnamed'}`}
              >
                <Trash2 />
              </Button>
            </div>
            <CollapsibleContent className="grid gap-3 border-t p-3">
              <div className="grid gap-3 sm:grid-cols-2">
                <div
                  className="grid gap-1.5 data-[invalid=true]:text-destructive"
                  data-invalid={shown?.field === 'name'}
                >
                  <Label htmlFor={`${d.rowKey}-name`} className="text-xs" required>
                    Name
                  </Label>
                  <Input
                    id={`${d.rowKey}-name`}
                    value={d.name}
                    onChange={(e) => {
                      const name = e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, '_')
                      const autoEvent = !d.event || d.event === `item.${d.name}_requested`

                      update(d.rowKey, { name, ...(autoEvent ? { event: name ? `item.${name}_requested` : '' } : {}) })
                    }}
                    aria-invalid={shown?.field === 'name'}
                    aria-required
                    className="font-mono text-xs"
                    placeholder="regenerate"
                  />
                  {shown?.field === 'name' && <FieldError className="text-xs">{shown.message}</FieldError>}
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor={`${d.rowKey}-label`} className="text-xs">
                    Label
                  </Label>
                  <Input
                    id={`${d.rowKey}-label`}
                    value={d.label}
                    onChange={(e) => update(d.rowKey, { label: e.target.value })}
                    placeholder="Regenerate"
                  />
                </div>
                <div className="grid min-w-0 gap-1.5 sm:col-span-2">
                  <Label className="text-xs">What it does</Label>
                  <Select
                    value={effectOf(d) === 'other' ? d.set_status! : effectOf(d)}
                    onValueChange={(v) =>
                      update(d.rowKey, {
                        set_status:
                          v === 'run'
                            ? 'processing'
                            : v === 'notify'
                              ? undefined
                              : v === 'archive'
                                ? 'archived'
                                : (v as ItemStatus),
                      })
                    }
                  >
                    <SelectTrigger className="w-full" aria-label="What it does">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="run">Starts a run</SelectItem>
                      <SelectItem value="notify">Only notifies</SelectItem>
                      <SelectItem value="archive">Archives the item</SelectItem>
                      {effectOf(d) === 'other' && (
                        <SelectItem value={d.set_status!}>
                          Sets {STATUS_META[d.set_status!].label} (older setup)
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                  {/* The event is named after the action, so webhooks can't miss it through a typo. */}
                  <p className="text-xs text-muted-foreground">
                    {effectOf(d) === 'run' ? 'Your automation works on the item and reports done or failed. ' : ''}
                    Sends{' '}
                    <code className="font-mono">
                      {d.event || (d.name ? `item.${d.name}_requested` : 'item.<name>_requested')}
                    </code>{' '}
                    to your webhooks.
                  </p>
                </div>
              </div>
              {effectOf(d) === 'run' && (
                <div className="grid gap-1.5">
                  <Label className="text-xs">What to call the run</Label>
                  <div className="grid gap-2 sm:grid-cols-3">
                    {(
                      [
                        ['running', 'While it runs', 'Processing'],
                        ['done', 'When it worked', 'Completed'],
                        ['failed', 'When it failed', 'Failed'],
                      ] as const
                    ).map(([key, hint, fallback]) => (
                      <label key={key} className="grid min-w-0 gap-1">
                        <span className="text-[11px] text-muted-foreground">{hint}</span>
                        <Input
                          aria-label={`${hint} (${d.label || d.name || 'action'})`}
                          value={d.run_labels?.[key] ?? ''}
                          maxLength={40}
                          placeholder={fallback}
                          onChange={(e) => update(d.rowKey, { run_labels: { ...d.run_labels, [key]: e.target.value } })}
                        />
                      </label>
                    ))}
                  </div>
                </div>
              )}
              <div className="grid gap-1.5">
                <Label id={`${d.rowKey}-allowed`} className="text-xs">
                  Allowed from
                </Label>
                <StatusMultiSelect
                  aria-labelledby={`${d.rowKey}-allowed`}
                  // A run starts after a decision (approve or reject first): older setups drop pending and draft.
                  value={(d.allowed_statuses ?? []).filter(
                    (st) => effectOf(d) !== 'run' || !BEFORE_DECISION.includes(st),
                  )}
                  exclude={effectOf(d) === 'run' ? BEFORE_DECISION : NONE}
                  onChange={(v) => update(d.rowKey, { allowed_statuses: v })}
                />
              </div>
              <div className="flex items-center gap-2">
                <Switch
                  id={`${d.rowKey}-fb`}
                  checked={d.requires_feedback}
                  onCheckedChange={(v) => update(d.rowKey, { requires_feedback: v })}
                />
                <Label htmlFor={`${d.rowKey}-fb`} className="text-xs font-normal">
                  Requires feedback
                </Label>
              </div>
            </CollapsibleContent>
          </Collapsible>
        )
      })}
      <FieldError>{serverError}</FieldError>
      <div className="flex items-center justify-between gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={drafts.length >= 20}
          onClick={() => {
            const added = withKey({ name: '', label: '', event: '', requires_feedback: false })

            setDrafts((ds) => [...ds, added])
            setOpen(added.rowKey, true)
            setDirty(true)
          }}
        >
          <Plus /> Add action
        </Button>
        <div className="flex gap-2">
          {dirty && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setDrafts((collection.actions ?? []).map(withKey))
                setDirty(false)
                setSubmitted(false)
                setServerError(undefined)
              }}
            >
              Reset
            </Button>
          )}
          <Button
            type="button"
            size="sm"
            disabled={!dirty || save.isPending}
            onClick={(e) => submit(e.currentTarget.closest<HTMLElement>('[data-testid="actions-editor"]'))}
          >
            {save.isPending ? 'Saving...' : 'Save actions'}
          </Button>
        </div>
      </div>
    </div>
  )
}
