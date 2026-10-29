'use client'

import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  ArrowUp,
  CalendarDays,
  Check,
  FileText,
  FolderPlus,
  History,
  Loader2,
  Maximize2,
  Minimize2,
  Paperclip,
  PenLine,
  Plus,
  RotateCcw,
  Search,
  SearchX,
  Square,
  Trash2,
  X,
} from 'lucide-react'
import { usePathname } from 'next/navigation'
import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react'

import { AssistantIcon } from '@/components/assistant/assistant-icon'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { useIsMobile } from '@/hooks/use-mobile'
import { useAiStatus } from '@/lib/ai-status'
import {
  api,
  ApiError,
  assistantChat,
  errorMessage,
  mediaUrl,
  type AssistantEvent,
  type AssistantMessage,
} from '@/lib/api'
import { timeAgo } from '@/lib/format'
import { useSession } from '@/lib/queries'
import { cn } from '@/lib/utils'

import { answerOnDevice, canRunOnDevice } from './on-device'

// The assistant: a chat docked on the right (a sheet on phones) that works on the workspace with staqly's tools,
// as the person signed in (POST /v1/assistant/chat). Reading runs at once; every change shows a card to allow or
// deny first. The conversation stays in this browser, per workspace, until New chat. When the server can't answer
// (the allowance ran out, its models refused) and it names a model for the device, the chat can go on in this
// browser, text only (on-device.ts), until New chat.

type ToolCall = NonNullable<AssistantMessage['tool_calls']>[number]

interface Saved {
  messages: AssistantMessage[]
  pending: ToolCall | null
}

const EMPTY: Saved = { messages: [], pending: null }

// The open chat is remembered per workspace in this browser (to come back to it); the chats themselves are kept
// on the server, which saves each turn (GET /v1/assistant/chats).
function currentKey(workspaceId: string) {
  return `staqly.assistant.${workspaceId}.current`
}

function loadCurrent(workspaceId: string) {
  try {
    return localStorage.getItem(currentKey(workspaceId))
  } catch {
    return null
  }
}

function saveCurrent(workspaceId: string, id: string) {
  try {
    localStorage.setItem(currentKey(workspaceId), id)
  } catch {
    // A private window: the panel opens a new chat next time.
  }
}

/** A chat's id, named here so its first turn can carry it: ach_ and 16 letters or digits. */
function newChatId() {
  const abc = 'abcdefghijklmnopqrstuvwxyz0123456789'

  return `ach_${Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => abc[b % abc.length]).join('')}`
}

const SUGGESTIONS = [
  { icon: PenLine, text: 'Write a LinkedIn post about our newest feature' },
  { icon: CalendarDays, text: 'What is scheduled for this week?' },
  { icon: SearchX, text: 'Which posts failed, and why?' },
  { icon: FolderPlus, text: 'Create a collection for Instagram that posts every weekday at 9:00' },
]

/** Tools that only read: the rest change something, so the pages refresh after them. */
const READS = /^(list|get|search|preview)_/

/** A tool's name in words: "create_post" -> "Create post". */
function toolLabel(name: string) {
  const s = name.replaceAll('_', ' ')

  return s.charAt(0).toUpperCase() + s.slice(1)
}

/** Inline **bold**, `code` and links of one line. */
function inline(line: string, key: string) {
  const out: ReactNode[] = []
  const re = /\*\*([^*]+)\*\*|`([^`]+)`|\[([^\]]+)\]\((https?:\/\/[^\s)]+|\/[^\s)]*)\)|(https?:\/\/[^\s)]+)/g
  let at = 0
  let m: RegExpExecArray | null

  while ((m = re.exec(line))) {
    out.push(line.slice(at, m.index))

    if (m[1]) {
      out.push(<strong key={`${key}-${m.index}`}>{m[1]}</strong>)
    } else if (m[2]) {
      out.push(
        <code key={`${key}-${m.index}`} className="rounded bg-muted px-1 text-[0.85em]">
          {m[2]}
        </code>,
      )
    } else {
      const href = m[4] ?? m[5]!

      out.push(
        <a
          key={`${key}-${m.index}`}
          href={href}
          className="text-brand underline underline-offset-2"
          {...(href.startsWith('http') ? { target: '_blank', rel: 'noreferrer' } : {})}
        >
          {m[3] ?? href}
        </a>,
      )
    }

    at = m.index + m[0].length
  }

  out.push(line.slice(at))

  return out
}

/** **bold**, `code`, links and "- " lists: enough for the model's short answers. */
function Rich({ text }: { text: string }) {
  const lines = text.split('\n')

  return (
    <div className="grid gap-1.5 text-sm leading-relaxed break-words">
      {lines.map((l, i) => {
        const item = /^\s*(?:[-*]|\d+\.)\s+(.*)$/.exec(l)
        const head = /^#{1,4}\s+(.*)$/.exec(l)

        if (!l.trim()) {
          return null
        }

        if (item) {
          return (
            <div key={i} className="flex gap-2 pl-1">
              <span className="text-muted-foreground">•</span>
              <span>{inline(item[1]!, `l${i}`)}</span>
            </div>
          )
        }

        return (
          <p key={i} className={cn(head && 'font-semibold')}>
            {inline(head ? head[1]! : l, `l${i}`)}
          </p>
        )
      })}
    </div>
  )
}

/** A change waiting for the person: what it does, then Allow or Deny. */
function ConfirmCard({ call, onDecide, busy }: { call: ToolCall; onDecide: (allow: boolean) => void; busy: boolean }) {
  let args: Record<string, unknown> = {}

  try {
    args = JSON.parse(call.arguments) as Record<string, unknown>
  } catch {
    // Shown as no details.
  }

  const entries = Object.entries(args).filter(([, v]) => v !== '' && v !== null && v !== undefined)

  return (
    <div
      className="grid gap-3 rounded-xl border border-brand/40 bg-brand-soft/30 p-3 shadow-xs"
      data-testid="assistant-confirm"
    >
      <p className="flex items-center gap-2 text-sm font-medium">
        <span className="size-2 animate-pulse rounded-full bg-brand" aria-hidden />
        {toolLabel(call.name)}?
      </p>
      {entries.length > 0 && (
        <dl className="grid max-h-56 gap-1.5 overflow-auto text-xs">
          {entries.map(([k, v]) => (
            <div key={k} className="grid gap-0.5">
              <dt className="text-muted-foreground">{toolLabel(k)}</dt>
              <dd className="whitespace-pre-wrap break-words">
                {typeof v === 'string' ? v : JSON.stringify(v, null, 1)}
              </dd>
            </div>
          ))}
        </dl>
      )}
      <div className="flex gap-2">
        <Button size="sm" onClick={() => onDecide(true)} disabled={busy}>
          <Check /> Allow
        </Button>
        <Button size="sm" variant="outline" onClick={() => onDecide(false)} disabled={busy}>
          Deny
        </Button>
      </div>
    </div>
  )
}

function Conversation({
  messages,
  draft,
  pending,
  busy,
  onDecide,
}: {
  messages: AssistantMessage[]
  draft: string
  pending: ToolCall | null
  busy: boolean
  onDecide: (allow: boolean) => void
}) {
  const results = new Map(messages.filter((m) => m.role === 'tool').map((m) => [m.tool_call_id, m.content]))

  return (
    <>
      {messages.map((m, i) => {
        if (m.role === 'user') {
          return (
            <div key={i} className="ml-10 grid max-w-[85%] justify-items-end gap-1.5 self-end">
              {(m.attachments?.length ?? 0) > 0 && (
                <div className="flex flex-wrap justify-end gap-1.5">
                  {m.attachments!.map((a) => (
                    <AttachmentThumb
                      key={a.media_id}
                      name={a.name ?? ''}
                      mime={a.mime_type ?? ''}
                      src={mediaUrl(a.media_id)}
                    />
                  ))}
                </div>
              )}
              {m.content && (
                <div className="rounded-2xl rounded-br-sm bg-primary px-3.5 py-2 text-sm whitespace-pre-wrap text-primary-foreground">
                  {m.content}
                </div>
              )}
            </div>
          )
        }

        if (m.role !== 'assistant') {
          return null
        }

        return (
          <Fragment key={i}>
            {m.content && <AssistantBubble>{<Rich text={m.content} />}</AssistantBubble>}
            {(m.tool_calls ?? []).map((c) => {
              const res = results.get(c.id)

              if (pending?.id === c.id) {
                return <ConfirmCard key={c.id} call={c} onDecide={onDecide} busy={busy} />
              }

              const failed = res?.startsWith('Error:') || res?.startsWith('The person declined')

              return (
                <p
                  key={c.id}
                  className={cn(
                    'ml-9 flex w-fit items-center gap-1.5 rounded-full border bg-muted/40 px-2.5 py-1 text-xs text-muted-foreground',
                    failed && 'line-through',
                  )}
                  title={failed ? res : undefined}
                >
                  {res === undefined ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : failed ? (
                    <X className="size-3.5 text-destructive" />
                  ) : (
                    <Check className="size-3.5 text-brand" />
                  )}
                  {toolLabel(c.name)}
                </p>
              )
            })}
          </Fragment>
        )
      })}
      {draft && (
        <AssistantBubble>
          <Rich text={draft} />
        </AssistantBubble>
      )}
    </>
  )
}

/** A thumbnail's address: only one of the library's (the media id may come from a saved chat). */
function thumbSrc(src?: string) {
  return src && /^\/api\/v1\/media\/[\w%-]+$/.test(src) ? src : undefined
}

/** A file added to a message: a picture's thumbnail, else its name. */
function AttachmentThumb({
  name,
  mime,
  src,
  onRemove,
  busy,
}: {
  name: string
  mime: string
  src?: string
  onRemove?: () => void
  busy?: boolean
}) {
  const safe = thumbSrc(src)

  return (
    <span className="relative inline-flex">
      {mime.startsWith('image/') && safe ? (
        <img src={safe} alt={name} className="size-14 rounded-lg border object-cover" />
      ) : (
        <span className="flex h-14 max-w-40 items-center gap-1.5 rounded-lg border bg-muted/50 px-2.5 text-xs">
          <FileText className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate">{name}</span>
        </span>
      )}
      {busy && (
        <span className="absolute inset-0 grid place-items-center rounded-lg bg-background/60">
          <Loader2 className="size-4 animate-spin" />
        </span>
      )}
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove ${name}`}
          className="absolute -top-1.5 -right-1.5 grid size-5 place-items-center rounded-full border bg-background text-muted-foreground shadow-xs hover:text-foreground"
        >
          <X className="size-3" />
        </button>
      )}
    </span>
  )
}

/** A file waiting in the composer: uploading to the media library, uploaded, or (text) read here. */
interface Draft {
  key: string
  name: string
  mime: string
  mediaId?: string
  text?: string
  error?: string
}

const TEXT_FILE = /\.(txt|md|markdown|csv|tsv|json|xml|html?|ya?ml)$/i
const MAX_TEXT = 100_000

/** The assistant's mark beside its words. */
function AssistantAvatar() {
  return (
    <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-muted" aria-hidden>
      <AssistantIcon className="size-3.5" />
    </span>
  )
}

function AssistantBubble({ children }: { children: ReactNode }) {
  return (
    <div className="flex gap-2">
      <AssistantAvatar />
      <div className="min-w-0 flex-1 pt-0.5">{children}</div>
    </div>
  )
}

/**
 * The person's chats, newest first, a page at a time (there may be very many): search by title, open one, or
 * remove it.
 */
function ChatHistory({
  current,
  onOpen,
  onRemoved,
}: {
  current: string
  onOpen: (id: string) => void
  onRemoved: (id: string) => void
}) {
  const qc = useQueryClient()
  const [q, setQ] = useState('')
  const [query, setQuery] = useState('')

  // The search runs once typing pauses.
  useEffect(() => {
    const t = setTimeout(() => setQuery(q.trim()), 250)

    return () => clearTimeout(t)
  }, [q])

  const chats = useInfiniteQuery({
    queryKey: ['assistant-chats', query],
    queryFn: ({ pageParam }) => api.assistantChats(query, pageParam || undefined),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.deleteAssistantChat(id),
    onSuccess: (_, id) => {
      onRemoved(id)
      void qc.invalidateQueries({ queryKey: ['assistant-chats'] })
    },
  })
  const rows = chats.data?.pages.flatMap((p) => p.data) ?? []

  return (
    <div className="mx-auto flex min-h-0 w-full max-w-2xl flex-1 flex-col">
      <div className="p-3">
        <div className="flex h-9 items-center gap-2 rounded-lg border bg-background px-2.5 focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/30">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search chats"
            aria-label="Search chats"
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        {chats.isPending ? (
          <p className="flex items-center gap-2 px-2 py-3 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading chats...
          </p>
        ) : chats.isError ? (
          <p className="px-2 py-3 text-sm text-destructive">{errorMessage(chats.error)}</p>
        ) : rows.length === 0 ? (
          <p className="px-2 py-8 text-center text-sm text-muted-foreground">
            {query ? 'No chat has these words in its title.' : 'No chats yet.'}
          </p>
        ) : (
          <ul className="grid gap-0.5">
            {rows.map((c) => (
              <li key={c.id} className="group/chat flex items-center rounded-lg hover:bg-muted">
                <button
                  type="button"
                  className={cn(
                    'grid min-w-0 flex-1 gap-0.5 rounded-lg px-3 py-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring',
                    c.id === current && 'bg-muted',
                  )}
                  onClick={() => onOpen(c.id)}
                >
                  <span className="truncate text-sm">{c.title}</span>
                  <span className="text-xs text-muted-foreground">{timeAgo(c.updated_at)}</span>
                </button>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  className="mr-1.5 opacity-0 group-hover/chat:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100"
                  aria-label={`Remove ${c.title}`}
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(c.id)}
                >
                  <Trash2 />
                </Button>
              </li>
            ))}
          </ul>
        )}
        {chats.hasNextPage && (
          <Button
            variant="ghost"
            size="sm"
            className="mt-1 w-full"
            onClick={() => void chats.fetchNextPage()}
            disabled={chats.isFetchingNextPage}
          >
            {chats.isFetchingNextPage && <Loader2 className="animate-spin" />} Load more
          </Button>
        )}
      </div>
    </div>
  )
}

function Chat({
  workspaceId,
  onClose,
  expanded,
  onExpand,
}: {
  workspaceId: string
  onClose: () => void
  /** Full screen on wide screens; absent on phones. */
  expanded?: boolean
  onExpand?: () => void
}) {
  const qc = useQueryClient()
  const pathname = usePathname()
  // The open chat: its id, and its conversation (loaded from the server when it is an old one).
  const [chatId, setChatId] = useState(() => loadCurrent(workspaceId) ?? newChatId())
  const [state, setState] = useState<Saved>(EMPTY)
  const [title, setTitle] = useState('')
  const [history, setHistory] = useState(false)
  const fresh = useRef(false) // a new chat: nothing on the server to load yet
  const [draft, setDraft] = useState('')
  const [input, setInput] = useState('')
  const [files, setFiles] = useState<Draft[]>([])
  const [dragging, setDragging] = useState(false)
  const picker = useRef<HTMLInputElement>(null)
  const uploading = files.some((f) => !f.mediaId && f.text === undefined && !f.error)

  // Pictures, videos and sounds go to the media library (the assistant can put them on a post); text files are
  // read here and go in the message itself.
  const addFiles = (list: FileList | File[]) => {
    for (const file of Array.from(list).slice(0, 10)) {
      const key = `${file.name}:${file.size}:${Math.random()}`
      const mime = file.type || 'application/octet-stream'
      const pending: Draft = { key, name: file.name || 'Pasted picture', mime }

      if (TEXT_FILE.test(file.name) || mime.startsWith('text/')) {
        setFiles((f) => [...f, pending])
        void file.text().then(
          (t) =>
            setFiles((f) =>
              f.map((d) =>
                d.key === key
                  ? t.length > MAX_TEXT
                    ? { ...d, text: t.slice(0, MAX_TEXT), error: undefined }
                    : { ...d, text: t }
                  : d,
              ),
            ),
          () => setFiles((f) => f.map((d) => (d.key === key ? { ...d, error: 'Could not be read' } : d))),
        )
        continue
      }

      if (!/^(image|video|audio)\//.test(mime)) {
        setFiles((f) => [...f, { ...pending, error: 'Only pictures, videos, sounds and text files' }])
        continue
      }

      setFiles((f) => [...f, pending]) // its thumbnail shows once it is in the library
      api.uploadMedia(file, pending.name).then(
        (m) => setFiles((f) => f.map((d) => (d.key === key ? { ...d, mediaId: m.id } : d))),
        (e: unknown) => setFiles((f) => f.map((d) => (d.key === key ? { ...d, error: errorMessage(e) } : d))),
      )
    }
  }
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // The server couldn't answer and a model for this device is there: offer it; onDevice once the person took it.
  const [offer, setOffer] = useState(false)
  const [onDevice, setOnDevice] = useState(false)
  const [progress, setProgress] = useState<number | null>(null)
  const browserModel = useAiStatus().data?.assistant_browser_model
  const abort = useRef<AbortController | null>(null)

  useEffect(() => {
    saveCurrent(workspaceId, chatId)

    if (fresh.current) {
      return
    }

    let live = true

    api
      .assistantChatById(chatId)
      .then((c) => {
        if (live) {
          setState({ messages: c.messages ?? [], pending: c.pending ?? null })
          setTitle(c.title)
        }
      })
      .catch(() => live && setState(EMPTY)) // gone (removed, or never saved): a new chat under this id

    return () => {
      live = false
    }
  }, [workspaceId, chatId])

  const switchTo = (id: string, isNew = false) => {
    abort.current?.abort()
    fresh.current = isNew
    setChatId(id)
    setState(EMPTY)
    setTitle('')
    setHistory(false)
    setError('')
    setOffer(false)
    setOnDevice(false)
  }

  const lastUser = state.messages.toReversed().find((m) => m.role === 'user')
  // Try again: the conversation up to the last thing the person asked, sent once more.
  const retry = () => {
    const at = state.messages.lastIndexOf(lastUser!)

    run(state.messages.slice(0, at + 1))
  }

  useEffect(() => () => abort.current?.abort(), [])

  const offerDevice = (code?: string) =>
    setOffer(!!browserModel && canRunOnDevice() && (code === 'ai_limit_reached' || code === 'ai_failed'))

  const runOnDevice = (messages: AssistantMessage[]) => {
    const ac = new AbortController()

    abort.current = ac
    setBusy(true)
    setError('')
    setOffer(false)
    setState({ messages, pending: null })
    void answerOnDevice(
      browserModel ?? '',
      messages,
      (t) => setDraft((d) => d + t),
      (p) => setProgress(p < 1 ? p : null),
      ac.signal,
    )
      .then((text) => {
        if (text) {
          setState({ messages: [...messages, { role: 'assistant', content: text }], pending: null })
        }
      })
      .catch((e: unknown) => {
        if (!ac.signal.aborted) {
          setError(e instanceof Error ? e.message : 'The model could not run on this device.')
        }
      })
      .finally(() => {
        setBusy(false)
        setDraft('')
        setProgress(null)
      })
  }

  const run = (messages: AssistantMessage[], decision?: { tool_call_id: string; allow: boolean }) => {
    const ac = new AbortController()
    let cur = messages

    abort.current = ac
    setBusy(true)
    setError('')
    setOffer(false)
    setState({ messages, pending: null })
    void assistantChat(
      { messages, decision, page: pathname, chat_id: chatId },
      (e: AssistantEvent) => {
        if (e.type === 'text') {
          setDraft((d) => d + (e.text ?? ''))
        } else if (e.type === 'message' && e.message) {
          cur = [...cur, e.message]
          setState({ messages: cur, pending: null })

          if (e.message.role === 'assistant') {
            setDraft('')
          }
        } else if (e.type === 'tool' && e.ok && e.tool && !READS.test(e.tool)) {
          void qc.invalidateQueries()
        } else if (e.type === 'confirm' && e.tool_call) {
          setState({ messages: cur, pending: e.tool_call })
        } else if (e.type === 'error') {
          setError(e.error?.message ?? 'Something went wrong; try again.')
          offerDevice(e.error?.code)
        }
      },
      ac.signal,
    )
      .catch((e: unknown) => {
        if (!ac.signal.aborted) {
          setError(errorMessage(e))
          offerDevice(e instanceof ApiError ? e.code : 'ai_failed')
        }
      })
      .finally(() => {
        setBusy(false)
        setDraft('')
        void qc.invalidateQueries({ queryKey: ['assistant-chats'] })
      })
  }

  const send = (text: string) => {
    const ready = files.filter((f) => !f.error)
    const texts = ready.filter((f) => f.text !== undefined)
    const media = ready.filter((f) => f.mediaId)
    const t = [text.trim(), ...texts.map((f) => `File ${f.name}:\n\`\`\`\n${f.text}\n\`\`\``)]
      .filter(Boolean)
      .join('\n\n')

    if ((!t && media.length === 0) || busy || uploading) {
      return
    }

    setInput('')
    setFiles([])

    const next: AssistantMessage[] = [
      ...state.messages,
      {
        role: 'user',
        content: t,
        ...(media.length > 0 && {
          attachments: media.map((f) => ({ media_id: f.mediaId!, name: f.name, mime_type: f.mime })),
        }),
      },
    ]

    if (onDevice) {
      runOnDevice(next)
    } else {
      run(next)
    }
  }

  const decide = (allow: boolean) => {
    if (state.pending) {
      run(state.messages, { tool_call_id: state.pending.id, allow })
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="assistant">
      <div className="flex h-12 shrink-0 items-center gap-1 border-b px-3">
        <AssistantAvatar />
        <div className="ml-1 grid min-w-0 flex-1 leading-tight">
          <h2 className="truncate text-sm font-semibold">
            {history ? 'Chats' : title || state.messages.find((m) => m.role === 'user')?.content || 'AI Assistant'}
          </h2>
          {onDevice && <span className="text-xs text-muted-foreground">On this device</span>}
        </div>
        <Button
          size="icon-sm"
          variant={history ? 'secondary' : 'ghost'}
          aria-label="Chats"
          aria-pressed={history}
          tooltip="Chats"
          onClick={() => setHistory((h) => !h)}
        >
          <History />
        </Button>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label="New chat"
          tooltip="New chat"
          onClick={() => switchTo(newChatId(), true)}
          disabled={state.messages.length === 0 && !history}
        >
          <Plus />
        </Button>
        {onExpand && (
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label={expanded ? 'Back to the side panel' : 'Full screen'}
            tooltip={expanded ? 'Back to the side panel' : 'Full screen'}
            onClick={onExpand}
          >
            {expanded ? <Minimize2 /> : <Maximize2 />}
          </Button>
        )}
        <Button size="icon-sm" variant="ghost" onClick={onClose} aria-label="Close the assistant" tooltip="Close">
          <X />
        </Button>
      </div>
      {history && (
        <ChatHistory
          current={chatId}
          onOpen={(id) => (id === chatId ? setHistory(false) : switchTo(id))}
          onRemoved={(id) => id === chatId && switchTo(newChatId(), true)}
        />
      )}
      <div hidden={history} className="@container min-h-0 flex-1 overflow-y-auto" aria-live="polite">
        <div
          className={cn('mx-auto flex min-h-full w-full max-w-2xl flex-col gap-4 p-4', expanded && 'max-w-3xl py-8')}
        >
          {state.messages.length === 0 ? (
            <div className="my-auto grid justify-items-center gap-5 py-8 text-center">
              <span className="grid size-12 place-items-center rounded-2xl bg-muted">
                <AssistantIcon className="size-6" />
              </span>
              <div className="grid gap-1.5">
                <p className="text-lg font-semibold">How can I help?</p>
                <p className="max-w-sm text-sm text-muted-foreground">
                  Write and schedule posts, set up collections, find what failed. Changes wait for you to allow them.
                </p>
              </div>
              {/* Inset from the panel's edges, with room inside each option. */}
              <div className="grid w-full gap-2 px-2 text-left sm:px-4 @xl:grid-cols-2">
                {SUGGESTIONS.map(({ icon: Icon, text }) => (
                  <button
                    key={text}
                    type="button"
                    onClick={() => send(text)}
                    className="flex items-start gap-2.5 rounded-xl border bg-card px-4 py-2.5 text-left text-sm transition-colors outline-none hover:border-brand/40 hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:py-3"
                  >
                    <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    {text}
                  </button>
                ))}
              </div>
            </div>
          ) : (
            <Conversation
              messages={state.messages}
              draft={draft}
              pending={state.pending}
              busy={busy}
              onDecide={decide}
            />
          )}
          {busy && !draft && (
            <div className="flex items-center gap-2">
              <AssistantAvatar />
              {progress === null ? (
                <span className="flex gap-1" aria-label="Working on it">
                  {[0, 150, 300].map((d) => (
                    <span
                      key={d}
                      className="size-1.5 animate-bounce rounded-full bg-muted-foreground/60"
                      style={{ animationDelay: `${d}ms` }}
                    />
                  ))}
                </span>
              ) : (
                <span className="text-xs text-muted-foreground">
                  Loading the model on this device... {Math.round(progress * 100)}%
                </span>
              )}
            </div>
          )}
          {error && (
            <div
              role="alert"
              className="flex items-start gap-2.5 rounded-xl border border-destructive/30 bg-destructive/5 p-3 text-sm"
            >
              <AlertCircle className="mt-0.5 size-4 shrink-0 text-destructive" />
              <div className="grid min-w-0 flex-1 gap-2">
                <p className="break-words">{error}</p>
                {lastUser && !busy && (
                  <Button size="sm" variant="outline" className="justify-self-start" onClick={retry}>
                    <RotateCcw /> Try again
                  </Button>
                )}
              </div>
            </div>
          )}
          {offer && !busy && (
            <div className="grid gap-2 rounded-xl border bg-card p-3" data-testid="assistant-on-device">
              <p className="text-sm">
                Continue on this device instead? The assistant then runs in this browser: free, and nothing leaves it,
                but it only writes text and cannot change anything in your workspace. The first time downloads the model
                (a few GB).
              </p>
              <Button
                size="sm"
                className="justify-self-start"
                onClick={() => {
                  setOnDevice(true)
                  runOnDevice(state.messages)
                }}
              >
                Continue on this device
              </Button>
            </div>
          )}
          {/* Remounted as the chat grows, so the newest message stays in view. */}
          <div
            key={`${state.messages.length}:${draft.length}:${state.pending?.id ?? ''}`}
            ref={(el) => el?.scrollIntoView({ block: 'end' })}
          />
        </div>
      </div>
      <form
        hidden={history}
        className={cn('mx-auto w-full max-w-2xl shrink-0 p-3', expanded && 'max-w-3xl pb-6')}
        onSubmit={(e) => {
          e.preventDefault()
          send(input)
        }}
      >
        <div
          className={cn(
            'rounded-2xl border bg-background shadow-xs transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/30',
            dragging && 'border-ring ring-3 ring-ring/30',
          )}
          onDragOver={(e) => {
            if (e.dataTransfer.types.includes('Files')) {
              e.preventDefault()
              setDragging(true)
            }
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(e) => {
            e.preventDefault()
            setDragging(false)
            addFiles(e.dataTransfer.files)
          }}
        >
          {files.length > 0 && (
            <div className="flex flex-wrap gap-2 px-3 pt-3" aria-label="Files to send">
              {files.map((f) => (
                <span key={f.key} className="grid gap-0.5" title={f.error}>
                  <AttachmentThumb
                    name={f.name}
                    mime={f.mime}
                    src={f.mediaId ? mediaUrl(f.mediaId) : undefined}
                    busy={!f.mediaId && f.text === undefined && !f.error}
                    onRemove={() => setFiles((all) => all.filter((d) => d.key !== f.key))}
                  />
                  {f.error && <span className="max-w-24 truncate text-[10px] text-destructive">{f.error}</span>}
                </span>
              ))}
            </div>
          )}
          <input
            ref={picker}
            type="file"
            multiple
            hidden
            accept="image/*,video/*,audio/*,.txt,.md,.csv,.json,.xml,.html,.yaml,.yml"
            onChange={(e) => {
              if (e.target.files) {
                addFiles(e.target.files)
              }

              e.target.value = ''
            }}
          />
          <textarea
            onPaste={(e) => {
              // A screenshot pasted from the clipboard comes as a file.
              if (e.clipboardData.files.length > 0) {
                e.preventDefault()
                addFiles(e.clipboardData.files)
              }
            }}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                send(input)
              }
            }}
            rows={1}
            maxLength={8000}
            placeholder={state.pending ? 'Allow or deny the change above first' : 'Ask the assistant...'}
            aria-label="Message to the assistant"
            disabled={!!state.pending}
            className="block max-h-48 min-h-11 w-full resize-none bg-transparent px-3.5 pt-3 text-sm outline-none [field-sizing:content] placeholder:text-muted-foreground"
          />
          <div className="flex items-center gap-2 px-2 pb-2">
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              className="rounded-full"
              aria-label="Add files"
              tooltip="Add pictures or files (or paste a screenshot)"
              onClick={() => picker.current?.click()}
              disabled={!!state.pending}
            >
              <Paperclip />
            </Button>
            <span className="flex-1 truncate text-xs text-muted-foreground max-sm:hidden">
              Enter to send, Shift+Enter for a new line
            </span>
            {busy ? (
              <Button
                type="button"
                size="icon-sm"
                variant="outline"
                className="ml-auto rounded-full"
                onClick={() => abort.current?.abort()}
                aria-label="Stop"
              >
                <Square className="fill-current" />
              </Button>
            ) : (
              <Button
                type="submit"
                size="icon-sm"
                className="ml-auto rounded-full"
                disabled={
                  (!input.trim() && !files.some((f) => f.mediaId || f.text !== undefined)) ||
                  uploading ||
                  !!state.pending
                }
                aria-label="Send"
              >
                <ArrowUp />
              </Button>
            )}
          </div>
        </div>
      </form>
    </div>
  )
}

/** Docked on the right on wider screens, a sheet on phones. */
export function AssistantPanel({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const session = useSession()
  const isMobile = useIsMobile()
  const workspaceId = session.data?.workspace.id

  if (!workspaceId) {
    return null
  }

  if (isMobile) {
    const chat = <Chat key={workspaceId} workspaceId={workspaceId} onClose={() => onOpenChange(false)} />

    return (
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent side="right" className="w-full gap-0 p-0 sm:max-w-md" showCloseButton={false}>
          <SheetTitle className="sr-only">AI Assistant</SheetTitle>
          {chat}
        </SheetContent>
      </Sheet>
    )
  }

  return <Docked workspaceId={workspaceId} open={open} onClose={() => onOpenChange(false)} />
}

/** Keeps the docked panel mounted while it slides out, then lets it go. */
function Docked({ workspaceId, open, onClose }: { workspaceId: string; open: boolean; onClose: () => void }) {
  const [mounted, setMounted] = useState(open)

  if (open && !mounted) {
    setMounted(true)
  }

  return mounted ? (
    <DockedPanel workspaceId={workspaceId} open={open} onClose={onClose} onGone={() => setMounted(false)} />
  ) : null
}

const MIN_WIDTH = 320
// How long the panel takes to slide in or out (the width transition's duration-200).
const SLIDE_MS = 200
const DEFAULT_WIDTH = 420
// Dragged narrower than this, the panel closes (it reopens at its last real width).
const CLOSE_BELOW = 220
const WIDTH_KEY = 'staqly.assistant.width'
// The sidebar (16rem) and a page that still reads well (40rem) stay beside the panel.
const PAGE_MIN = 896

function savedWidth() {
  try {
    const w = Number(localStorage.getItem(WIDTH_KEY))

    return w >= MIN_WIDTH ? w : DEFAULT_WIDTH
  } catch {
    return DEFAULT_WIDTH
  }
}

/**
 * The panel on the right, in the sidebar's colour so it reads apart from the page. Its left edge drags to resize
 * (at least MIN_WIDTH, at most most of the window); dragged nearly shut it closes. The edge also takes the arrow
 * keys, 16 px a press.
 */
function DockedPanel({
  workspaceId,
  open,
  onClose,
  onGone,
}: {
  workspaceId: string
  open: boolean
  onClose: () => void
  onGone: () => void
}) {
  const [width, setWidth] = useState(savedWidth)
  const [expanded, setExpanded] = useState(false)
  // It slides in from the right edge when it opens and back out when it closes (Close, ⌘J or the sidebar's
  // Assistant): width 0 before its first frame and once closed, then it unmounts.
  const [entered, setEntered] = useState(false)
  const shut = !entered || !open

  useEffect(() => {
    const frame = requestAnimationFrame(() => setEntered(true))

    return () => cancelAnimationFrame(frame)
  }, [])

  useEffect(() => {
    if (open) {
      return
    }

    const t = window.setTimeout(onGone, SLIDE_MS)

    return () => window.clearTimeout(t)
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- onGone only clears the parent's flag
  }, [open])
  // One element whatever the mode, so the chat (and an answer on its way) stays mounted.
  const children = (
    <Chat
      key={workspaceId}
      workspaceId={workspaceId}
      onClose={onClose}
      expanded={expanded}
      onExpand={() => setExpanded((e) => !e)}
    />
  )

  // Full screen: over the whole dashboard, Escape back to the side.
  useEffect(() => {
    if (!expanded) {
      return
    }

    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setExpanded(false)

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [expanded])

  const [dragging, setDragging] = useState(false)
  // A narrower window (or a width saved on a wider one) gives the page its room back.
  const [viewport, setViewport] = useState(() => window.innerWidth)

  useEffect(() => {
    const onResize = () => setViewport(window.innerWidth)

    window.addEventListener('resize', onResize)

    return () => window.removeEventListener('resize', onResize)
  }, [])

  const shown = dragging ? width : Math.min(width, Math.max(MIN_WIDTH, Math.min(viewport * 0.5, viewport - PAGE_MIN)))
  // The page beside it keeps room to work: at least PAGE_MIN wide next to the sidebar.
  const max = () => Math.max(MIN_WIDTH, Math.min(Math.round(window.innerWidth * 0.5), window.innerWidth - PAGE_MIN))
  const keep = (w: number) => {
    const next = Math.min(max(), Math.max(MIN_WIDTH, w))

    setWidth(next)

    try {
      localStorage.setItem(WIDTH_KEY, String(next))
    } catch {
      // A private window: the width lasts until the page reloads.
    }
  }

  const startDrag = (e: React.PointerEvent<HTMLDivElement>) => {
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    setDragging(true)
  }

  const drag = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragging) {
      setWidth(Math.min(max(), window.innerWidth - e.clientX))
    }
  }

  const endDrag = () => {
    if (!dragging) {
      return
    }

    setDragging(false)

    if (width < CLOSE_BELOW) {
      setWidth(savedWidth())
      onClose()
    } else {
      keep(width)
    }
  }

  return (
    <aside
      aria-label="AI Assistant"
      style={expanded ? undefined : { width: shut ? 0 : Math.max(shown, 0) }}
      className={cn(
        expanded
          ? 'fixed inset-0 z-50 flex flex-col bg-background'
          : 'sticky top-0 flex h-svh shrink-0 flex-col overflow-x-clip border-l bg-sidebar shadow-[-8px_0_24px_-12px_rgb(0_0_0/0.35)]',
        !expanded && !dragging && 'transition-[width] duration-200 ease-out motion-reduce:transition-none',
        dragging && 'select-none',
      )}
    >
      {/* The edge: drag to resize, or focus and use the arrow keys. */}
      <div
        hidden={expanded}
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize the assistant"
        aria-valuemin={MIN_WIDTH}
        aria-valuenow={Math.round(width)}
        tabIndex={0}
        onPointerDown={startDrag}
        onPointerMove={drag}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onKeyDown={(e) => {
          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
            e.preventDefault()
            keep(width + (e.key === 'ArrowLeft' ? 16 : -16))
          }
        }}
        className={cn(
          'absolute inset-y-0 left-0 z-20 w-2 cursor-col-resize outline-none after:absolute after:inset-y-0 after:left-1/2 after:w-px after:-translate-x-1/2 after:transition-colors hover:after:bg-ring focus-visible:after:w-0.5 focus-visible:after:bg-ring',
          dragging && 'after:bg-ring',
        )}
      />
      {/* The chat keeps its width while the panel slides, so its text doesn't reflow on the way. */}
      <div
        className="flex min-h-0 flex-1 flex-col"
        style={expanded ? undefined : { width: Math.max(shown, 0), minWidth: Math.max(shown, 0) }}
      >
        {children}
      </div>
      {!expanded && width < MIN_WIDTH && (
        <p className="absolute inset-0 z-10 grid place-items-center bg-sidebar/90 px-4 text-center text-sm text-muted-foreground">
          Release to close
        </p>
      )}
    </aside>
  )
}
