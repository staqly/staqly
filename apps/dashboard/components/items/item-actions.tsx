'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Archive,
  Check,
  CheckCheck,
  MoreHorizontal,
  Pencil,
  Play,
  RotateCcw,
  Send,
  Sparkles,
  X,
  XCircle,
} from 'lucide-react'
import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Kbd } from '@/components/ui/kbd'
import { Textarea } from '@/components/ui/textarea'
import { publishesOnApproval } from '@/lib/action-catalog'
import { api, ApiError, type ActionBody, type ActionDef, type Collection, type Item, type ItemFields } from '@/lib/api'
import { charCount } from '@/lib/chars'
import { env } from '@/lib/env'
import { focusFirstInvalid, useFormErrors } from '@/lib/form-errors'
import { parseJSON, prettyJSON } from '@/lib/format'
import { itemActionToast, namedToast, quoted, toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, qk, syncItemInCache, useCollections, useItemActions } from '@/lib/queries'
import { statusMeta } from '@/lib/status'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

const NO_ACTIONS: ActionDef[] = []

export const REJECT_ACTION: ActionDef = {
  name: 'reject',
  label: 'Reject',
  event: 'item.rejected',
  requires_feedback: true,
  builtin: true,
}

/** A collection action as a dropdown row: coloured icon, tinted hover. */
function menuTone(a: ActionDef) {
  // Archive stays neutral like Edit; only status-changing actions get a colour.
  if (!a.set_status || a.set_status === 'archived') {
    return undefined
  }

  const { icon, menu } = TONES[statusMeta(a.set_status).tone]

  return cn(icon, menu)
}

/**
 * One style for every collection action (outline); the icon takes the colour of the status it sets. Archiving
 * is worth a second look (orange, like the Archive card), though Archived itself is grey.
 */
export function actionTone(a: ActionDef): { variant: 'outline'; className?: string } {
  if (!a.set_status) {
    return { variant: 'outline' }
  }

  const tone: Tone = a.set_status === 'archived' ? 'attention' : statusMeta(a.set_status).tone

  return { variant: 'outline', className: TONES[tone].icon }
}

/** A collection action as a menu row: its icon, label and the status it moves the item to. */
export function ActionMenuItem({ action, onRun }: { action: ActionDef; onRun: (a: ActionDef) => void }) {
  return (
    <DropdownMenuItem onSelect={() => onRun(action)} className={menuTone(action)}>
      <ActionIcon action={action} />
      <span className="flex-1">{action.label}</span>
      {action.set_status && <span className="text-xs text-muted-foreground">→ {action.set_status}</span>}
    </DropdownMenuItem>
  )
}

// The action worth a button on a decided item, best first: retry a failure, publish an approval.
const MAIN_ACTIONS = ['retry', 'publish']

/** A decided item's main action: Retry or Publish first, else the first that doesn't archive (or any, `orAny`). */
export function mainAction(custom: ActionDef[], orAny = false) {
  return (
    MAIN_ACTIONS.map((n) => custom.find((a) => a.name === n)).find(Boolean) ??
    custom.find((a) => a.set_status !== 'archived') ??
    (orAny ? custom[0] : undefined)
  )
}

/**
 * The action buttons of an item, one layout everywhere. A pending item shows the decision (Reject, Approve)
 * and puts Edit and the collection's actions in a "More" menu; a decided item shows its main collection
 * action and puts the rest (Archive included) in the menu.
 */
export function ActionToolbar({
  canApprove,
  canReject,
  canEdit,
  custom,
  busy,
  size = 'default',
  onApprove,
  onReject,
  onEdit,
  onCustom,
  menuStart,
  menuExtra,
  mainOnly,
}: {
  canApprove: boolean
  canReject: boolean
  canEdit: boolean
  custom: ActionDef[]
  busy: boolean
  size?: 'sm' | 'default' | 'xs'
  onApprove: () => void
  onReject: () => void
  onEdit: () => void
  onCustom: (a: ActionDef) => void
  /** Items at the top of the menu (Open in the item panel). */
  menuStart?: React.ReactNode
  /** Page-level items (download, delete...) added to the same menu, so there is one menu, not two. */
  menuExtra?: React.ReactNode
  /** Inbox rows: the main action only (or the first one when there is none), no More menu. */
  mainOnly?: boolean
}) {
  const deciding = canApprove || canReject
  // A decided item shows its one main action (e.g. Publish); archiving and the rest live in More.
  const main = deciding ? undefined : mainAction(custom, mainOnly)
  const shown = main ? [main] : []
  const more = custom.filter((a) => a !== main)
  const hasMenu = !mainOnly && (canEdit || more.length > 0 || !!menuExtra || !!menuStart)

  return (
    <div className="flex flex-wrap items-center gap-1.5" role="toolbar" aria-label="Item actions">
      {canReject && (
        <Button size={size} variant="reject" onClick={onReject} disabled={busy}>
          <X /> Reject
        </Button>
      )}
      {canApprove && (
        <Button size={size} variant="approve" onClick={onApprove} disabled={busy}>
          <Check /> Approve
        </Button>
      )}
      {shown.map((a) => (
        <Button
          key={a.name}
          size={size}
          variant={actionTone(a).variant}
          className={actionTone(a).className}
          onClick={() => onCustom(a)}
          disabled={busy}
        >
          <ActionIcon action={a} /> {a.label}
        </Button>
      ))}
      {hasMenu && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size={size} variant="outline" disabled={busy} aria-label="More actions">
              <MoreHorizontal /> More
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="min-w-56">
            {menuStart}
            {menuStart && (canEdit || more.length > 0 || !!menuExtra) && <DropdownMenuSeparator />}
            {canEdit && (
              <DropdownMenuItem onSelect={onEdit}>
                <Pencil /> <span className="flex-1">Edit</span>
                <span className="text-xs text-muted-foreground">⇧E</span>
              </DropdownMenuItem>
            )}
            {canEdit && more.length > 0 && <DropdownMenuSeparator />}
            {more.length > 0 && <DropdownMenuLabel>Collection actions</DropdownMenuLabel>}
            {more.map((a) => (
              <ActionMenuItem key={a.name} action={a} onRun={onCustom} />
            ))}
            {menuExtra && (canEdit || more.length > 0) && <DropdownMenuSeparator />}
            {menuExtra}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  )
}

const ICON_BY_NAME: Record<string, typeof Play> = {
  publish: Send,
  archive: Archive,
  regenerate: Sparkles,
  retry: RotateCcw,
}
const ICON_BY_STATUS: Record<string, typeof Play> = { archived: Archive, completed: CheckCheck, failed: XCircle }

/** One icon per action: the built-in and catalog ones by name or the status they set, else a play arrow. */
export function ActionIcon({ action }: { action: ActionDef }) {
  const Icon = ICON_BY_NAME[action.name] ?? ICON_BY_STATUS[action.set_status ?? ''] ?? Play

  return <Icon />
}

export function needsFeedback(action: ActionDef) {
  return action.requires_feedback || action.name === 'reject'
}

function conflictMessage(err: ApiError) {
  return /modified|concurrent/i.test(err.message)
    ? 'This item changed since you loaded it. The latest version has been loaded.'
    : `Already decided — ${err.message}`
}

/** How long an approval waits for Undo before it reaches the API (and its webhooks). Tests set it to 0. */
export const undoDelay = { ms: env.isTest ? 0 : 10_000 }

type RunVars = { item: Item; action: string; body?: ActionBody; deferred?: boolean }

/**
 * Approvals still waiting for Undo, by item id (shared by every useRunAction). Another action on the item
 * (Publish, Archive... offered because it shows as approved) sends its approval first instead of failing.
 */
const pendingApprovals = new Map<string, () => Promise<Item>>()

/** The item's collection from any cached collection list or single collection, without a request. */
function cachedCollection(qc: ReturnType<typeof useQueryClient>, id: string): Collection | undefined {
  const one = qc.getQueryData<Collection>(qk.collection(id))

  if (one) {
    return one
  }

  for (const [, list] of qc.getQueriesData<Collection[]>({ queryKey: qk.collectionsAll })) {
    const found = list?.find((c) => c.id === id)

    if (found) {
      return found
    }
  }

  return undefined
}

/**
 * Mutation that runs approve/reject/collection actions with consistent cache updates & errors. An approval is
 * shown at once (onDone gets the item as approved) but sent after undoDelay, unless Undo in its toast is
 * clicked: then the item is put back and onUndo runs.
 */
export function useRunAction(opts?: {
  onDone?: (item: Item, action: string) => void
  onUndo?: (item: Item) => void
  silent?: boolean
}) {
  const qc = useQueryClient()
  const { onDone, onUndo, silent } = opts ?? {}

  const mutation = useMutation({
    mutationFn: ({ item, action, body }: RunVars) => api.runAction(item.id, action, body),
    onSuccess: (updated, { action, deferred }) => {
      syncItemInCache(qc, updated)
      invalidateAfterItemChange(qc, updated.id)

      // A deferred approval already had its toast and onDone when it was clicked.
      if (deferred) {
        return
      }

      if (!silent) {
        itemActionToast(action, updated)
      }

      onDone?.(updated, action)
    },
    onError: (err, { item }) => {
      if (err instanceof ApiError && err.status === 409) {
        toast.warning(conflictMessage(err))
        void qc.invalidateQueries({ queryKey: qk.item(item.id) })
        void qc.invalidateQueries({ queryKey: qk.itemsAll })
        invalidateAfterItemChange(qc, item.id)

        return
      }

      toastApiError(err)
    },
  })
  const { mutate, mutateAsync } = mutation
  // The latest callbacks, so `run` stays stable for memoised rows and a deferred approval calls current ones.
  const latest = useRef({ onDone, onUndo })

  useLayoutEffect(() => {
    latest.current = { onDone, onUndo }
  })

  const run = useCallback(
    (v: RunVars, options?: Parameters<typeof mutate>[1]) => {
      if (v.action !== 'approve') {
        const flush = pendingApprovals.get(v.item.id)

        if (!flush) {
          mutate(v, options)

          return
        }

        // The approval goes out now; the action then expects the version it produced.
        flush()
          .then((approved) => {
            const body =
              v.body?.expected_version === undefined ? v.body : { ...v.body, expected_version: approved.version }

            mutate({ ...v, item: approved, body }, options)
          })
          .catch(() => {})

        return
      }

      // ponytail: a pending approval lives in this tab only; closing the tab before it is sent drops it.
      const send = () => {
        pendingApprovals.delete(v.item.id)

        return mutateAsync({ ...v, deferred: true })
      }
      const timer = setTimeout(() => void send().catch(() => {}), undoDelay.ms)

      pendingApprovals.set(v.item.id, () => {
        clearTimeout(timer)

        return send()
      })
      const approved: Item = {
        ...v.item,
        status: 'approved',
        decision: 'approved',
        decided_at: new Date().toISOString(),
      }
      // A collection that publishes on approval shows Publishing at once, as the API will return it.
      const autoPublish = publishesOnApproval(cachedCollection(qc, v.item.collection_id))
      const shown: Item = autoPublish
        ? { ...approved, status: 'processing', run: 'processing', run_action: 'publish' }
        : approved

      latest.current.onDone?.(shown, 'approve')
      syncItemInCache(qc, shown)
      toast.success(`${quoted(v.item.title)} was approved`, {
        description: autoPublish ? 'Publishing starts now.' : 'Your agent can go ahead.',
        duration: Math.max(undoDelay.ms, 4000),
        action: undoDelay.ms
          ? {
              label: 'Undo',
              onClick: () => {
                clearTimeout(timer)
                pendingApprovals.delete(v.item.id)
                syncItemInCache(qc, v.item)
                latest.current.onUndo?.(v.item)
              },
            }
          : undefined,
      })
    },
    [mutate, mutateAsync, qc],
  )

  return { ...mutation, mutate: run }
}

/** Collection actions that start your automation (Publish, Regenerate...) are confirmed; archiving is not. */
export function needsConfirm(a: ActionDef) {
  return !a.builtin && a.set_status !== 'archived' && !needsFeedback(a)
}

/** "Publish “Title”?": a collection action runs only after this confirmation. */
export function ConfirmActionDialog({
  action,
  itemTitle,
  onCancel,
  onConfirm,
}: {
  action: ActionDef | null
  itemTitle?: string
  onCancel: () => void
  onConfirm: () => void
}) {
  return (
    <ConfirmDialog
      open={!!action}
      onOpenChange={(o) => !o && onCancel()}
      title={
        <>
          {action?.label} {quoted(itemTitle)}?
        </>
      }
      description={
        <>
          staqly sends the {action?.event} event to your automation
          {action?.set_status ? ` and the item moves to ${action.set_status}` : ''}.
        </>
      }
      confirmLabel={action?.label}
      onConfirm={onConfirm}
    />
  )
}

export function FeedbackDialog({
  open,
  onOpenChange,
  action,
  itemTitle,
  pending,
  onSubmit,
  count,
  followUp,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  action: ActionDef | null
  itemTitle?: string
  pending?: boolean
  /** `followUp`: the follow-up action to run right after (reject and ask to regenerate), if ticked. */
  onSubmit: (feedback: string, followUp?: string) => void
  count?: number
  /** Offered after a reject: the collection's regenerate-style action. */
  followUp?: ActionDef | null
}) {
  const [feedback, setFeedback] = useState('')
  const [andThen, setAndThen] = useState(false)
  // Set by the first submit (button or Enter); from then on the error follows the text.
  const [submitted, setSubmitted] = useState(false)
  // Every opening starts empty, however it was closed (the parent closes it after a submit). Reset on
  // opening, not closing, so the text doesn't vanish while the dialog animates out.
  const [wasOpen, setWasOpen] = useState(open)

  if (open !== wasOpen) {
    setWasOpen(open)

    if (open) {
      setFeedback('')
      setSubmitted(false)
      setAndThen(false)
    }
  }

  const required = action ? needsFeedback(action) : false
  const empty = feedback.trim().length === 0
  const isReject = action?.name === 'reject'
  const invalid = submitted && required && empty

  function submit() {
    if (pending) {
      return
    }

    setSubmitted(true)

    if (required && empty) {
      document.getElementById('action-feedback')?.focus()

      return
    }

    onSubmit(feedback.trim(), isReject && followUp && andThen ? followUp.name : undefined)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {action?.label ?? 'Action'}
            {count && count > 1 ? ` ${count} items${isReject ? '?' : ''}` : ''}
          </DialogTitle>
          <DialogDescription className="text-foreground/75">
            {isReject && count && count > 1 ? `${count} pending items will be rejected. This can't be undone. ` : ''}
            {isReject
              ? 'Tell the agent what to change. Your feedback is saved on the item and sent with the webhook.'
              : required
                ? 'This action requires feedback for the agent.'
                : 'Optional note sent with the action event.'}
          </DialogDescription>
        </DialogHeader>
        {itemTitle && (
          <div className="flex min-w-0 items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-sm">
            <span className="shrink-0 text-xs text-muted-foreground">Item</span>
            <span className="truncate font-medium">{itemTitle}</span>
          </div>
        )}
        <Field data-invalid={invalid}>
          <FieldLabel htmlFor="action-feedback" required={required}>
            Feedback{required ? '' : ' (optional)'}
          </FieldLabel>
          <Textarea
            id="action-feedback"
            rows={4}
            maxLength={750}
            value={feedback}
            placeholder={isReject ? 'e.g. Background is too busy, make the face larger' : 'Add a note...'}
            onChange={(e) => setFeedback(e.target.value)}
            aria-required={required}
            aria-invalid={invalid}
            aria-describedby={invalid ? 'action-feedback-error' : undefined}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                submit()
              }
            }}
          />
          {invalid && <FieldError id="action-feedback-error">Feedback is required for this action</FieldError>}
          {isReject && followUp && (
            <label className="mt-1 flex items-center gap-2 text-sm">
              <Checkbox checked={andThen} onCheckedChange={(v) => setAndThen(!!v)} />
              Ask the agent to {followUp.label.toLowerCase()} with this feedback
            </label>
          )}
          <p className="text-xs text-muted-foreground">
            <Kbd>Enter</Kbd> to submit · <Kbd>Shift</Kbd>+<Kbd>Enter</Kbd> for a new line
          </p>
        </Field>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant={isReject ? 'reject' : 'default'} disabled={pending} onClick={submit}>
            {pending ? 'Sending...' : `${action?.label ?? 'Submit'}${count && count > 1 ? ` ${count}` : ''}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function editableContent(item: Item) {
  return item.type === 'text' || item.type === 'json' || item.type === 'link'
}

/**
 * Edit a pending item's title and content (⇧E in the inbox). It saves the changes only; the item stays
 * pending, so the reviewer approves or rejects it afterwards.
 */
export function EditItemDialog({
  item,
  open,
  onOpenChange,
}: {
  item: Item | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const qc = useQueryClient()
  const save = useMutation({
    mutationFn: ({ it, edits }: { it: Item; edits: ItemFields }) =>
      api.updateItem(it.id, { ...edits, expected_version: it.version }),
    onSuccess: (updated) => {
      syncItemInCache(qc, updated)
      invalidateAfterItemChange(qc, updated.id)
      namedToast(updated.title, 'was updated', 'It is still pending: approve or reject it when ready.')
      onOpenChange(false)
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        {item && (
          <EditItemForm
            key={item.id + item.version}
            item={item}
            pending={save.isPending}
            onSubmit={(edits) => {
              if (Object.keys(edits).length === 0) {
                onOpenChange(false)

                return
              }

              save.mutate({ it: item, edits })
            }}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

// Outside the component: a try/catch around conditional code makes the React Compiler skip the component.
function EditItemForm({
  item,
  pending,
  onSubmit,
  onCancel,
}: {
  item: Item
  pending?: boolean
  onSubmit: (edits: ItemFields) => void
  onCancel: () => void
}) {
  const [title, setTitle] = useState(item.title)
  const [content, setContent] = useState(item.type === 'json' ? prettyJSON(item.content) : item.content)
  const [url, setUrl] = useState(item.url)
  const parsed = item.type === 'json' ? parseJSON(content || 'null') : null
  const clientErrors: { title?: string; url?: string; content?: string } = {}

  if (!title.trim()) {
    clientErrors.title = 'Title is required'
  } else if (charCount(title) > 500) {
    clientErrors.title = 'At most 500 characters'
  }

  // Text and JSON items are their content; a link's content is an optional description.
  if ((item.type === 'text' || item.type === 'json') && !content.trim()) {
    clientErrors.content = item.type === 'json' ? 'JSON is required' : 'Content is required'
  }

  if (item.type === 'link') {
    if (!url.trim()) {
      clientErrors.url = 'URL is required'
    } else if (!/^https?:\/\/\S+$/i.test(url.trim())) {
      clientErrors.url = 'Enter an absolute http(s) URL'
    }
  }

  if (!clientErrors.content && parsed && !parsed.ok) {
    clientErrors.content = 'Content must be valid JSON'
  }

  const form = useFormErrors(clientErrors)
  const errors = form.errors

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()

    if (!form.submit()) {
      focusFirstInvalid(e.currentTarget)

      return
    }

    const edits: ItemFields = {}

    if (title.trim() !== item.title) {
      edits.title = title.trim()
    }

    if (editableContent(item)) {
      if (parsed?.ok) {
        if (JSON.stringify(parsed.value) !== item.content) {
          edits.content = parsed.value
        }
      } else if (item.type !== 'json' && content !== item.content) {
        edits.content = content
      }
    }

    if (item.type === 'link' && url.trim() !== item.url) {
      edits.url = url.trim()
    }

    onSubmit(edits)
  }

  return (
    <form onSubmit={submit} className="grid gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>Edit item</DialogTitle>
        <DialogDescription>
          Fix the title or content. The item stays pending; approve or reject it afterwards.
        </DialogDescription>
      </DialogHeader>
      <Field data-invalid={!!errors.title}>
        <FieldLabel htmlFor="edit-title" required>
          Title
        </FieldLabel>
        <Input
          id="edit-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          aria-invalid={!!errors.title}
          aria-required
        />
        <FieldError>{errors.title}</FieldError>
      </Field>
      {item.type === 'link' && (
        <Field data-invalid={!!errors.url}>
          <FieldLabel htmlFor="edit-url" required>
            URL
          </FieldLabel>
          <Input
            id="edit-url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            className="font-mono text-xs"
            aria-invalid={!!errors.url}
            aria-required
          />
          <FieldError>{errors.url}</FieldError>
        </Field>
      )}
      {editableContent(item) && (
        <Field data-invalid={!!errors.content}>
          <FieldLabel htmlFor="edit-content" required={item.type !== 'link'}>
            {item.type === 'json' ? 'JSON' : item.type === 'link' ? 'Description' : 'Content'}
          </FieldLabel>
          <Textarea
            id="edit-content"
            rows={10}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            className={cn('max-h-[45vh]', item.type === 'json' && 'font-mono text-xs')}
            aria-invalid={!!errors.content}
            aria-required={item.type !== 'link'}
          />
          <FieldError>{errors.content}</FieldError>
        </Field>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving...' : 'Save changes'}
        </Button>
      </DialogFooter>
    </form>
  )
}

/**
 * Action buttons for an item, driven by GET /v1/items/{id}/actions so the
 * buttons always reflect the caller's scopes and the item's current status.
 */
export function ItemActionBar({
  item,
  className,
  size = 'sm',
  showEdit = true,
  onDone,
  compact = false,
  menuStart,
  menuExtra,
}: {
  item: Item
  menuStart?: React.ReactNode
  menuExtra?: React.ReactNode
  className?: string
  size?: 'sm' | 'default' | 'xs'
  showEdit?: boolean
  onDone?: (item: Item, action: string) => void
  /** Touch layout: big Reject / Approve plus a "more" menu (used in sticky mobile bars). */
  compact?: boolean
}) {
  const actions = useItemActions(item)
  const { mutate: run, isPending: running } = useRunAction({ onDone })
  const collections = useCollections(true)
  const [feedbackFor, setFeedbackFor] = useState<ActionDef | null>(null)
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState<ActionDef | null>(null)

  const list = actions.data ?? NO_ACTIONS
  const approve = list.find((a) => a.name === 'approve')
  const reject = list.find((a) => a.name === 'reject')
  const custom = list.filter((a) => !a.builtin)

  function trigger(action: ActionDef) {
    if (needsFeedback(action)) {
      setFeedbackFor(action)

      return
    }

    if (needsConfirm(action)) {
      setConfirming(action)

      return
    }

    run({ item, action: action.name, body: { expected_version: item.version } })
  }

  if (actions.isLoading) {
    return <div className={cn('h-8 w-48 animate-pulse rounded-lg bg-muted', compact && 'h-12 w-full', className)} />
  }

  const dialogs = (
    <>
      <ConfirmActionDialog
        action={confirming}
        itemTitle={item.title}
        onCancel={() => setConfirming(null)}
        onConfirm={() => {
          if (confirming) {
            run({ item, action: confirming.name, body: { expected_version: item.version } })
          }

          setConfirming(null)
        }}
      />
      <FeedbackDialog
        open={!!feedbackFor}
        onOpenChange={(o) => !o && setFeedbackFor(null)}
        action={feedbackFor}
        itemTitle={item.title}
        pending={running}
        followUp={followUpAction(collections.data?.find((c) => c.id === item.collection_id))}
        onSubmit={(feedback, followUp) => {
          if (!feedbackFor) {
            return
          }

          run(
            { item, action: feedbackFor.name, body: { feedback, expected_version: item.version, follow_up: followUp } },
            { onSettled: () => setFeedbackFor(null) },
          )
        }}
      />
      <EditItemDialog item={editing ? item : null} open={editing} onOpenChange={setEditing} />
    </>
  )

  if (compact) {
    if (list.length === 0) {
      return null
    }

    const more = (approve && showEdit) || custom.length > 0

    return (
      <div className={cn('flex items-center gap-2', className)} data-testid="item-actions-compact">
        {reject && (
          <Button
            variant="reject"
            className="h-12 flex-1 rounded-xl text-[15px] pointer-coarse:h-12"
            onClick={() => trigger(reject)}
            disabled={running}
          >
            <X className="size-5" /> Reject
          </Button>
        )}
        {approve && (
          <Button
            variant="approve"
            className="h-12 flex-[1.4] rounded-xl text-[15px] pointer-coarse:h-12"
            onClick={() => trigger(approve)}
            disabled={running}
          >
            <Check className="size-5" /> Approve
          </Button>
        )}
        {!approve && !reject && custom.length > 0 && (
          <Button
            variant={actionTone(custom[0]).variant}
            className={cn('h-12 flex-1 rounded-xl pointer-coarse:h-12', actionTone(custom[0]).className)}
            onClick={() => trigger(custom[0])}
            disabled={running}
          >
            <ActionIcon action={custom[0]} /> {custom[0].label}
          </Button>
        )}
        {more && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                className="size-12 shrink-0 rounded-xl pointer-coarse:size-12"
                aria-label="More actions"
                disabled={running}
              >
                <MoreHorizontal className="size-5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" side="top" className="min-w-56">
              {approve && showEdit && (
                <DropdownMenuItem onSelect={() => setEditing(true)}>
                  <Pencil /> Edit
                </DropdownMenuItem>
              )}
              {custom.length > 0 && <DropdownMenuLabel>Collection actions</DropdownMenuLabel>}
              {custom.map((a) => (
                <ActionMenuItem key={a.name} action={a} onRun={trigger} />
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        {dialogs}
      </div>
    )
  }

  return (
    <div className={cn('flex flex-wrap items-center gap-1.5', className)}>
      <ActionToolbar
        canApprove={!!approve}
        canReject={!!reject}
        canEdit={!!approve && showEdit}
        custom={custom}
        busy={running}
        size={size}
        onApprove={() => approve && trigger(approve)}
        onReject={() => reject && trigger(reject)}
        onEdit={() => setEditing(true)}
        onCustom={trigger}
        menuStart={menuStart}
        menuExtra={menuExtra}
      />

      {dialogs}
    </div>
  )
}

/** Bulk bar "More": the collection actions that also apply to the selection (Regenerate, Archive...). */
export function BulkMoreMenu({
  actions,
  disabled,
  onRun,
}: {
  actions: ActionDef[]
  disabled?: boolean
  onRun: (a: ActionDef) => void
}) {
  if (actions.length === 0) {
    return null
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="sm" variant="outline" disabled={disabled} aria-label="More actions">
          <MoreHorizontal /> More
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="top" className="min-w-48">
        {actions.map((a) => (
          <ActionMenuItem key={a.name} action={a} onRun={onRun} />
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * The action a reject can hand over to: one that starts a run from rejected and takes feedback (Regenerate by
 * default). Null when the collection has none.
 */
export function followUpAction(collection?: { actions?: ActionDef[] } | null) {
  return (
    collection?.actions?.find(
      (a) => a.set_status === 'processing' && a.requires_feedback && a.allowed_statuses?.includes('rejected'),
    ) ?? null
  )
}
