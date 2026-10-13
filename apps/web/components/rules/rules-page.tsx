'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  GripVertical,
  ListChecks,
  MoreHorizontal,
  Pencil,
  Plus,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { UsageChip } from '@/components/plans/usage-chip'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, type Rule } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useCollections, useRules } from '@/lib/queries'
import {
  RULE_ACTIONS,
  RULE_TEMPLATES,
  moveItem,
  nextPriority,
  reorderPriorities,
  summarizeRule,
  templateValues,
  type RuleTemplate,
} from '@/lib/rules'
import { cn } from '@/lib/utils'

import { actionIcon, RuleFormSheet, type RuleSheetState } from './rule-form-sheet'

function TemplateCards({ onPick, compact = false }: { onPick: (t: RuleTemplate) => void; compact?: boolean }) {
  return (
    <div className={cn('grid gap-2 sm:grid-cols-2', !compact && 'lg:grid-cols-4')} data-testid="rule-templates">
      {RULE_TEMPLATES.map((t) => {
        const action = t.values.action ?? 'auto_approve'
        const { Icon, cls } = actionIcon(action)

        return (
          <button
            key={t.id}
            type="button"
            onClick={() => onPick(t)}
            className="group flex items-start gap-3 rounded-xl border bg-card p-3 text-left shadow-xs transition-colors hover:border-foreground/20 hover:bg-muted/40"
          >
            <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted">
              <Icon className={cn('size-4', cls)} />
            </span>
            <span className="min-w-0">
              <span className="block text-sm font-medium">{t.title}</span>
              <span className="block text-xs text-muted-foreground">{t.description}</span>
            </span>
          </button>
        )
      })}
    </div>
  )
}

function RuleRow({
  rule,
  index,
  count,
  summary,
  onEdit,
  onDelete,
  onMove,
  onToggle,
  dragging,
  dragOver,
  dragHandlers,
}: {
  rule: Rule
  index: number
  count: number
  summary: string
  onEdit: () => void
  onDelete: () => void
  onMove: (to: number) => void
  onToggle: (enabled: boolean) => void
  dragging: boolean
  dragOver: boolean
  dragHandlers: React.HTMLAttributes<HTMLLIElement> & { draggable: boolean }
}) {
  const { Icon, cls } = actionIcon(rule.action)
  const actionLabel = RULE_ACTIONS.find((a) => a.value === rule.action)?.label ?? rule.action

  return (
    <li
      {...dragHandlers}
      data-testid="rule-row"
      className={cn(
        'relative flex items-start gap-2 rounded-xl border bg-card p-3 shadow-xs transition-[opacity,box-shadow] sm:gap-3 sm:p-4',
        !rule.enabled && 'bg-muted/30',
        dragging && 'opacity-50',
        dragOver && 'ring-2 ring-brand/40',
      )}
    >
      <div className="flex shrink-0 flex-col items-center gap-0.5 pt-0.5">
        <span
          className="hidden cursor-grab text-muted-foreground/60 hover:text-muted-foreground active:cursor-grabbing md:block"
          title="Drag to reorder"
          aria-hidden
        >
          <GripVertical className="size-4" />
        </span>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label={`Move ${rule.name} up`}
          disabled={index === 0}
          onClick={() => onMove(index - 1)}
        >
          <ArrowUp />
        </Button>
        <span
          className="text-[11px] font-semibold text-muted-foreground tabular-nums"
          aria-label={`Position ${index + 1}`}
        >
          {index + 1}
        </span>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label={`Move ${rule.name} down`}
          disabled={index === count - 1}
          onClick={() => onMove(index + 1)}
        >
          <ArrowDown />
        </Button>
      </div>

      <button type="button" onClick={onEdit} className="min-w-0 flex-1 text-left" aria-label={`Edit ${rule.name}`}>
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className={cn('text-sm font-semibold', !rule.enabled && 'text-muted-foreground')}>{rule.name}</span>
          <span className="inline-flex h-5 items-center gap-1 rounded-md bg-muted px-1.5 text-[11px] font-medium text-muted-foreground">
            <Icon className={cn('size-3', cls)} /> {actionLabel}
          </span>
          {!rule.enabled && (
            <span className="inline-flex h-5 items-center rounded-md px-1.5 text-[11px] font-medium text-muted-foreground ring-1 ring-border ring-inset">
              Off
            </span>
          )}
        </span>
        <span
          className={cn(
            'mt-1 block text-[13px] leading-snug break-words',
            rule.enabled ? 'text-foreground/80' : 'text-muted-foreground',
          )}
          data-testid="rule-summary-line"
        >
          {summary}
        </span>
        <span className="mt-1.5 block text-xs text-muted-foreground" data-testid="rule-applied">
          {rule.times_applied ? `Applied ${pluralize(rule.times_applied, 'time')}` : 'Not applied yet'}
        </span>
      </button>

      <div className="flex shrink-0 flex-col items-end gap-2 sm:flex-row sm:items-center">
        <Switch
          checked={rule.enabled}
          onCheckedChange={onToggle}
          aria-label={rule.enabled ? `Disable ${rule.name}` : `Enable ${rule.name}`}
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon-sm" variant="ghost" aria-label={`Options for ${rule.name}`}>
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="[&_[role=menuitem]]:pointer-coarse:min-h-11">
            <DropdownMenuItem onSelect={onEdit}>
              <Pencil /> Edit
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onMove(0)} disabled={index === 0}>
              <ArrowUp /> Move to top
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onMove(count - 1)} disabled={index === count - 1}>
              <ArrowDown /> Move to bottom
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onDelete}>
              <Trash2 /> Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  )
}

export function RulesPage() {
  const qc = useQueryClient()
  const rules = useRules()
  const collections = useCollections(true)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [sheet, setSheet] = useState<RuleSheetState>({})
  const [deleting, setDeleting] = useState<Rule | null>(null)
  const [dragId, setDragId] = useState<string | null>(null)
  const [overId, setOverId] = useState<string | null>(null)

  const list = rules.data ?? []
  const collectionName = (id: string) => collections.data?.find((c) => c.id === id)?.name ?? 'unknown collection'

  const openNew = (t?: RuleTemplate) => {
    setSheet({ rule: null, initial: t ? templateValues(t) : undefined })
    setSheetOpen(true)
  }
  const openEdit = (r: Rule) => {
    setSheet({ rule: r })
    setSheetOpen(true)
  }

  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.updateRule(id, { enabled }),
    onMutate: async ({ id, enabled }) => {
      // An in-flight refetch must not overwrite the optimistic value.
      await qc.cancelQueries({ queryKey: qk.rules })
      const prev = qc.getQueryData<Rule[]>(qk.rules)

      qc.setQueryData<Rule[]>(qk.rules, (old) => old?.map((r) => (r.id === id ? { ...r, enabled } : r)))

      return { prev }
    },
    onSuccess: (r) => toast.success(r.enabled ? `“${r.name}” enabled` : `“${r.name}” paused`),
    onError: (e, _v, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(qk.rules, ctx.prev)
      }

      toastApiError(e, 'Could not update the rule')
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.rules }),
  })

  const reorder = useMutation({
    mutationFn: async (ordered: Rule[]) => {
      // Sequential so a failure leaves a consistent prefix; usually 1–3 requests.
      for (const u of reorderPriorities(ordered)) {
        // oxlint-disable-next-line no-await-in-loop -- intentionally sequential (see above)
        await api.updateRule(u.id, { priority: u.priority })
      }
    },
    onMutate: async (ordered) => {
      await qc.cancelQueries({ queryKey: qk.rules })
      const prev = qc.getQueryData<Rule[]>(qk.rules)

      qc.setQueryData<Rule[]>(
        qk.rules,
        ordered.map((r, i) => ({ ...r, priority: (i + 1) * 10 })),
      )

      return { prev }
    },
    onError: (e, _v, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(qk.rules, ctx.prev)
      }

      toastApiError(e, 'Could not reorder rules')
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.rules }),
  })

  const del = useMutation({
    mutationFn: (id: string) => api.deleteRule(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.rules })
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success('Rule deleted')
    },
    onError: (e) => toastApiError(e),
  })

  const move = (from: number, to: number) => {
    if (to < 0 || to >= list.length || from === to) {
      return
    }

    reorder.mutate(moveItem(list, from, to))
  }

  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="Rules"
        icon={<ListChecks className="size-4" />}
        description="Decide what happens to new items before anyone looks at them."
        actions={
          <>
            <UsageChip metric="rules" noun="rules" />
            <Button onClick={() => openNew()}>
              <Plus /> New rule
            </Button>
          </>
        }
      />

      <div className="mt-6 grid gap-6">
        {rules.isLoading ? (
          <div className="grid gap-2">
            <Skeleton className="h-24 rounded-xl" />
            <Skeleton className="h-24 rounded-xl" />
          </div>
        ) : rules.error ? (
          <ErrorState error={rules.error} onRetry={() => rules.refetch()} />
        ) : list.length === 0 ? (
          <>
            <EmptyState
              icon={<ListChecks />}
              title="No rules yet"
              description="Without rules every new item waits in the Inbox. Add one to auto-approve confident results, mute noisy sources or escalate what matters."
              action={
                <Button onClick={() => openNew()}>
                  <Plus /> New rule
                </Button>
              }
            />
            <section className="grid gap-3">
              <h2 className="flex items-center gap-2 text-sm font-semibold">
                <Sparkles className="size-4 text-brand" /> Start from a template
              </h2>
              <TemplateCards onPick={openNew} />
            </section>
          </>
        ) : (
          <>
            <p className="flex items-start gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-[13px] text-muted-foreground">
              <ListChecks className="mt-0.5 size-4 shrink-0" />
              <span>
                Rules run once when an item arrives as <strong className="font-medium text-foreground">pending</strong>,
                top to bottom. The first match wins; the rest are skipped.
              </span>
            </p>
            <ol className="grid gap-2" data-testid="rules-list" aria-busy={reorder.isPending}>
              {list.map((r, i) => (
                <RuleRow
                  key={r.id}
                  rule={r}
                  index={i}
                  count={list.length}
                  summary={summarizeRule(r, collectionName)}
                  onEdit={() => openEdit(r)}
                  onDelete={() => setDeleting(r)}
                  onMove={(to) => move(i, to)}
                  onToggle={(enabled) => toggle.mutate({ id: r.id, enabled })}
                  dragging={dragId === r.id}
                  dragOver={overId === r.id && dragId !== r.id}
                  dragHandlers={{
                    draggable: true,
                    onDragStart: (e) => {
                      setDragId(r.id)
                      e.dataTransfer.effectAllowed = 'move'
                      e.dataTransfer.setData('text/plain', r.id)
                    },
                    onDragOver: (e) => {
                      if (!dragId) {
                        return
                      }

                      e.preventDefault()
                      setOverId(r.id)
                    },
                    onDragLeave: () => setOverId((o) => (o === r.id ? null : o)),
                    onDrop: (e) => {
                      e.preventDefault()
                      const from = list.findIndex((x) => x.id === dragId)

                      if (from >= 0) {
                        move(from, i)
                      }

                      setDragId(null)
                      setOverId(null)
                    },
                    onDragEnd: () => {
                      setDragId(null)
                      setOverId(null)
                    },
                  }}
                />
              ))}
            </ol>
            <section className="grid gap-3">
              <h2 className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                <Sparkles className="size-4 text-brand" /> Templates
              </h2>
              <TemplateCards onPick={openNew} compact />
            </section>
          </>
        )}
      </div>

      <RuleFormSheet open={sheetOpen} onOpenChange={setSheetOpen} state={sheet} nextPriority={nextPriority(list)} />
      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete “{deleting?.name}”?</AlertDialogTitle>
            <AlertDialogDescription>
              New items will no longer be checked against this rule. Decisions it already made stay in item history.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => deleting && del.mutate(deleting.id)}>
              Delete rule
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageContainer>
  )
}
