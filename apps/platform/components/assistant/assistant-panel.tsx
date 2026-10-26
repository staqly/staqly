'use client'

import { useQueryClient } from '@tanstack/react-query'
import { ArrowUp, Check, Loader2, Plus, Sparkles, Square, X } from 'lucide-react'
import { usePathname } from 'next/navigation'
import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { useIsMobile } from '@/hooks/use-mobile'
import { assistantChat, errorMessage, type AssistantEvent, type AssistantMessage } from '@/lib/api'
import { useSession } from '@/lib/queries'
import { cn } from '@/lib/utils'

// The assistant: a chat docked on the right (a sheet on phones) that works on the workspace with staqly's tools,
// as the person signed in (POST /v1/assistant/chat). Reading runs at once; every change shows a card to allow or
// deny first. The conversation stays in this browser, per workspace, until New chat.

type ToolCall = NonNullable<AssistantMessage['tool_calls']>[number]

interface Saved {
  messages: AssistantMessage[]
  pending: ToolCall | null
}

const EMPTY: Saved = { messages: [], pending: null }

function storeKey(workspaceId: string) {
  return `staqly.assistant.${workspaceId}`
}

function load(workspaceId: string): Saved {
  try {
    const raw = localStorage.getItem(storeKey(workspaceId))

    return raw ? { ...EMPTY, ...(JSON.parse(raw) as Saved) } : EMPTY
  } catch {
    return EMPTY
  }
}

function save(workspaceId: string, s: Saved) {
  try {
    localStorage.setItem(storeKey(workspaceId), JSON.stringify({ ...s, messages: s.messages.slice(-80) }))
  } catch {
    // A full or blocked storage only loses the chat on reload.
  }
}

const SUGGESTIONS = [
  'Write a LinkedIn post about our newest feature',
  'What is scheduled for this week?',
  'Which posts failed, and why?',
  'Create a collection for Instagram that posts every weekday at 9:00',
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
    <div className="grid gap-3 rounded-xl border border-brand/40 bg-brand-soft/40 p-3" data-testid="assistant-confirm">
      <p className="text-sm font-medium">{toolLabel(call.name)}?</p>
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
            <div
              key={i}
              className="ml-8 self-end rounded-2xl rounded-br-md bg-muted px-3 py-2 text-sm whitespace-pre-wrap"
            >
              {m.content}
            </div>
          )
        }

        if (m.role !== 'assistant') {
          return null
        }

        return (
          <Fragment key={i}>
            {m.content && <Rich text={m.content} />}
            {(m.tool_calls ?? []).map((c) => {
              const res = results.get(c.id)

              if (pending?.id === c.id) {
                return <ConfirmCard key={c.id} call={c} onDecide={onDecide} busy={busy} />
              }

              const failed = res?.startsWith('Error:') || res?.startsWith('The person declined')

              return (
                <p
                  key={c.id}
                  className={cn('flex items-center gap-1.5 text-xs text-muted-foreground', failed && 'line-through')}
                  title={failed ? res : undefined}
                >
                  {res === undefined ? <span className="size-3.5" /> : <Check className="size-3.5 text-brand" />}
                  {toolLabel(c.name)}
                </p>
              )
            })}
          </Fragment>
        )
      })}
      {draft && <Rich text={draft} />}
    </>
  )
}

function Chat({ workspaceId, onClose }: { workspaceId: string; onClose: () => void }) {
  const qc = useQueryClient()
  const pathname = usePathname()
  const [state, setState] = useState<Saved>(() => load(workspaceId))
  const [draft, setDraft] = useState('')
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const abort = useRef<AbortController | null>(null)

  useEffect(() => save(workspaceId, state), [workspaceId, state])
  useEffect(() => () => abort.current?.abort(), [])

  const run = (messages: AssistantMessage[], decision?: { tool_call_id: string; allow: boolean }) => {
    const ac = new AbortController()
    let cur = messages

    abort.current = ac
    setBusy(true)
    setError('')
    setState({ messages, pending: null })
    void assistantChat(
      { messages, decision, page: pathname },
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
        }
      },
      ac.signal,
    )
      .catch((e: unknown) => {
        if (!ac.signal.aborted) {
          setError(errorMessage(e))
        }
      })
      .finally(() => {
        setBusy(false)
        setDraft('')
      })
  }

  const send = (text: string) => {
    const t = text.trim()

    if (!t || busy) {
      return
    }

    setInput('')
    run([...state.messages, { role: 'user', content: t }])
  }

  const decide = (allow: boolean) => {
    if (state.pending) {
      run(state.messages, { tool_call_id: state.pending.id, allow })
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="assistant">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
        <Sparkles className="size-4 text-brand" />
        <h2 className="flex-1 text-sm font-semibold">Assistant</h2>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            abort.current?.abort()
            setState(EMPTY)
            setError('')
          }}
          disabled={state.messages.length === 0}
        >
          <Plus /> New chat
        </Button>
        <Button size="icon-sm" variant="ghost" onClick={onClose} aria-label="Close the assistant">
          <X />
        </Button>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4" aria-live="polite">
        {state.messages.length === 0 ? (
          <div className="grid gap-3 py-6">
            <p className="text-sm text-muted-foreground">
              Ask for anything in this workspace: write and schedule posts, set up collections and schedules, find what
              failed. Changes wait for you to allow them.
            </p>
            <div className="grid gap-1.5">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => send(s)}
                  className="rounded-lg border px-3 py-2 text-left text-sm hover:bg-muted pointer-coarse:py-3"
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
        ) : (
          <Conversation messages={state.messages} draft={draft} pending={state.pending} busy={busy} onDecide={decide} />
        )}
        {busy && !draft && (
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" /> Working on it...
          </p>
        )}
        {error && <p className="text-sm text-red-700 dark:text-red-400">{error}</p>}
        {/* Remounted as the chat grows, so the newest message stays in view. */}
        <div
          key={`${state.messages.length}:${draft.length}:${state.pending?.id ?? ''}`}
          ref={(el) => el?.scrollIntoView({ block: 'end' })}
        />
      </div>
      <form
        className="shrink-0 border-t p-3"
        onSubmit={(e) => {
          e.preventDefault()
          send(input)
        }}
      >
        <div className="flex items-end gap-2 rounded-xl border bg-background p-1.5 focus-within:ring-2 focus-within:ring-ring/40">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                send(input)
              }
            }}
            rows={2}
            maxLength={8000}
            placeholder={state.pending ? 'Allow or deny the change above first' : 'Ask the assistant...'}
            aria-label="Message to the assistant"
            disabled={!!state.pending}
            className="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-1.5 py-1 text-sm outline-none"
          />
          {busy ? (
            <Button
              type="button"
              size="icon-sm"
              variant="outline"
              onClick={() => abort.current?.abort()}
              aria-label="Stop"
            >
              <Square />
            </Button>
          ) : (
            <Button type="submit" size="icon-sm" disabled={!input.trim() || !!state.pending} aria-label="Send">
              <ArrowUp />
            </Button>
          )}
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

  const chat = <Chat key={workspaceId} workspaceId={workspaceId} onClose={() => onOpenChange(false)} />

  if (isMobile) {
    return (
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent side="right" className="w-full gap-0 p-0 sm:max-w-md" showCloseButton={false}>
          <SheetTitle className="sr-only">Assistant</SheetTitle>
          {chat}
        </SheetContent>
      </Sheet>
    )
  }

  return open ? (
    <aside
      aria-label="Assistant"
      className="sticky top-0 flex h-svh w-[min(420px,40vw)] shrink-0 flex-col border-l bg-background"
    >
      {chat}
    </aside>
  ) : null
}
