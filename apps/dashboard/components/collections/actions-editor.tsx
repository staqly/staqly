'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { FieldError } from '@/components/ui/field'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { ACTION_CATALOG, type CatalogAction, type CatalogActionName } from '@/lib/action-catalog'
import { api, type ActionDef, type Collection, type ItemStatus } from '@/lib/api'
import { formApiError } from '@/lib/form-errors'
import { qk } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'

/** One catalog action as the collection configures it: on or off, where it may run from, auto start. */
interface Flow {
  enabled: boolean
  allowed: ItemStatus[]
  autoRun: boolean
}

type Flows = Record<CatalogActionName, Flow>

/** The editor's state from the collection's actions; actions it doesn't list are off. */
function flowsOf(actions: ActionDef[] | undefined): Flows {
  const out = {} as Flows

  for (const c of ACTION_CATALOG) {
    const a = actions?.find((x) => x.name === c.name)
    const allowed = (a?.allowed_statuses ?? []).filter((s) => c.options?.includes(s))

    out[c.name] = {
      enabled: c.required || !!a,
      allowed: allowed.length ? allowed : [...c.defaultFrom],
      autoRun: !!a?.auto_run,
    }
  }

  return out
}

/** What is saved: enabled actions by name, with the statuses chosen where the action offers a choice. */
function toInput(flows: Flows) {
  return ACTION_CATALOG.filter((c) => flows[c.name].enabled).map((c) => {
    const f = flows[c.name]
    const out: { name: CatalogActionName; allowed_statuses?: ItemStatus[]; auto_run?: boolean } = { name: c.name }

    if (c.options) {
      out.allowed_statuses = c.options.filter((s) => f.allowed.includes(s))
    }

    if (c.autoRunnable && f.autoRun && f.allowed.includes('approved')) {
      out.auto_run = true
    }

    return out
  })
}

const statusList = (statuses: readonly ItemStatus[]) => statuses.map((s) => STATUS_META[s].label).join(' or ')

/** "Publishing, then Published or Publish failed". */
const runText = (c: CatalogAction) =>
  c.runLabels ? `${c.runLabels.running}, then ${c.runLabels.done} or ${c.runLabels.failed}` : null

export function ActionsEditor({ collection }: { collection: Collection }) {
  const qc = useQueryClient()
  const [flows, setFlows] = useState<Flows>(() => flowsOf(collection.actions))
  const [dirty, setDirty] = useState(false)
  const [serverError, setServerError] = useState<string | undefined>()

  const save = useMutation({
    mutationFn: () => api.updateCollection(collection.id, { actions: toInput(flows) }),
    onSuccess: (c) => {
      qc.setQueryData(qk.collection(c.id), c)
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      void qc.invalidateQueries({ queryKey: ['item'] })
      setFlows(flowsOf(c.actions))
      setDirty(false)
      toast.success('Actions saved')
    },
    onError: (e) => setServerError(formApiError(e, (k) => (k === 'actions' ? k : null)).actions),
  })

  function update(name: CatalogActionName, patch: Partial<Flow>) {
    setFlows((fs) => ({ ...fs, [name]: { ...fs[name], ...patch } }))
    setDirty(true)
    setServerError(undefined)
  }

  return (
    <div className="grid grid-cols-[minmax(0,1fr)] gap-3" data-testid="actions-editor">
      <p className="text-sm text-muted-foreground">
        Approve and reject are always there. Turn on the steps that follow; running one sends its event to your
        webhooks, and a run waits for your automation to report it done or failed.
      </p>
      <ul className="divide-y rounded-lg border bg-card">
        {ACTION_CATALOG.map((c) => {
          const f = flows[c.name]
          const id = `action-${c.name}`
          const run = runText(c)

          return (
            <li key={c.name} className="grid gap-2 p-3" data-testid={`action-row-${c.name}`}>
              <div className="flex items-start gap-3">
                <Switch
                  id={id}
                  className="mt-0.5"
                  checked={f.enabled}
                  disabled={c.required}
                  onCheckedChange={(v) => update(c.name, { enabled: v })}
                  aria-describedby={`${id}-desc`}
                />
                <div className="grid min-w-0 flex-1 gap-0.5">
                  <div className="flex flex-wrap items-baseline gap-x-2">
                    <Label htmlFor={id} className="text-sm font-medium">
                      {c.label}
                    </Label>
                    {c.required && <span className="text-xs text-muted-foreground">Always on</span>}
                  </div>
                  <p id={`${id}-desc`} className="text-xs text-muted-foreground">
                    {c.description}
                  </p>
                  {run && <p className="text-xs text-muted-foreground">{run}</p>}
                </div>
              </div>
              {f.enabled && (
                <div className="grid gap-2 pl-11">
                  {c.options ? (
                    <fieldset className="grid gap-1.5">
                      <legend className="mb-1.5 text-xs font-medium">Allowed from</legend>
                      <div className="flex flex-wrap gap-x-4 gap-y-2">
                        {c.options.map((s) => {
                          const checked = f.allowed.includes(s)
                          const last = checked && f.allowed.length === 1

                          return (
                            <div key={s} className="flex items-center gap-2">
                              <Checkbox
                                id={`${id}-from-${s}`}
                                checked={checked}
                                // At least one status: the last one can't be cleared.
                                disabled={last}
                                onCheckedChange={(v) =>
                                  update(c.name, {
                                    allowed: v ? [...f.allowed, s] : f.allowed.filter((x) => x !== s),
                                    // Starting after approval needs publish to run from approved.
                                    ...(!v && s === 'approved' ? { autoRun: false } : {}),
                                  })
                                }
                              />
                              <Label htmlFor={`${id}-from-${s}`} className="text-xs font-normal">
                                {STATUS_META[s].label}
                              </Label>
                            </div>
                          )
                        })}
                      </div>
                    </fieldset>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      Runs from {c.defaultFrom.length ? statusList(c.defaultFrom) : 'any status'}
                    </p>
                  )}
                  {c.autoRunnable && (
                    <div className="flex items-start gap-2">
                      <Switch
                        id={`${id}-auto`}
                        size="sm"
                        className="mt-0.5"
                        checked={f.autoRun && f.allowed.includes('approved')}
                        disabled={!f.allowed.includes('approved')}
                        onCheckedChange={(v) => update(c.name, { autoRun: v })}
                        aria-describedby={`${id}-auto-help`}
                      />
                      <div className="grid gap-0.5">
                        <Label htmlFor={`${id}-auto`} className="text-xs font-normal">
                          Start automatically after approval
                        </Label>
                        <p id={`${id}-auto-help`} className="text-xs text-muted-foreground">
                          {f.allowed.includes('approved')
                            ? 'Approving an item starts publishing right away, no second click.'
                            : 'Needs publish to be allowed from Approved.'}
                        </p>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </li>
          )
        })}
      </ul>
      <FieldError>{serverError}</FieldError>
      <div className="flex justify-end gap-2">
        {dirty && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              setFlows(flowsOf(collection.actions))
              setDirty(false)
              setServerError(undefined)
            }}
          >
            Reset
          </Button>
        )}
        <Button type="button" size="sm" disabled={!dirty || save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? 'Saving...' : 'Save actions'}
        </Button>
      </div>
    </div>
  )
}
