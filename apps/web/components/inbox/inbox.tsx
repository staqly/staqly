'use client'

import { useMutation, useQueryClient, type InfiniteData } from '@tanstack/react-query'
import {
  Check,
  CheckCheck,
  ChevronDown,
  ExternalLink,
  Inbox as InboxIcon,
  Keyboard,
  Pencil,
  Play,
  X,
} from 'lucide-react'
import Link from 'next/link'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { ExternalContentBadge } from '@/components/common/external-badge'
import { SkeletonList } from '@/components/common/skeleton-list'
import { StatusBadge } from '@/components/common/status-badge'
import { TYPE_LABELS, TypeIcon } from '@/components/common/type-icon'
import {
  EditApproveDialog,
  FeedbackDialog,
  needsFeedback,
  REJECT_ACTION,
  useRunAction,
} from '@/components/items/item-actions'
import { ItemPreview, ItemThumb } from '@/components/items/item-preview'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Kbd } from '@/components/ui/kbd'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useIsMobile } from '@/hooks/use-mobile'
import { api, ITEM_TYPES, type ActionDef, type Item, type ItemPage, type ItemQuery } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import {
  flattenItems,
  invalidateAfterItemChange,
  qk,
  useCollections,
  useItemActions,
  useItemsInfinite,
  useRefetchOnForeignChange,
} from '@/lib/queries'
import { cn } from '@/lib/utils'

import { MobileInbox, STATUS_TABS } from './mobile-inbox'

export const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ['j'], label: 'Next item' },
  { keys: ['k'], label: 'Previous item' },
  { keys: ['a'], label: 'Approve' },
  { keys: ['r'], label: 'Reject with feedback' },
  { keys: ['e'], label: 'Edit, then approve' },
  { keys: ['x'], label: 'Toggle selection' },
  { keys: ['⇧', 'A'], label: 'Approve selected' },
  { keys: ['o'], label: 'Open item page' },
  { keys: ['Esc'], label: 'Clear selection' },
  { keys: ['?'], label: 'Show shortcuts' },
]

/**
 * Keys pressed on form fields, buttons, links or inside overlays belong to that
 * element (Enter/Space activate a focused button), not to inbox shortcuts.
 */
export function isShortcutExempt(el: EventTarget | null) {
  if (!(el instanceof HTMLElement)) {
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

/** Keys that change an item's status: never repeat them by holding the key down. */
const DECISION_KEYS = new Set(['a', 'r', 'A'])

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
const InboxRow = memo(function InboxRow({
  item,
  active,
  selected,
  collectionName,
  time,
  showStatus,
  onActivate,
  onToggle,
}: {
  item: Item
  active: boolean
  selected: boolean
  collectionName: string
  time: string
  showStatus: boolean
  onActivate: (id: string) => void
  onToggle: (id: string) => void
}) {
  return (
    // content-visibility skips layout and paint for off-screen rows; thousands can be loaded.
    <li className="[contain-intrinsic-size:auto_65px] [content-visibility:auto]">
      {/* oxlint-disable-next-line jsx-a11y/click-events-have-key-events -- options are not focusable; the listbox is driven by j/k and arrow keys on the window */}
      <div
        role="option"
        aria-selected={active}
        data-item-id={item.id}
        tabIndex={-1}
        onClick={() => onActivate(item.id)}
        className={cn(
          'group relative flex cursor-pointer items-center gap-3 border-b px-3 py-2.5 transition-colors',
          active ? 'bg-accent' : 'hover:bg-muted/50',
          selected && 'bg-brand-soft',
        )}
      >
        {active && <span className="absolute inset-y-0 left-0 w-0.5 bg-brand" />}
        <Checkbox
          checked={selected}
          onCheckedChange={() => onToggle(item.id)}
          onClick={(e) => e.stopPropagation()}
          aria-label={`Select ${item.title}`}
          className={cn(!selected && 'opacity-40 group-hover:opacity-100')}
        />
        <ItemThumb item={item} className="size-10" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{item.title || 'Untitled'}</div>
          <div className="flex items-center gap-1.5 truncate text-xs text-muted-foreground">
            <ExternalContentBadge item={item} compact />
            <span className="truncate">{collectionName}</span>
            <span>·</span>
            <span className="shrink-0">{time}</span>
          </div>
        </div>
        {showStatus && <StatusBadge status={item.status} />}
      </div>
    </li>
  )
})

export function Inbox() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const qc = useQueryClient()

  const status = (params.get('status') as ItemQuery['status']) || 'pending'
  const collectionId = params.get('collection') || ''
  const type = (params.get('type') as ItemQuery['type']) || undefined

  const query: ItemQuery = useMemo(
    () => ({
      status,
      collection_id: collectionId || undefined,
      type,
      sort: status === 'pending' ? 'created_asc' : 'decided_desc',
      limit: 50,
    }),
    [status, collectionId, type],
  )

  const collections = useCollections(true)
  // A refetch reloads every loaded page one by one (42 requests at ~2000 pending items), and the pending
  // inbox sorts oldest first, so new items land on the last page. Instead, poll the newest events (one small
  // request, also refetched on window focus) and refetch the list only when something changed that this tab
  // has not already applied locally (its own approvals are removed from the cache right away).
  const list = useItemsInfinite(query, { refetchOnWindowFocus: false })
  const decidedHere = useRef(new Set<string>())
  const isDecidedHere = useCallback((itemId: string) => decidedHere.current.has(itemId), [])

  useRefetchOnForeignChange(list.refetch, { refetchInterval: 20_000, isLocal: isDecidedHere })

  const items = useMemo(() => flattenItems(list.data), [list.data])
  const collectionName = useCallback(
    (id: string) => collections.data?.find((c) => c.id === id)?.name ?? '',
    [collections.data],
  )

  const [activeId, setActiveId] = useState<string | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [feedbackFor, setFeedbackFor] = useState<{ action: ActionDef; ids: string[] } | null>(null)
  const [editing, setEditing] = useState(false)
  const [legendOpen, setLegendOpen] = useState(false)
  const listRef = useRef<HTMLDivElement>(null)

  const activeIndex = Math.max(
    0,
    items.findIndex((i) => i.id === activeId),
  )
  const active: Item | undefined = items.length ? items[activeIndex] : undefined
  const actions = useItemActions(active)
  const available = actions.data ?? []
  const custom = available.filter((a) => !a.builtin)

  const isMobile = useIsMobile()

  // Declared before setParam: the React Compiler bails out on the whole component when a function is
  // referenced before its declaration (hoisting), which re-rendered every inbox row on each keypress.
  function setParams(patch: Record<string, string | undefined>) {
    const sp = new URLSearchParams(params.toString())

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
      const stillMatching = new Set((updated ?? []).filter((u) => u.status === status).map((u) => u.id))
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
        void list.fetchNextPage()
      }
    },
    [items, active, activeIndex, qc, query, status, list],
  )

  const run = useRunAction({
    onDone: (updated) => {
      decidedHere.current.add(updated.id)
      dropFromList([updated.id], [updated])
      setFeedbackFor(null)
      setEditing(false)
    },
  })

  const bulk = useMutation({
    mutationFn: (v: { ids: string[]; action: string; feedback?: string }) => api.bulkAction(v),
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

  const busy = run.isPending || bulk.isPending

  const move = useCallback(
    (delta: number) => {
      if (!items.length) {
        return
      }

      const next = Math.min(items.length - 1, Math.max(0, activeIndex + delta))

      setActiveId(items[next].id)

      if (delta > 0 && next >= items.length - 3 && list.hasNextPage && !list.isFetchingNextPage) {
        void list.fetchNextPage()
      }

      requestAnimationFrame(() => {
        listRef.current?.querySelector(`[data-item-id="${items[next].id}"]`)?.scrollIntoView({ block: 'nearest' })
      })
    },
    [items, activeIndex, list],
  )

  function runAction(action: ActionDef) {
    if (!active || busy) {
      return
    }

    if (needsFeedback(action)) {
      setFeedbackFor({ action, ids: [active.id] })

      return
    }

    run.mutate({ item: active, action: action.name, body: { expected_version: active.version } })
  }

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

    if (!available.some((x) => x.name === 'approve')) {
      if (!actions.isLoading) {
        toast.info(`Can't approve an item that is ${active.status}`)
      }

      return
    }

    setEditing(true)
  }

  function toggleSelect(id: string) {
    setSelected((s) => {
      const n = new Set(s)

      if (n.has(id)) {
        n.delete(id)
      } else {
        n.add(id)
      }

      return n
    })
  }

  function bulkApprove() {
    if (busy) {
      return
    }

    const ids = [...selected]

    if (!ids.length) {
      toast.info('Select items with x first')

      return
    }

    bulk.mutate({ ids: ids.slice(0, 100), action: 'approve' })
  }

  // Keep handlers fresh without re-binding the listener on every render.
  const handlers = useRef({ move, approve, reject, edit, toggleSelect, bulkApprove, active, setLegendOpen, router })

  // oxlint-disable-next-line react/exhaustive-deps -- intentionally runs after every render
  useEffect(() => {
    handlers.current = { move, approve, reject, edit, toggleSelect, bulkApprove, active, setLegendOpen, router }
  })

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) {
        return
      }

      if (isShortcutExempt(e.target) || anyOverlayOpen()) {
        return
      }

      const h = handlers.current
      const key = e.key

      if (e.repeat && DECISION_KEYS.has(key)) {
        return
      }

      if (key === 'A' && e.shiftKey) {
        e.preventDefault()
        h.bulkApprove()

        return
      }

      switch (key) {
        case 'j':
        case 'ArrowDown':
          e.preventDefault()
          h.move(1)
          break
        case 'k':
        case 'ArrowUp':
          e.preventDefault()
          h.move(-1)
          break
        case 'a':
          e.preventDefault()
          h.approve()
          break
        case 'r':
          e.preventDefault()
          h.reject()
          break
        case 'e':
          e.preventDefault()
          h.edit()
          break
        case 'x':
          if (h.active) {
            e.preventDefault()
            h.toggleSelect(h.active.id)
          }

          break
        case 'o':
        case 'Enter':
          if (h.active) {
            e.preventDefault()
            h.router.push(`/items/${h.active.id}`)
          }

          break
        case 'Escape':
          setSelected(new Set())
          break
        case '?':
          e.preventDefault()
          h.setLegendOpen(true)
          break
      }
    }

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

  const countLabel =
    status === 'pending' && !type
      ? `${pendingTotal} pending`
      : `${items.length}${list.hasNextPage ? '+' : ''} ${status}`

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
          collectionId={collectionId}
          type={type}
          setParam={setParam}
          clearFilters={() => setParams({ collection: undefined, type: undefined })}
          collections={collections.data}
          collectionName={collectionName}
          countLabel={countLabel}
          loading={list.isLoading}
          error={list.error}
          onRetry={() => void list.refetch()}
          hasNextPage={!!list.hasNextPage}
          fetchingNext={list.isFetchingNextPage}
          fetchNext={() => void list.fetchNextPage()}
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
          <div className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
            <div className="mr-2 flex items-center gap-2">
              <h1 className="text-base font-semibold tracking-tight">Inbox</h1>
              <span className="text-sm text-muted-foreground tabular-nums" data-testid="inbox-count-label">
                {countLabel}
              </span>
            </div>
            <ToggleGroup
              type="single"
              size="sm"
              variant="outline"
              value={status}
              onValueChange={(v) => v && setParam('status', v === 'pending' ? undefined : v)}
              aria-label="Status filter"
            >
              {STATUS_TABS.map((t) => (
                <ToggleGroupItem key={t.value} value={t.value} className="px-2.5 text-xs">
                  {t.label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
            <Select
              value={collectionId || 'all'}
              onValueChange={(v) => setParam('collection', v === 'all' ? undefined : v)}
            >
              <SelectTrigger size="sm" className="w-44" aria-label="Collection filter">
                <SelectValue placeholder="All collections" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All collections</SelectItem>
                {(collections.data ?? []).map((c) => (
                  <SelectItem key={c.id} value={c.id}>
                    {c.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={type ?? 'all'} onValueChange={(v) => setParam('type', v === 'all' ? undefined : v)}>
              <SelectTrigger size="sm" className="w-32" aria-label="Type filter">
                <SelectValue placeholder="All types" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All types</SelectItem>
                {ITEM_TYPES.map((t) => (
                  <SelectItem key={t} value={t}>
                    {TYPE_LABELS[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="ml-auto flex items-center gap-2">
              {selected.size > 0 && (
                <div
                  className="flex items-center gap-1.5 rounded-lg border bg-brand-soft px-2 py-1 text-xs"
                  data-testid="bulk-bar"
                >
                  <span className="font-medium">{selected.size} selected</span>
                  <Button size="xs" onClick={bulkApprove} disabled={busy}>
                    <CheckCheck /> Approve
                  </Button>
                  <Button
                    size="xs"
                    variant="destructive"
                    disabled={busy}
                    onClick={() => setFeedbackFor({ action: REJECT_ACTION, ids: [...selected].slice(0, 100) })}
                  >
                    <X /> Reject
                  </Button>
                  <Button size="xs" variant="ghost" onClick={() => setSelected(new Set())}>
                    Clear
                  </Button>
                </div>
              )}
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setLegendOpen(true)}
                className="text-muted-foreground pointer-coarse:hidden"
              >
                <Keyboard /> <span className="hidden sm:inline">Shortcuts</span> <Kbd>?</Kbd>
              </Button>
            </div>
          </div>

          <div className="flex min-h-0 flex-1 flex-row">
            {/* List */}
            <div className="flex min-h-0 w-[300px] shrink-0 flex-col border-r lg:w-[380px]">
              <div
                ref={listRef}
                className="scrollbar-thin min-h-0 flex-1 overflow-y-auto"
                role="listbox"
                aria-label="Items"
              >
                {list.isLoading ? (
                  <div className="grid gap-2 p-3">
                    <SkeletonList count={6} className="h-14" />
                  </div>
                ) : list.error ? (
                  <ErrorState error={list.error} onRetry={() => list.refetch()} className="m-3" />
                ) : items.length === 0 ? null : (
                  <ul>
                    {items.map((it) => (
                      <InboxRow
                        key={it.id}
                        item={it}
                        active={active?.id === it.id}
                        selected={selected.has(it.id)}
                        collectionName={collectionName(it.collection_id)}
                        time={timeAgo(status === 'pending' ? it.created_at : (it.decided_at ?? it.updated_at))}
                        showStatus={status !== 'pending'}
                        onActivate={setActiveId}
                        onToggle={toggleSelect}
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
                      onClick={() => list.fetchNextPage()}
                      disabled={list.isFetchingNextPage}
                    >
                      {list.isFetchingNextPage ? 'Loading…' : 'Load more'}
                    </Button>
                  </div>
                )}
              </div>
              <div className="hidden flex-wrap gap-x-3 gap-y-1 border-t px-3 py-2 text-[11px] text-muted-foreground lg:flex pointer-coarse:hidden">
                {SHORTCUTS.slice(0, 7).map((s) => (
                  <span key={s.label} className="inline-flex items-center gap-1">
                    {s.keys.map((k) => (
                      <Kbd key={k} className="h-4 min-w-4 text-[10px]">
                        {k}
                      </Kbd>
                    ))}
                    {s.label.split(' ')[0].toLowerCase()}
                  </span>
                ))}
              </div>
            </div>

            {/* Preview */}
            <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto">
              {!list.isLoading && !list.error && items.length === 0 ? (
                <div className="flex h-full items-center justify-center p-8">
                  <EmptyState
                    icon={<InboxIcon />}
                    title={status === 'pending' ? 'Inbox zero' : `No ${status} items`}
                    description={
                      status === 'pending'
                        ? 'Nothing is waiting for review. New items from your agents will show up here.'
                        : status === 'expired'
                          ? "Items that nobody reviewed before an expire rule's deadline show up here."
                          : 'Decisions you make in the inbox will be listed here.'
                    }
                    action={
                      status === 'pending' ? (
                        <Button asChild variant="outline" size="sm">
                          <Link href="/integrations">Connect an agent</Link>
                        </Button>
                      ) : undefined
                    }
                  />
                </div>
              ) : active ? (
                <div className="mx-auto grid max-w-4xl gap-4 p-4 sm:p-6" data-testid="inbox-preview">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <h2 className="text-lg font-semibold tracking-tight break-words">{active.title || 'Untitled'}</h2>
                      <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <StatusBadge status={active.status} />
                        <ExternalContentBadge item={active} />
                        <span className="inline-flex items-center gap-1">
                          <TypeIcon type={active.type} className="size-3.5" /> {TYPE_LABELS[active.type]}
                        </span>
                        <span>·</span>
                        <Link
                          href={`/collections/${active.collection_id}`}
                          className="hover:text-foreground hover:underline"
                        >
                          {collectionName(active.collection_id)}
                        </Link>
                        {active.source && (
                          <>
                            <span>·</span>
                            <span>via {active.source}</span>
                          </>
                        )}
                        <span>·</span>
                        <span title={formatDateTime(active.created_at)}>{timeAgo(active.created_at)}</span>
                      </div>
                    </div>
                    <Button asChild variant="ghost" size="sm">
                      <Link href={`/items/${active.id}`}>
                        <ExternalLink /> Details
                      </Link>
                    </Button>
                  </div>

                  <div className="flex flex-wrap items-center gap-1.5" data-testid="inbox-actions">
                    {available.some((a) => a.name === 'approve') && (
                      <>
                        <Button onClick={approve} disabled={busy}>
                          <Check /> Approve{' '}
                          <Kbd className="ml-1 pointer-coarse:hidden bg-primary-foreground/15 text-primary-foreground">
                            A
                          </Kbd>
                        </Button>
                        <Button variant="outline" onClick={edit} disabled={busy}>
                          <Pencil /> Edit & approve <Kbd className="ml-1 pointer-coarse:hidden">E</Kbd>
                        </Button>
                      </>
                    )}
                    {available.some((a) => a.name === 'reject') && (
                      <Button variant="destructive" onClick={reject} disabled={busy}>
                        <X /> Reject{' '}
                        <Kbd className="ml-1 pointer-coarse:hidden bg-destructive/15 text-destructive">R</Kbd>
                      </Button>
                    )}
                    {custom.length > 0 &&
                      custom.length <= 2 &&
                      custom.map((a) => (
                        <Button key={a.name} variant="outline" onClick={() => runAction(a)} disabled={busy}>
                          <Play /> {a.label}
                        </Button>
                      ))}
                    {custom.length > 2 && (
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="outline" disabled={busy}>
                            More actions <ChevronDown />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start">
                          {custom.map((a) => (
                            <DropdownMenuItem key={a.name} onSelect={() => runAction(a)}>
                              <Play /> <span className="flex-1">{a.label}</span>
                              {a.requires_feedback && <span className="text-xs text-muted-foreground">feedback</span>}
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    )}
                    {!actions.isLoading && available.length === 0 && (
                      <span className="text-sm text-muted-foreground">No actions available for this item.</span>
                    )}
                  </div>

                  {active.feedback && (
                    <div className="rounded-lg border-l-2 border-brand/70 bg-muted/50 px-3 py-2 text-sm">
                      <div className="mb-0.5 text-xs font-medium text-muted-foreground">Feedback</div>
                      <div className="whitespace-pre-wrap">{active.feedback}</div>
                    </div>
                  )}

                  <ItemPreview item={active} scaled />

                  {Object.keys(active.metadata ?? {}).length > 0 && (
                    <details className="rounded-lg border bg-card text-sm" open>
                      <summary className="cursor-pointer px-3 py-2 text-xs font-medium text-muted-foreground select-none">
                        Metadata
                      </summary>
                      <dl className="grid gap-x-4 gap-y-1 border-t px-3 py-2 sm:grid-cols-[160px_1fr]">
                        {Object.entries(active.metadata)
                          .slice(0, 20)
                          .map(([k, v]) => (
                            <div key={k} className="contents">
                              <dt className="truncate font-mono text-xs text-muted-foreground">{k}</dt>
                              <dd className="font-mono text-xs break-words">
                                {typeof v === 'string' ? v : JSON.stringify(v)}
                              </dd>
                            </div>
                          ))}
                      </dl>
                    </details>
                  )}
                </div>
              ) : null}
            </div>
          </div>
        </>
      )}

      <FeedbackDialog
        open={!!feedbackFor}
        onOpenChange={(o) => !o && setFeedbackFor(null)}
        action={feedbackFor?.action ?? null}
        count={feedbackFor?.ids.length}
        itemTitle={feedbackFor?.ids.length === 1 ? items.find((i) => i.id === feedbackFor.ids[0])?.title : undefined}
        pending={busy}
        onSubmit={(feedback) => {
          if (!feedbackFor) {
            return
          }

          const { action, ids } = feedbackFor

          if (ids.length > 1 || (ids.length === 1 && selected.size > 0 && ids[0] !== active?.id)) {
            bulk.mutate({ ids, action: action.name, feedback })

            return
          }

          const item = items.find((i) => i.id === ids[0])

          if (item) {
            run.mutate({ item, action: action.name, body: { feedback, expected_version: item.version } })
          }
        }}
      />
      <EditApproveDialog
        item={editing && active ? active : null}
        open={editing}
        onOpenChange={setEditing}
        pending={busy}
        onSubmit={(edits, feedback) => {
          if (!active) {
            return
          }

          run.mutate({
            item: active,
            action: 'approve',
            body: {
              ...(Object.keys(edits).length ? { edits } : {}),
              ...(feedback ? { feedback } : {}),
              expected_version: active.version,
            },
          })
        }}
      />
      <Dialog open={legendOpen} onOpenChange={setLegendOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>Keyboard shortcuts</DialogTitle>
            <DialogDescription>Review without touching the mouse.</DialogDescription>
          </DialogHeader>
          <ul className="grid gap-1.5 text-sm">
            {SHORTCUTS.map((s) => (
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
    </div>
  )
}
