'use client'

import { useMutation, useQuery, useQueryClient, type InfiniteData } from '@tanstack/react-query'
import { ArchiveRestore, CheckCheck, Inbox as InboxIcon, Keyboard, Send, Trash2, X } from 'lucide-react'
import Link from 'next/link'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { memo, useCallback, useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { ExternalContentBadge } from '@/components/common/external-badge'
import { SkeletonList } from '@/components/common/skeleton-list'
import { ItemState } from '@/components/common/status-badge'
import {
  EditItemDialog,
  FeedbackDialog,
  ActionIcon,
  ActionToolbar,
  actionTone,
  BulkMoreMenu,
  ConfirmActionDialog,
  followUpAction,
  mainAction,
  needsFeedback,
  REJECT_ACTION,
  needsConfirm,
  useRunAction,
} from '@/components/items/item-actions'
import { ItemThumb } from '@/components/items/item-preview'
import { ItemSheet } from '@/components/items/item-sheet'
import { ProcessingReport, ProcessingWait } from '@/components/items/processing-note'
import { LIVE_INTERVAL_MS } from '@/components/shell/live-updates'
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
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Kbd } from '@/components/ui/kbd'
import { useIsMobile } from '@/hooks/use-mobile'
import { api, type ActionDef, type Item, type ItemPage, type ItemQuery } from '@/lib/api'
import { formatDateTime, pluralize, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import {
  flattenItems,
  invalidateAfterItemChange,
  qk,
  useCollections,
  useMode,
  useItemActions,
  useItemsInfinite,
  useRefetchOnForeignChange,
} from '@/lib/queries'
import { cn } from '@/lib/utils'

import { FilterPopover } from './filter-popover'
import { MobileInbox } from './mobile-inbox'
import { legacyStep, stepAccent, stepCounts, stepOf, Stepper } from './steps'

/** `pendingOnly`: approve/reject/edit, hidden on every step but Review. */
const SHORTCUTS: { keys: string[]; label: string; pendingOnly?: boolean; decidedOnly?: boolean }[] = [
  { keys: ['j'], label: 'Next item' },
  { keys: ['k'], label: 'Previous item' },
  { keys: ['⇧', 'A'], label: 'Approve (selected, or this item)', pendingOnly: true },
  { keys: ['⇧', 'R'], label: 'Reject with feedback', pendingOnly: true },
  { keys: ['⇧', 'E'], label: 'Edit title and content', pendingOnly: true },
  { keys: ['⇧', 'P'], label: 'Main action (Publish, Regenerate, Retry...)', decidedOnly: true },
  { keys: ['x'], label: 'Toggle selection' },
  { keys: ['⇧', 'Click'], label: 'Select a range' },
  { keys: ['Esc'], label: 'Clear selection' },
  { keys: ['o'], label: 'Open item page' },
  { keys: ['?'], label: 'Show shortcuts' },
]

// Module constants: a fresh filtered array per render would re-render the legend on every j/k.
const DECIDED_SHORTCUTS = SHORTCUTS.filter((s) => !s.pendingOnly)
const PENDING_SHORTCUTS = SHORTCUTS.filter((s) => !s.decidedOnly)
// The list footer: one row to move and select, one to decide (Pending only).
const FOOTER_MOVE = {
  title: 'Move',
  keys: [
    { keys: ['j', 'k'], label: 'next/prev' },
    { keys: ['o'], label: 'open' },
    { keys: ['x'], label: 'select' },
  ],
}
const FOOTER_DECIDE = {
  title: 'Decide',
  keys: [
    { keys: ['⇧', 'A'], label: 'approve' },
    { keys: ['⇧', 'R'], label: 'reject' },
    { keys: ['⇧', 'E'], label: 'edit' },
  ],
}
const FOOTER_MAIN = { title: 'Run', keys: [{ keys: ['⇧', 'P'], label: 'main action (Publish, Regenerate...)' }] }
const FOOTER = { all: [FOOTER_MOVE, FOOTER_DECIDE], decided: [FOOTER_MOVE, FOOTER_MAIN] }

/**
 * Keys pressed on form fields, buttons, links or inside overlays belong to that
 * element (Enter/Space activate a focused button), not to inbox shortcuts.
 */
export function isShortcutExempt(el: EventTarget | null, key?: string) {
  if (!(el instanceof HTMLElement)) {
    return false
  }

  // A step tab keeps focus after it is clicked: the list's shortcuts still work there (Enter and Space stay the tab's).
  if (key !== undefined && key !== 'Enter' && key !== ' ' && el.closest('[role=tab]') !== null) {
    return false
  }

  return (
    el.isContentEditable ||
    ['INPUT', 'TEXTAREA', 'SELECT', 'BUTTON', 'A'].includes(el.tagName) ||
    el.closest(
      'button,a[href],[role=button],[contenteditable]:not([contenteditable=false]),[role=dialog],[role=alertdialog],[role=menu],[data-slot=select-content]',
    ) !== null
  )
}

const NO_ACTIONS: ActionDef[] = []

/** Actions column width per tab, as narrow as its buttons: Reject + Approve, one main action, or the "..." menu. */
function actionsWidth(stepId: string) {
  return stepId === 'review' ? 'lg:w-48' : stepId === 'processing' ? 'lg:w-12' : 'lg:w-32'
}

const noop = () => {}

/** The collection actions a row offers for its item's status (what GET /v1/items/{id}/actions would list). */
function rowActions(actions: ActionDef[], status: Item['status']) {
  return actions.filter(
    (a) =>
      !a.builtin &&
      status !== 'processing' &&
      (!a.allowed_statuses?.length || a.allowed_statuses.includes(status)) &&
      a.set_status !== status,
  )
}

/** Keys that change an item's status: never repeat them by holding the key down. */
const DECISION_KEYS = new Set(['A', 'R', 'E'])

function anyOverlayOpen() {
  return (
    document.querySelector(
      '[role=dialog][data-state=open],[role=alertdialog][data-state=open],[role=menu][data-state=open],[data-slot=select-content][data-state=open]',
    ) !== null
  )
}

/**
 * One row of the desktop queue. Rows are rendered in a loop, which the React Compiler does not memoise per
 * item, so `memo` keeps j/k, selection and approve from re-rendering every loaded row.
 */
export const InboxRow = memo(function InboxRow({
  item,
  active,
  highlighted,
  selected,
  accent,
  actionsW,
  collectionName,
  time,
  onActivate,
  onToggle,
  onApprove,
  onReject,
  actions = NO_ACTIONS,
  onAction,
  onUnarchive,
}: {
  item: Item
  /** The keyboard cursor (aria-current); `highlighted` is whether it is drawn. */
  active: boolean
  highlighted: boolean
  selected: boolean
  /** Background class of the active row's edge: the open tab's colour. */
  accent: string
  /** Width class of the actions column (the open tab's, so rows line up with the header). */
  actionsW: string
  collectionName: string
  time: string
  onActivate: (id: string) => void
  /** Reject / Approve at the end of the row while reviewing. Stable callbacks keep the row memoised. */
  onApprove?: (item: Item) => void
  onReject?: (id: string) => void
  /** The collection's actions (stable array from the collections query) and a stable runner, outside Review. */
  actions?: ActionDef[]
  onAction?: (item: Item, action: ActionDef) => void
  /** Unarchive at the end of an archived row. */
  onUnarchive?: (id: string) => void
  /** `range`: shift-click, select everything from the last toggled row to this one (like Gmail). */
  onToggle: (id: string, range: boolean) => void
}) {
  const source = item.source.replace(/^watcher:/, '')
  const custom = onAction ? rowActions(actions, item.status) : NO_ACTIONS

  return (
    // content-visibility skips layout and paint for off-screen rows; thousands can be loaded.
    // A plain list (rows hold a checkbox and buttons, which an option role would hide); the active row is
    // aria-current. j/k and the arrow keys on the window move it.
    <li
      className="[contain-intrinsic-size:auto_85px] [content-visibility:auto]"
      aria-current={active ? 'true' : undefined}
    >
      {/* oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- keyboard users move with j/k and open with o */}
      <div
        data-item-id={item.id}
        tabIndex={-1}
        onClick={() => onActivate(item.id)}
        className={cn(
          'group relative flex cursor-pointer items-center gap-3 border-b px-4 py-3 transition-colors',
          highlighted ? 'bg-accent' : 'hover:bg-muted/50',
          selected && 'bg-brand-soft',
        )}
      >
        {highlighted && <span className={cn('absolute inset-y-0 left-0 w-0.5', accent)} />}
        {/* Every item can be selected: decided ones take collection actions (publish, archive...) in bulk. */}
        <Checkbox
          checked={selected}
          onClick={(e) => {
            // Handled here instead of onCheckedChange, which has no shiftKey; preventDefault skips Radix's own toggle.
            e.stopPropagation()
            e.preventDefault()
            onToggle(item.id, e.shiftKey)
          }}
          aria-label={`Select ${item.title}`}
          className={cn(!selected && 'opacity-40 group-hover:opacity-100')}
        />
        <ItemThumb item={item} className="size-12" />
        <div className="min-w-0 flex-1">
          <div className="max-w-3xl truncate text-sm font-medium">{item.title || 'Untitled'}</div>
          {/* The model's one-line summary under the title; nothing until there is one. */}
          {item.summary && <div className="max-w-3xl truncate text-xs text-muted-foreground">{item.summary}</div>}
          {/* Narrow screens: details on a line of their own. Wide ones put them in columns (below). */}
          <div className="flex items-center gap-1.5 truncate text-xs text-muted-foreground lg:hidden">
            <ExternalContentBadge item={item} compact />
            <span className="truncate">{collectionName}</span>
            {source && (
              <>
                <span>·</span>
                <span className="truncate">{source}</span>
              </>
            )}
            <span>·</span>
            <span className="shrink-0">{time}</span>
          </div>
        </div>
        {/* Wide screens: two lines at most, details in columns. */}
        <div className="hidden w-56 shrink-0 flex-wrap gap-1 overflow-hidden xl:flex xl:max-h-11">
          {item.tags.slice(0, 3).map((t) => (
            <span
              key={t}
              className="max-w-full truncate rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground"
            >
              {t}
            </span>
          ))}
        </div>
        <div className="hidden w-44 min-w-0 shrink-0 text-xs lg:block">
          <div className="flex items-center gap-1.5">
            <ExternalContentBadge item={item} compact />
            <span className="truncate text-sm">{collectionName}</span>
          </div>
          {/* The source (agent, watcher) as a small badge, like the version next to the time. */}
          {source && (
            <span
              className="mt-0.5 inline-block max-w-full truncate rounded bg-muted px-1 py-px align-top font-mono text-[10px] text-muted-foreground"
              title={`Source: ${source}`}
            >
              {source}
            </span>
          )}
        </div>
        {/* Same time the parent passes: added while pending, decided (or last updated) after. */}
        <div
          className="mr-4 hidden w-36 shrink-0 text-xs text-muted-foreground lg:block"
          title={formatDateTime(item.status === 'pending' ? item.created_at : (item.decided_at ?? item.updated_at))}
        >
          {item.status === 'processing' ? (
            <ProcessingWait item={item} />
          ) : (
            // The header names the column (Added / Decided); the version is a small badge next to the time.
            <div className="flex items-center gap-1.5">
              <span className="text-sm text-foreground">{time}</span>
              {item.version > 1 && (
                <span className="rounded bg-muted px-1 py-px font-mono text-[10px]" title={`Version ${item.version}`}>
                  v{item.version}
                </span>
              )}
            </div>
          )}
        </div>
        <div className="flex shrink-0 justify-center lg:w-32">
          <ItemState item={item} />
        </div>
        {/* Fixed width on wide screens so the columns line up with the header. */}
        <div className={cn('flex shrink-0 items-center justify-end', actionsW)}>
          {item.status === 'processing' && (
            // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- only stops the row's click
            <div onClick={(e) => e.stopPropagation()}>
              <ProcessingReport item={item} />
            </div>
          )}
          {onApprove && onReject && (
            // Buttons act on this row only; a click on them must not open the item.
            // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- only stops the row's click
            <div className="flex shrink-0 items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
              <Button size="sm" variant="reject" onClick={() => onReject(item.id)}>
                <X /> Reject
              </Button>
              <Button size="sm" variant="approve" onClick={() => onApprove(item)}>
                <CheckCheck /> Approve
              </Button>
            </div>
          )}
          {onUnarchive && item.archived_at && (
            // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- only stops the row's click
            <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
              <Button size="sm" variant="outline" onClick={() => onUnarchive(item.id)}>
                <ArchiveRestore /> Unarchive
              </Button>
            </div>
          )}
          {onAction && custom.length > 0 && !item.archived_at && (
            // The item page's main action (Publish, Retry...) only; the rest is in the preview and bulk menus.
            // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- only stops the row's click
            <div className="shrink-0" onClick={(e) => e.stopPropagation()}>
              <ActionToolbar
                canApprove={false}
                canReject={false}
                canEdit={false}
                custom={custom}
                busy={false}
                size="sm"
                mainOnly
                onApprove={noop}
                onReject={noop}
                onEdit={noop}
                onCustom={(a) => onAction(item, a)}
              />
            </div>
          )}
        </div>
      </div>
    </li>
  )
})

export function Inbox() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const qc = useQueryClient()

  // The step (review, approved, processing, done, failed, rejected, archived) is the tab; the rest are filters.
  const step = stepOf(params.get('step') ?? legacyStep(params.get('decision')))
  const tagParam = params.get('tag') ?? ''
  const tags = useMemo(() => tagParam.split(',').filter(Boolean), [tagParam])
  const source = params.get('source') ?? ''
  // The review queue gets decisions, shortcuts and oldest-first; any other step lists items.
  const status = step.id === 'review' ? 'pending' : 'list'
  const noun = step.id === 'review' ? 'to review' : step.label.toLowerCase()
  const collectionId = params.get('collection') || ''
  const type = (params.get('type') as ItemQuery['type']) || undefined

  const query: ItemQuery = useMemo(
    () => ({
      ...step.query,
      tag: tags.join(',') || undefined,
      source: source || undefined,
      collection_id: collectionId || undefined,
      type,
      sort: status === 'pending' ? 'created_asc' : 'updated_desc',
      limit: 50,
    }),
    [step, tags, source, status, collectionId, type],
  )
  const flow = useQuery({
    queryKey: ['flow', collectionId],
    queryFn: () => api.flow(collectionId),
  })
  // Flow counts cover a whole collection: shown while no other filter narrows the list.
  const narrowed = tags.length > 0 || !!source || !!type
  const counts = flow.data && !narrowed ? stepCounts(flow.data.states) : null

  const collections = useCollections(true)
  const { isLocal } = useMode()
  // A refetch reloads every loaded page one by one (42 requests at ~2000 pending items), and the pending
  // inbox sorts oldest first, so new items land on the last page. Instead, poll the newest events (one small
  // request, also refetched on window focus) and refetch the list only when something changed that this tab
  // has not already applied locally (its own approvals are removed from the cache right away).
  const list = useItemsInfinite(query, { refetchOnWindowFocus: false })
  // Query and mutation results are new objects on every render; their functions are stable. Taking the
  // functions out keeps callbacks and JSX that use them memoised.
  const { refetch, fetchNextPage } = list
  // Results of the previous tab or filter, shown until the new list arrives: never act on them.
  const stale = list.isPlaceholderData
  const decidedHere = useRef(new Set<string>())
  const isDecidedHere = useCallback((itemId: string) => decidedHere.current.has(itemId), [])

  useRefetchOnForeignChange(refetch, { refetchInterval: LIVE_INTERVAL_MS, isLocal: isDecidedHere })

  // Only what still belongs to this step: an item approved or archived in the drawer leaves Review at once.
  const items = useMemo(() => flattenItems(list.data).filter(step.holds), [list.data, step])
  const collectionName = useCallback(
    (id: string) => collections.data?.find((c) => c.id === id)?.name ?? '',
    [collections.data],
  )

  const [activeId, setActiveId] = useState<string | null>(null)
  // The active row (what A / R / E act on) is highlighted only while it means something: during j/k and while
  // its preview is open. Closing the preview hides it; the next j/k picks up where it was.
  const [cursorShown, setCursorShown] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [feedbackFor, setFeedbackFor] = useState<{ action: ActionDef; ids: string[] } | null>(null)
  const [editing, setEditing] = useState(false)
  // The item open in the side panel (Details, o or Enter).
  const [detailsId, setDetailsId] = useState<string | null>(null)
  const [legendOpen, setLegendOpen] = useState(false)
  const listRef = useRef<HTMLDivElement>(null)

  const activeIndex = Math.max(
    0,
    items.findIndex((i) => i.id === activeId),
  )
  const active: Item | undefined = items.length ? items[activeIndex] : undefined
  const actions = useItemActions(active)
  // A constant and a useMemo: the React Compiler doesn't know these are arrays, so a fresh `[]` or a bare
  // `.filter()` result counts as mutable until its last use (the JSX below). That spans hook calls, so the
  // compiler gave up memoising the handlers and the whole page: every j/k re-rendered toolbar and dialogs.
  const available = actions.data ?? NO_ACTIONS
  const custom = useMemo(() => available.filter((a) => !a.builtin), [available])

  const isMobile = useIsMobile()

  // Declared before setParam: the React Compiler bails out on the whole component when a function is
  // referenced before its declaration (hoisting), which re-rendered every inbox row on each keypress.
  function setParams(patch: Record<string, string | undefined>) {
    const sp = new URLSearchParams(params.toString())

    // Filters from before the steps (?decision=, ?run=) never stick around.
    sp.delete('decision')
    sp.delete('run')

    for (const [key, value] of Object.entries(patch)) {
      if (value) {
        sp.set(key, value)
      } else {
        sp.delete(key)
      }
    }

    setSelected(new Set())
    setActiveId(null)
    router.replace(`${pathname}${sp.toString() ? `?${sp}` : ''}`, { scroll: false })
  }

  function setParam(key: string, value: string | undefined) {
    setParams({ [key]: value })
  }

  /** Remove decided items from this list (they no longer match the filter) and advance. */
  const dropFromList = useCallback(
    (ids: string[], updated?: Item[]) => {
      const matches = (u: Item) => step.holds(u)
      const stillMatching = new Set((updated ?? []).filter(matches).map((u) => u.id))
      const gone = ids.filter((id) => !stillMatching.has(id))

      if (!gone.length) {
        return
      }

      const remaining = items.filter((i) => !gone.includes(i.id))

      if (active && gone.includes(active.id)) {
        // Advance to the next item after the current one, else the previous.
        const after = items.slice(activeIndex + 1).find((i) => !gone.includes(i.id))
        const before = items
          .slice(0, activeIndex)
          .toReversed()
          .find((i) => !gone.includes(i.id))

        setActiveId((after ?? before)?.id ?? null)
      }

      qc.setQueryData<InfiniteData<ItemPage>>(qk.items(query), (old) =>
        old
          ? { ...old, pages: old.pages.map((p) => ({ ...p, data: p.data.filter((i) => !gone.includes(i.id)) })) }
          : old,
      )
      setSelected((s) => {
        const n = new Set(s)

        gone.forEach((id) => n.delete(id))

        return n
      })

      if (remaining.length < 5 && list.hasNextPage && !list.isFetchingNextPage) {
        void fetchNextPage()
      }
    },
    [items, active, activeIndex, qc, query, step, list.hasNextPage, list.isFetchingNextPage, fetchNextPage],
  )

  const { mutate: runMutate, isPending: running } = useRunAction({
    onDone: (updated) => {
      decidedHere.current.add(updated.id)
      dropFromList([updated.id], [updated])
      setFeedbackFor(null)
      setEditing(false)
    },
    // Undo of an approval: the item is pending again, back in the list and active.
    onUndo: (it) => {
      void refetch().then(() => setActiveId(it.id))
    },
  })

  // Empty inbox: a sample item to try reviewing without connecting an agent first.
  const { mutate: sendTestMutate, isPending: sendingTest } = useMutation({
    mutationFn: async () => {
      const item = {
        title: 'Test approval',
        content:
          'This is what an agent proposal looks like. Approve or reject it to see how decisions work; agents send these through MCP or the REST API.',
        source: isLocal ? 'staqly-local-test' : 'staqly-cloud-test',
        metadata: { sample: true },
      }

      // Every workspace has a Default collection; the name is the fallback while the list loads.
      return api.createItem(collections.data?.find((c) => c.is_default)?.id ?? 'Default', item)
    },
    onSuccess: (it) => {
      setActiveId(it.id)
      void refetch()
      // Pending counts in the header and the sidebar come from the collection list.
      invalidateAfterItemChange(qc)
    },
    onError: (e) => toastApiError(e),
  })

  const { mutate: bulkMutate, isPending: bulkPending } = useMutation({
    mutationFn: (v: { ids: string[]; action: string; feedback?: string; follow_up?: string }) => api.bulkAction(v),
    onSuccess: (res, v) => {
      const results = res.data ?? []
      const ok = results.filter((r) => r.ok)
      const failed = results.filter((r) => !r.ok)
      const verb = v.action === 'approve' ? 'Approved' : v.action === 'reject' ? 'Rejected' : 'Updated'

      for (const r of ok) {
        if (r.id) {
          decidedHere.current.add(r.id)
        }
      }

      if (ok.length) {
        toast.success(`${verb} ${ok.length} item${ok.length === 1 ? '' : 's'}`)
      }

      if (failed.length) {
        const conflicts = failed.filter((f) => f.error?.code === 'conflict').length

        toast.warning(
          `${failed.length} item${failed.length === 1 ? '' : 's'} skipped` +
            (conflicts ? ` (${conflicts} already decided)` : `: ${failed[0].error?.message ?? 'error'}`),
        )
      }

      dropFromList(
        results.filter((r) => r.ok || r.error?.code === 'conflict').map((r) => r.id!),
        ok.map((r) => r.item!).filter(Boolean),
      )
      setFeedbackFor(null)
      invalidateAfterItemChange(qc)
    },
    onError: (e) => toastApiError(e),
  })

  // Unarchive and Delete on the selection: one request per item; the list drops those that went through.
  // The ids to delete, taken when the confirmation opens, so its title doesn't change while it closes.
  const [confirmBulkDelete, setConfirmBulkDelete] = useState<string[] | null>(null)
  // A row's collection action (Publish, Regenerate...) waiting for confirmation.
  const [confirming, setConfirming] = useState<{ item: Item; action: ActionDef } | null>(null)
  // The ids to approve, taken when the confirmation opens (so the dialog doesn't re-render on every selection).
  const [confirmBulkApprove, setConfirmBulkApprove] = useState<string[] | null>(null)
  const { mutate: itemsMutate, isPending: itemsPending } = useMutation({
    mutationFn: async (v: { op: 'unarchive' | 'delete'; ids: string[] }) => {
      const results = await Promise.allSettled(
        v.ids.map((id) => (v.op === 'delete' ? api.deleteItem(id) : api.unarchiveItem(id))),
      )

      return v.ids.filter((_, i) => results[i].status === 'fulfilled')
    },
    onSuccess: (done, v) => {
      const failed = v.ids.length - done.length

      if (done.length) {
        toast.success(`${v.op === 'delete' ? 'Deleted' : 'Unarchived'} ${pluralize(done.length, 'item')}`)
      }

      if (failed) {
        toast.warning(`${pluralize(failed, 'item')} skipped`)
      }

      setConfirmBulkDelete(null)
      dropFromList(done)
      invalidateAfterItemChange(qc, undefined, { usage: v.op === 'delete' })
    },
    onError: (e) => toastApiError(e),
  })

  const busy = running || bulkPending || itemsPending
  const unarchiveRow = useCallback((id: string) => itemsMutate({ op: 'unarchive', ids: [id] }), [itemsMutate])

  const move = useCallback(
    (delta: number) => {
      if (!items.length) {
        return
      }

      const next = Math.min(items.length - 1, Math.max(0, activeIndex + delta))

      setActiveId(items[next].id)
      setCursorShown(true)

      if (delta > 0 && next >= items.length - 3 && list.hasNextPage && !list.isFetchingNextPage) {
        void fetchNextPage()
      }

      requestAnimationFrame(() => {
        listRef.current?.querySelector(`[data-item-id="${items[next].id}"]`)?.scrollIntoView({ block: 'nearest' })
      })
    },
    [items, activeIndex, list.hasNextPage, list.isFetchingNextPage, fetchNextPage],
  )

  function runAction(action: ActionDef) {
    if (!active || busy) {
      return
    }

    if (needsFeedback(action)) {
      setFeedbackFor({ action, ids: [active.id] })

      return
    }

    if (needsConfirm(action)) {
      setConfirming({ item: active, action })

      return
    }

    runMutate({ item: active, action: action.name, body: { expected_version: active.version } })
  }

  /** A row click: it becomes the active row (j/k continue from it) and opens in the drawer. */
  const openItem = useCallback((id: string) => {
    setActiveId(id)
    setCursorShown(true)
    setDetailsId(id)
  }, [])
  const approveRow = useCallback(
    (it: Item) => runMutate({ item: it, action: 'approve', body: { expected_version: it.version } }),
    [runMutate],
  )
  const rejectRow = useCallback((id: string) => setFeedbackFor({ action: REJECT_ACTION, ids: [id] }), [])
  const actionRow = useCallback(
    (it: Item, a: ActionDef) =>
      needsFeedback(a)
        ? setFeedbackFor({ action: a, ids: [it.id] })
        : needsConfirm(a)
          ? setConfirming({ item: it, action: a })
          : runMutate({ item: it, action: a.name, body: { expected_version: it.version } }),
    [runMutate],
  )
  const collectionActions = useCallback(
    (id: string) => collections.data?.find((c) => c.id === id)?.actions ?? NO_ACTIONS,
    [collections.data],
  )

  function approve() {
    if (!active || busy) {
      return
    }

    const a = available.find((x) => x.name === 'approve')

    if (!a) {
      if (!actions.isLoading) {
        toast.info(`Can't approve an item that is ${active.status}`)
      }

      return
    }

    runAction(a)
  }

  function reject() {
    if (!active || busy) {
      return
    }

    const a = available.find((x) => x.name === 'reject')

    if (!a) {
      if (!actions.isLoading) {
        toast.info(`Can't reject an item that is ${active.status}`)
      }

      return
    }

    setFeedbackFor({ action: { ...a, requires_feedback: true }, ids: [active.id] })
  }

  function edit() {
    if (!active || busy) {
      return
    }

    // Only items still under review can be edited here; Approve being offered is the sign of that.
    if (!available.some((x) => x.name === 'approve')) {
      if (!actions.isLoading) {
        toast.info(`Can't edit an item that is ${active.status}`)
      }

      return
    }

    setEditing(true)
  }

  // Read by toggleSelect, which must stay stable for the memoised rows.
  const itemsRef = useRef(items)
  const anchorRef = useRef<string | null>(null)

  useLayoutEffect(() => {
    itemsRef.current = items
  }, [items])

  const toggleSelect = useCallback((id: string, range = false) => {
    const loaded = itemsRef.current
    const from = range && anchorRef.current ? loaded.findIndex((i) => i.id === anchorRef.current) : -1
    const to = loaded.findIndex((i) => i.id === id)
    const ids = from >= 0 && to >= 0 ? loaded.slice(Math.min(from, to), Math.max(from, to) + 1).map((i) => i.id) : [id]

    anchorRef.current = id
    setSelected((s) => {
      const n = new Set(s)
      // The whole range takes the new state of the clicked row.
      const on = !n.has(id)

      ids.forEach((x) => (on ? n.add(x) : n.delete(x)))

      return n
    })
  }, [])

  function bulkApprove() {
    if (busy) {
      return
    }

    const ids = [...selected]

    if (!ids.length) {
      toast.info('Select items with x first')

      return
    }

    // One item gets the Undo toast; several are confirmed first, since a bulk approval can't be undone.
    if (ids.length === 1) {
      const it = items.find((i) => i.id === ids[0])

      if (it) {
        runMutate({ item: it, action: 'approve', body: { expected_version: it.version } })

        return
      }
    }

    setConfirmBulkApprove(ids.slice(0, 100))
  }

  function runBulk(a: ActionDef) {
    const ids = [...selected].slice(0, 100)

    if (needsFeedback(a)) {
      setFeedbackFor({ action: a, ids })
    } else {
      bulkMutate({ ids, action: a.name })
    }
  }

  function bulkReject() {
    if (!busy) {
      setFeedbackFor({ action: REJECT_ACTION, ids: [...selected].slice(0, 100) })
    }
  }

  function runMain() {
    if (!active || busy) {
      return
    }

    const a = mainAction(rowActions(collectionActions(active.collection_id), active.status), true)

    if (a) {
      actionRow(active, a)
    } else {
      toast.info('No action for this item')
    }
  }

  // An effect event sees the latest handlers without re-binding the listener.
  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.metaKey || e.ctrlKey || e.altKey) {
      return
    }

    if (isShortcutExempt(e.target, e.key) || anyOverlayOpen() || stale) {
      return
    }

    // With Caps Lock on, Shift+A arrives as 'a': letters with Shift count as the capital.
    const key = e.shiftKey && e.key.length === 1 ? e.key.toUpperCase() : e.key

    // ⇧P runs the active row's main action outside Review (the button the row shows: Publish, Regenerate...).
    if (e.shiftKey && key === 'P' && status !== 'pending' && !e.repeat) {
      e.preventDefault()
      runMain()

      return
    }

    if (e.repeat && DECISION_KEYS.has(key)) {
      return
    }

    // Decisions need Shift, so a stray key never approves or rejects anything. With a selection they apply to it.
    if (e.shiftKey && DECISION_KEYS.has(key) && status === 'pending') {
      e.preventDefault()

      if (key === 'E') {
        edit()
      } else if (selected.size > 0) {
        if (key === 'A') {
          bulkApprove()
        } else {
          bulkReject()
        }
      } else if (key === 'A') {
        approve()
      } else {
        reject()
      }

      return
    }

    switch (key) {
      case 'j':
      case 'ArrowDown':
        e.preventDefault()
        move(1)
        break
      case 'k':
      case 'ArrowUp':
        e.preventDefault()
        move(-1)
        break
      case 'x':
        if (active) {
          e.preventDefault()
          toggleSelect(active.id)
        }

        break
      case 'o':
      case 'Enter':
        if (active) {
          e.preventDefault()
          setDetailsId(active.id)
        }

        break
      case 'Escape':
        // Keep the same set when nothing is selected: a new one would re-render the page for nothing.
        setSelected((s) => (s.size ? new Set() : s))
        break
      case '?':
        e.preventDefault()
        setLegendOpen(true)
        break
    }
  })

  useEffect(() => {
    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const pendingTotal = useMemo(
    () =>
      (collections.data ?? [])
        .filter((c) => !collectionId || c.id === collectionId)
        .reduce((n, c) => n + (c.status_counts?.pending ?? 0), 0),
    [collections.data, collectionId],
  )

  // The collections' pending counts are exact only while nothing else narrows the list.
  const exactTotal = status === 'pending' && !type && !narrowed
  const total = exactTotal ? pendingTotal : items.length
  const more = exactTotal ? '' : list.hasNextPage ? '+' : ''
  const shortcuts = status === 'pending' ? PENDING_SHORTCUTS : DECIDED_SHORTCUTS
  const footerShortcuts = status === 'pending' ? FOOTER.all : FOOTER.decided
  const allSelected = items.length > 0 && items.every((i) => selected.has(i.id))
  // Outside Pending, the selection takes the collection actions its items allow (publish, archive...), the
  // union over their collections; the API skips items an action doesn't apply to and reports them.
  const bulkActions = useMemo(() => {
    if (selected.size === 0) {
      return NO_ACTIONS
    }

    const chosen = items.filter((i) => selected.has(i.id))
    const cols = new Set(chosen.map((i) => i.collection_id))
    const statuses = new Set(chosen.map((i) => i.status))
    const byName = new Map<string, ActionDef>()

    for (const c of collections.data ?? []) {
      if (!cols.has(c.id)) {
        continue
      }

      for (const a of c.actions ?? []) {
        // Offered when it applies to at least one selected item; the API skips the others and reports them.
        const applies = [...statuses].some(
          (st) =>
            st !== 'processing' &&
            (!a.allowed_statuses?.length || a.allowed_statuses.includes(st)) &&
            a.set_status !== st,
        )

        if (!a.builtin && applies && !byName.has(a.name)) {
          byName.set(a.name, a)
        }
      }
    }

    return [...byName.values()]
  }, [selected, items, collections.data])

  // Each part of the page is its own statement. The React Compiler memoises JSX per statement but not per
  // element inside a `?:`/`&&` expression, so one conditional tree re-rendered the toolbar, the filters and
  // the dialogs on every j/k, selection and approval.
  const filters = (
    <>
      <h1 className="mr-2 text-base font-semibold tracking-tight">Inbox</h1>
      <Stepper active={step.id} counts={counts} onPick={(id) => setParam('step', id === 'review' ? undefined : id)} />
      <div className="ml-auto">
        <FilterPopover
          value={{ collection: collectionId, source, tags, type: type ?? '' }}
          collections={collections.data ?? []}
          sources={(flow.data?.sources ?? []).map((x: { source: string }) => x.source).filter(Boolean)}
          onChange={(k, v) => setParam(k, v)}
          onClear={() => setParams({ collection: undefined, source: undefined, tag: undefined, type: undefined })}
        />
      </div>
    </>
  )
  // One fixed-height row above the list: select all, the count, and bulk actions once something is selected.
  const listHeader = (
    <div
      className="flex h-11 shrink-0 items-center gap-3 border-b bg-muted/40 px-4 text-xs text-muted-foreground"
      data-testid="list-header"
    >
      {items.length > 0 && (
        <Checkbox
          checked={allSelected ? true : selected.size > 0 ? 'indeterminate' : false}
          onCheckedChange={() => setSelected(allSelected ? new Set() : new Set(items.map((i) => i.id)))}
          aria-label={allSelected ? 'Deselect all' : 'Select all'}
        />
      )}
      <span className="min-w-0 flex-1 truncate tabular-nums" data-testid="inbox-count-label">
        {selected.size > 0 ? (
          <>
            <span className="font-medium text-foreground">{selected.size}</span> of {total}
            {more} selected
          </>
        ) : (
          `${total}${more} ${noun}`
        )}
      </span>
      {/* Column titles, lined up with the row columns (same widths, same breakpoints). */}
      {items.length > 0 && (
        <>
          <span className="hidden w-56 shrink-0 xl:block">Tags</span>
          <span className="hidden w-44 shrink-0 lg:block">Collection</span>
          <span className="mr-4 hidden w-36 shrink-0 lg:block">
            {status === 'pending' ? 'Added' : step.id === 'processing' ? 'Waiting' : 'Decided'}
          </span>
          <span className="hidden w-32 shrink-0 text-center lg:block">Status</span>
          <span className={cn('hidden shrink-0 text-right lg:block', actionsWidth(step.id))}>Actions</span>
        </>
      )}
    </div>
  )
  // Bulk actions float over the page: they never have to fit next to the count.
  const bulkBar = selected.size > 0 && (
    <div
      // Black in both themes (the dark class gives its buttons dark styles), clear of the shortcuts footer.
      className="dark fixed bottom-16 left-1/2 z-40 flex max-w-[calc(100vw-2rem)] -translate-x-1/2 items-center gap-1.5 rounded-xl bg-black p-1.5 pl-3 text-sm text-foreground shadow-2xl ring-1 ring-white/15 animate-in fade-in slide-in-from-bottom-2"
      data-testid="bulk-bar"
      role="toolbar"
      aria-label="Bulk actions"
    >
      <span className="mr-1 whitespace-nowrap tabular-nums">
        <span className="font-medium">{selected.size}</span> selected
      </span>
      <div className="flex min-w-0 items-center gap-1.5 overflow-x-auto">
        {status === 'pending' ? (
          <>
            <Button size="sm" variant="reject" disabled={busy} onClick={bulkReject}>
              <X /> Reject
            </Button>
            <Button size="sm" variant="approve" onClick={bulkApprove} disabled={busy}>
              <CheckCheck /> Approve
            </Button>
            <BulkMoreMenu actions={bulkActions} disabled={busy} onRun={runBulk} />
          </>
        ) : step.id === 'archived' ? (
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => itemsMutate({ op: 'unarchive', ids: [...selected] })}
          >
            <ArchiveRestore /> Unarchive
          </Button>
        ) : bulkActions.length === 0 ? null : (
          bulkActions.map((a) => (
            <Button
              key={a.name}
              size="sm"
              variant={actionTone(a).variant}
              className={actionTone(a).className}
              disabled={busy}
              onClick={() => runBulk(a)}
            >
              <ActionIcon action={a} /> {a.label}
            </Button>
          ))
        )}
        <Button
          size="sm"
          variant="ghost"
          className="text-destructive hover:text-destructive"
          disabled={busy}
          onClick={() => setConfirmBulkDelete([...selected])}
        >
          <Trash2 /> Delete
        </Button>
      </div>
      <Button size="icon-sm" variant="ghost" onClick={() => setSelected(new Set())} aria-label="Clear selection">
        <X />
      </Button>
    </div>
  )
  const shortcutsButton = (
    <Button
      variant="ghost"
      size="sm"
      onClick={() => setLegendOpen(true)}
      className="text-muted-foreground pointer-coarse:hidden"
    >
      <Keyboard /> <span className="hidden sm:inline">Shortcuts</span> <Kbd>?</Kbd>
    </Button>
  )
  const listPane = (
    <div
      className={cn(
        'flex min-h-0 min-w-0 flex-1 flex-col',
        !list.isLoading && !list.error && items.length === 0 && 'hidden',
      )}
    >
      {listHeader}
      {bulkBar}
      <div ref={listRef} className="scrollbar-thin min-h-0 flex-1 overflow-y-auto">
        {list.isLoading ? (
          <div className="grid gap-2 p-3">
            <SkeletonList count={6} className="h-14" />
          </div>
        ) : list.error ? (
          <ErrorState error={list.error} onRetry={() => refetch()} className="m-3" />
        ) : items.length === 0 ? null : (
          <ul aria-label="Items">
            {items.map((it) => (
              <InboxRow
                key={it.id}
                item={it}
                active={active?.id === it.id}
                highlighted={cursorShown && active?.id === it.id}
                selected={selected.has(it.id)}
                accent={stepAccent(step.id)}
                actionsW={actionsWidth(step.id)}
                collectionName={collectionName(it.collection_id)}
                time={timeAgo(status === 'pending' ? it.created_at : (it.decided_at ?? it.updated_at))}
                onActivate={openItem}
                onToggle={toggleSelect}
                onApprove={status === 'pending' ? approveRow : undefined}
                onReject={status === 'pending' ? rejectRow : undefined}
                actions={collectionActions(it.collection_id)}
                onAction={status === 'pending' ? undefined : actionRow}
                onUnarchive={unarchiveRow}
              />
            ))}
          </ul>
        )}
        {list.hasNextPage && (
          <div className="p-3">
            <Button
              variant="outline"
              size="sm"
              className="w-full"
              onClick={() => fetchNextPage()}
              disabled={list.isFetchingNextPage}
            >
              {list.isFetchingNextPage ? 'Loading...' : 'Load more'}
            </Button>
          </div>
        )}
      </div>
      {/* One line: the list is full width now. */}
      <div className="hidden items-center gap-4 border-t px-4 py-2 text-[11px] text-muted-foreground lg:flex pointer-coarse:hidden">
        {footerShortcuts.map((g, i) => (
          <div key={g.title} className="flex items-center gap-3">
            {i > 0 && <span aria-hidden className="mr-1 h-3.5 w-px bg-border" />}
            <span className="shrink-0 text-[10px] font-medium tracking-wide uppercase">{g.title}:</span>
            {g.keys.map((s) => (
              <span key={s.label} className="inline-flex items-center gap-1">
                {s.keys.map((k) => (
                  <Kbd key={k} className="h-4 min-w-4 text-[10px]">
                    {k}
                  </Kbd>
                ))}
                {s.label}
              </span>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
  const emptyState = (
    <div className="flex flex-1 items-center justify-center p-8">
      <EmptyState
        icon={<InboxIcon />}
        title={narrowed ? 'No items match these filters' : step.empty.title}
        description={narrowed ? 'Try another step, or clear the filters.' : step.empty.description}
        action={
          status === 'pending' && !narrowed ? (
            <div className="flex flex-wrap justify-center gap-2">
              <Button size="sm" onClick={() => sendTestMutate()} disabled={sendingTest}>
                <Send /> {sendingTest ? 'Sending...' : 'Send a test approval'}
              </Button>
              <Button asChild variant="outline" size="sm">
                <Link href="/integrations">Connect an agent</Link>
              </Button>
            </div>
          ) : undefined
        }
      />
    </div>
  )
  const feedbackDialog = (
    <FeedbackDialog
      open={!!feedbackFor}
      onOpenChange={(o) => !o && setFeedbackFor(null)}
      action={feedbackFor?.action ?? null}
      count={feedbackFor?.ids.length}
      itemTitle={feedbackFor?.ids.length === 1 ? items.find((i) => i.id === feedbackFor.ids[0])?.title : undefined}
      pending={busy}
      // Offered only when every item shares one collection, so the follow-up means the same action for all.
      followUp={followUpAction(
        new Set(items.filter((i) => feedbackFor?.ids.includes(i.id)).map((i) => i.collection_id)).size === 1
          ? collections.data?.find((c) => c.id === items.find((i) => i.id === feedbackFor?.ids[0])?.collection_id)
          : null,
      )}
      onSubmit={(feedback, followUp) => {
        if (!feedbackFor) {
          return
        }

        const { action, ids } = feedbackFor

        if (ids.length > 1 || (ids.length === 1 && selected.size > 0 && ids[0] !== active?.id)) {
          bulkMutate({ ids, action: action.name, feedback, follow_up: followUp })

          return
        }

        const item = items.find((i) => i.id === ids[0])

        if (item) {
          runMutate({
            item,
            action: action.name,
            body: { feedback, expected_version: item.version, follow_up: followUp },
          })
        }
      }}
    />
  )
  const editDialog = (
    <EditItemDialog item={editing && active ? active : null} open={editing} onOpenChange={setEditing} />
  )
  const legend = (
    <Dialog open={legendOpen} onOpenChange={setLegendOpen}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>Review without touching the mouse.</DialogDescription>
        </DialogHeader>
        <ul className="grid gap-1.5 text-sm">
          {shortcuts.map((s) => (
            <li key={s.label} className="flex items-center justify-between">
              <span>{s.label}</span>
              <span className="flex gap-1">
                {s.keys.map((k) => (
                  <Kbd key={k}>{k}</Kbd>
                ))}
              </span>
            </li>
          ))}
          <li className="flex items-center justify-between">
            <span>Command palette</span>
            <Kbd>⌘K</Kbd>
          </li>
        </ul>
      </DialogContent>
    </Dialog>
  )

  return (
    <div className={cn('flex min-h-0 flex-col', isMobile ? 'flex-1' : 'h-svh')} data-testid="inbox">
      {isMobile ? (
        <MobileInbox
          items={items}
          active={active}
          activeIndex={activeIndex}
          setActiveId={setActiveId}
          move={move}
          status={status}
          step={step}
          collectionId={collectionId}
          type={type}
          setParam={setParam}
          clearFilters={() => setParams({ collection: undefined, type: undefined, tag: undefined, source: undefined })}
          narrowedBy={(tags.length ? 1 : 0) + (source ? 1 : 0)}
          collections={collections.data}
          collectionName={collectionName}
          countLabel={`${total}${more} ${noun}`}
          loading={list.isLoading}
          error={list.error}
          onRetry={() => void refetch()}
          hasNextPage={!!list.hasNextPage}
          fetchingNext={list.isFetchingNextPage}
          fetchNext={() => void fetchNextPage()}
          available={available}
          custom={custom}
          actionsLoading={actions.isLoading}
          busy={busy}
          approve={approve}
          reject={reject}
          edit={edit}
          runAction={runAction}
        />
      ) : (
        <>
          {/* Toolbar */}
          <div className="flex items-center gap-2 overflow-x-auto border-b px-4">
            {filters}
            {shortcutsButton}
          </div>

          {/* While another tab or filter loads, the previous list stays visible but dimmed and inert. */}
          <div
            className={cn('flex min-h-0 flex-1 flex-row transition-opacity', stale && 'opacity-60')}
            inert={stale}
            aria-busy={stale}
          >
            {/* One list; a row opens the whole item in the drawer. With nothing to list, the empty state. */}
            {listPane}
            {!list.isLoading && !list.error && items.length === 0 && emptyState}
          </div>
        </>
      )}

      {feedbackDialog}
      <AlertDialog open={!!confirmBulkDelete} onOpenChange={(o) => !o && !itemsPending && setConfirmBulkDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {pluralize(confirmBulkDelete?.length ?? 0, 'item')}?</AlertDialogTitle>
            <AlertDialogDescription>
              They are removed from their collections. Their history is kept for the activity log.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={itemsPending}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={itemsPending}
              onClick={(e) => {
                e.preventDefault()
                itemsMutate({ op: 'delete', ids: confirmBulkDelete ?? [] })
              }}
            >
              {itemsPending ? 'Deleting...' : 'Delete'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={!!confirmBulkApprove} onOpenChange={(o) => !o && !bulkPending && setConfirmBulkApprove(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Approve {pluralize(confirmBulkApprove?.length ?? 0, 'item')}?</AlertDialogTitle>
            <AlertDialogDescription>
              {pluralize(confirmBulkApprove?.length ?? 0, 'pending item')} will be approved as-is and their agents told
              to go ahead. This can&apos;t be undone; open an item on its own to edit it first.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkPending}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="approve"
              disabled={bulkPending}
              onClick={(e) => {
                e.preventDefault()
                bulkMutate(
                  { ids: confirmBulkApprove ?? [], action: 'approve' },
                  { onSettled: () => setConfirmBulkApprove(null) },
                )
              }}
            >
              {bulkPending ? 'Approving...' : `Approve ${confirmBulkApprove?.length ?? 0}`}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <ConfirmActionDialog
        action={confirming?.action ?? null}
        itemTitle={confirming?.item.title}
        onCancel={() => setConfirming(null)}
        onConfirm={() => {
          if (confirming) {
            runMutate({
              item: confirming.item,
              action: confirming.action.name,
              body: { expected_version: confirming.item.version },
            })
          }

          setConfirming(null)
        }}
      />
      {editDialog}
      <ItemSheet
        id={detailsId}
        onClose={() => {
          setDetailsId(null)
          setCursorShown(false)
        }}
      />
      {legend}
    </div>
  )
}
