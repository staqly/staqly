'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { api, ITEM_STATUSES, type ActionDef, type Collection, type ItemStatus } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'

const NAME_RE = /^[a-z][a-z0-9_]{0,39}$/
const EVENT_RE = /^item\.[a-z0-9_.]{1,60}$/
/** Custom actions may not decide items; approve/reject are built in. */
const SETTABLE: ItemStatus[] = ITEM_STATUSES.filter((s) => s !== 'approved' && s !== 'rejected')

interface Draft extends ActionDef {
  rowKey: string
}

let keySeq = 0
const withKey = (a: ActionDef): Draft => ({ ...a, rowKey: `a${++keySeq}` })

export function validateAction(a: ActionDef, all: ActionDef[]): string | null {
  if (!NAME_RE.test(a.name)) {
    return 'Name must be lowercase letters, digits or _ and start with a letter'
  }

  if (a.name === 'approve' || a.name === 'reject') {
    return `“${a.name}” is a built-in action`
  }

  if (all.filter((x) => x.name === a.name).length > 1) {
    return `Duplicate action name “${a.name}”`
  }

  if (a.event && !EVENT_RE.test(a.event)) {
    return 'Event must look like item.something'
  }

  return null
}

export function ActionsEditor({ collection }: { collection: Collection }) {
  const qc = useQueryClient()
  const [drafts, setDrafts] = useState<Draft[]>(() => (collection.actions ?? []).map(withKey))
  const [dirty, setDirty] = useState(false)

  const save = useMutation({
    mutationFn: (actions: ActionDef[]) => api.updateCollection(collection.id, { actions }),
    onSuccess: (c) => {
      qc.setQueryData(qk.collection(c.id), c)
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      void qc.invalidateQueries({ queryKey: ['item'] })
      setDrafts((c.actions ?? []).map(withKey))
      setDirty(false)
      toast.success('Actions saved')
    },
    onError: (e) => toastApiError(e),
  })

  function update(key: string, patch: Partial<ActionDef>) {
    setDrafts((ds) => ds.map((d) => (d.rowKey === key ? { ...d, ...patch } : d)))
    setDirty(true)
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

    if (d.allowed_statuses?.length) {
      out.allowed_statuses = d.allowed_statuses
    }

    return out
  })
  const firstError = clean.map((a) => validateAction(a, clean)).find(Boolean) ?? null

  return (
    <div className="grid gap-3" data-testid="actions-editor">
      <p className="text-sm text-muted-foreground">
        Custom actions appear next to Approve/Reject. Running one records an event (sent to webhooks) and can move the
        item to a new status — your automation does the actual work.
      </p>
      {drafts.length === 0 && (
        <div className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
          No custom actions. Approve and reject are always available.
        </div>
      )}
      {drafts.map((d) => (
        <div key={d.rowKey} className="grid gap-3 rounded-lg border bg-card p-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor={`${d.rowKey}-name`} className="text-xs">
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
                className="font-mono text-xs"
                placeholder="regenerate"
              />
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
            <div className="grid gap-1.5">
              <Label htmlFor={`${d.rowKey}-event`} className="text-xs">
                Event
              </Label>
              <Input
                id={`${d.rowKey}-event`}
                value={d.event}
                onChange={(e) => update(d.rowKey, { event: e.target.value })}
                className="font-mono text-xs"
                placeholder="item.regenerate_requested"
              />
            </div>
            <div className="grid gap-1.5">
              <Label className="text-xs">Set status to</Label>
              <Select
                value={d.set_status ?? 'none'}
                onValueChange={(v) => update(d.rowKey, { set_status: v === 'none' ? undefined : (v as ItemStatus) })}
              >
                <SelectTrigger className="w-full" aria-label="Set status">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">Keep current status</SelectItem>
                  {SETTABLE.map((s) => (
                    <SelectItem key={s} value={s}>
                      {STATUS_META[s].label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-1.5">
            <Label className="text-xs">Allowed from (empty = any status)</Label>
            <div className="flex flex-wrap gap-x-3 gap-y-1.5">
              {ITEM_STATUSES.map((s) => {
                const id = `${d.rowKey}-allowed-${s}`
                const checked = d.allowed_statuses?.includes(s) ?? false

                return (
                  <div key={s} className="flex items-center gap-1.5">
                    <Checkbox
                      id={id}
                      checked={checked}
                      onCheckedChange={(v) =>
                        update(d.rowKey, {
                          allowed_statuses: v
                            ? [...(d.allowed_statuses ?? []), s]
                            : (d.allowed_statuses ?? []).filter((x) => x !== s),
                        })
                      }
                    />
                    <Label htmlFor={id} className="text-xs font-normal">
                      {STATUS_META[s].label}
                    </Label>
                  </div>
                )
              })}
            </div>
          </div>
          <div className="flex items-center justify-between">
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
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="text-destructive"
              onClick={() => {
                setDrafts((ds) => ds.filter((x) => x.rowKey !== d.rowKey))
                setDirty(true)
              }}
              aria-label={`Remove action ${d.name || 'unnamed'}`}
            >
              <Trash2 /> Remove
            </Button>
          </div>
        </div>
      ))}
      {firstError && dirty && <p className="text-sm text-destructive">{firstError}</p>}
      <div className="flex items-center justify-between gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={drafts.length >= 20}
          onClick={() => {
            setDrafts((ds) => [...ds, withKey({ name: '', label: '', event: '', requires_feedback: false })])
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
              }}
            >
              Reset
            </Button>
          )}
          <Button
            type="button"
            size="sm"
            disabled={!dirty || !!firstError || save.isPending}
            onClick={() => save.mutate(clean)}
          >
            {save.isPending ? 'Saving…' : 'Save actions'}
          </Button>
        </div>
      </div>
    </div>
  )
}
