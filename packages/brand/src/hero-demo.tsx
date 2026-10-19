'use client'

import { Check, Clock, Terminal, X, Zap } from 'lucide-react'
import { useEffect, useRef, useState, useSyncExternalStore } from 'react'

type Scenario = {
  agent: string
  prompt: string
  collection: string
  kind: string
  title: string
  content: string
  meta: string[]
  done: string
  result: string
  image?: string // a file under the app's public/ (the app serves /demo/golden-hour.svg)
}

// Use cases of the landing page (apps/web/lib/use-cases.ts), as an agent would send them.
const scenarios: Scenario[] = [
  {
    agent: 'n8n',
    prompt: 'Generate today’s Instagram post',
    collection: 'Instagram Avatar',
    kind: 'instagram.post',
    title: 'Golden hour on the rooftop',
    content: 'Chasing the last light of the day ☀️ #goldenhour #citylife',
    meta: ['seed 42', 'flux-1.1'],
    done: 'Published',
    result: 'instagram_post_id 17901234567890123',
    image: '/demo/golden-hour.svg',
  },
  {
    agent: 'support-agent',
    prompt: 'Handle ticket #6021: mug arrived cracked',
    collection: 'Refunds',
    kind: 'payment.refund',
    title: 'Refund $84.00 — order #10432',
    content:
      'Customer sent a photo of the cracked mug on ticket #6021. Within the 30-day policy; adjust the amount before approving if needed.',
    meta: ['ticket #6021', 'within policy'],
    done: 'Refunded',
    result: 'refund_id re_3QxK8L2eZvKYlo2C',
  },
  {
    agent: 'claude',
    prompt: 'Write a LinkedIn post about the v1.4 launch',
    collection: 'Social',
    kind: 'linkedin.post',
    title: 'v1.4 is live 🚀',
    content:
      'Approve what your agents make with one tap from Slack or Telegram. v1.4 also brings a review queue on your phone. Changelog in the comments 👇',
    meta: ['LinkedIn', 'scheduled 10:00'],
    done: 'Posted',
    result: 'urn:li:share:7152384950617283',
  },
  {
    agent: 'codex',
    prompt: 'Tell Acme the release slips to Thursday',
    collection: 'Outbox',
    kind: 'email.send',
    title: 'Reply to Acme: v1.4 moves to Thursday',
    content: 'Hi Sam, v1.4 ships Thursday instead of Monday so we can finish the migration safely. Sorry for the wait!',
    meta: ['to sam@acme.com', 'draft'],
    done: 'Sent',
    result: 'message_id <8f2c1a@staqly>',
  },
]

type Phase = 'proposing' | 'pending' | 'approved' | 'done'

const AUTO_APPROVE_MS = 3200
const STEP_MS = 900

const REDUCED_MOTION = '(prefers-reduced-motion: reduce)'

function subscribeReducedMotion(onChange: () => void) {
  const mq = window.matchMedia(REDUCED_MOTION)

  mq.addEventListener('change', onChange)

  return () => mq.removeEventListener('change', onChange)
}

const reducedMotion = () => window.matchMedia(REDUCED_MOTION).matches
const noReducedMotion = () => false

/**
 * Hero demo of the loop: an agent proposes, the item waits in a review card, it gets approved, the agent
 * reports back. Display only, nothing is clickable. The loop runs only while the demo is on screen; with
 * reduced motion it shows the finished first scenario and stays still.
 */
export function HeroDemo({ basePath = '' }: { basePath?: string }) {
  const root = useRef<HTMLDivElement>(null)
  const [onScreen, setOnScreen] = useState(false)
  const still = useSyncExternalStore(subscribeReducedMotion, reducedMotion, noReducedMotion)
  const [index, setIndex] = useState(0)
  const [phase, setPhase] = useState<Phase>('proposing')
  const shownIndex = still ? 0 : index
  const shownPhase: Phase = still ? 'done' : phase
  const s = scenarios[shownIndex]

  // Pause the loop while the demo is scrolled out of view.
  useEffect(() => {
    const el = root.current

    if (!el) {
      return
    }

    const io = new IntersectionObserver(([entry]) => setOnScreen(entry.isIntersecting))

    io.observe(el)

    return () => io.disconnect()
  }, [])

  // Drive the loop. Every step is a timer, so state only changes asynchronously.
  useEffect(() => {
    if (still || !onScreen) {
      return
    }

    const next: Record<Phase, [number, () => void]> = {
      proposing: [STEP_MS, () => setPhase('pending')],
      pending: [AUTO_APPROVE_MS, () => setPhase('approved')],
      approved: [STEP_MS * 1.4, () => setPhase('done')],
      done: [
        STEP_MS * 2.6,
        () => {
          setIndex((index + 1) % scenarios.length)
          setPhase('proposing')
        },
      ],
    }
    const [ms, step] = next[phase]
    const t = setTimeout(step, ms)

    return () => clearTimeout(t)
  }, [phase, index, onScreen, still])

  const approved = shownPhase === 'approved' || shownPhase === 'done'

  return (
    <div ref={root} className="relative min-w-0 select-none" aria-hidden>
      {/* The agent's side of the conversation */}
      <div className="relative z-0 rounded-2xl border border-border bg-card/90 shadow-[0_30px_80px_-30px_rgb(0_0_0/0.5)] backdrop-blur">
        <div className="flex items-center gap-2 border-b border-border px-4 py-2.5">
          <span className="size-2.5 rounded-full bg-[#ff5f57]" />
          <span className="size-2.5 rounded-full bg-[#febc2e]" />
          <span className="size-2.5 rounded-full bg-[#28c840]" />
          <span className="ml-2 flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
            <Terminal className="size-3.5" /> {s.agent}
          </span>
        </div>
        <div
          key={shownIndex}
          className="min-h-[9.5rem] space-y-1.5 overflow-hidden px-4 pt-3 pb-12 font-mono text-[12px] leading-relaxed whitespace-nowrap sm:text-[12.5px]"
        >
          <p className="sq-demo-in truncate">
            <span className="text-brand">❯</span> “{s.prompt}”
          </p>
          <p className="sq-demo-in truncate text-muted-foreground" style={{ animationDelay: '300ms' }}>
            ⏺ staqly · <span className="text-foreground">create_item</span> → {s.collection}
          </p>
          {shownPhase !== 'proposing' && (
            <p className="sq-demo-in truncate text-muted-foreground">
              ⏺ staqly · <span className="text-foreground">await_decision</span>{' '}
              {approved ? (
                <span className="text-brand-strong">→ approved</span>
              ) : (
                <span className="sq-dots">waiting</span>
              )}
            </p>
          )}
          {shownPhase === 'done' && (
            <p className="sq-demo-in truncate text-muted-foreground">
              ⏺ staqly · <span className="text-foreground">report_result</span> →{' '}
              <span className="text-brand-strong">{s.result}</span>
            </p>
          )}
        </div>
      </div>

      {/* The human's side: the review card */}
      <div className="relative z-10 -mt-9 ml-3 sm:ml-10">
        <div
          key={shownIndex}
          className="sq-demo-card relative rounded-2xl border border-border bg-background/95 p-4 shadow-[0_40px_90px_-30px_rgb(0_0_0/0.6)] backdrop-blur sm:p-5"
        >
          <div className="flex items-center gap-2 text-xs">
            <span className="rounded-full border border-[color-mix(in_oklch,var(--brand)_35%,transparent)] bg-brand-soft px-2.5 py-0.5 font-mono font-medium text-brand-strong">
              {s.kind}
            </span>
            <span className="truncate text-muted-foreground">
              {s.collection} · from <span className="text-foreground">{s.agent}</span>
            </span>
          </div>

          <div className="mt-3 flex gap-3">
            {s.image && (
              <img
                src={`${basePath}${s.image}`}
                alt=""
                width={160}
                height={160}
                className="size-20 shrink-0 rounded-xl ring-1 ring-border sm:size-24"
              />
            )}
            <div className="min-w-0 flex-1">
              <h3 className="truncate font-semibold tracking-tight">{s.title}</h3>
              <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{s.content}</p>
            </div>
          </div>

          <div className="mt-3 flex flex-wrap gap-1.5">
            {s.meta.map((m) => (
              <code key={m} className="rounded-md bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
                {m}
              </code>
            ))}
          </div>

          <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
            <Status phase={shownPhase} done={s.done} />
            <div className={`flex gap-2 transition-opacity ${shownPhase === 'pending' ? '' : 'opacity-40'}`}>
              <span className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-sm font-medium text-red-400">
                <X className="size-4" /> Reject
              </span>
              <span
                className={`inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-sm font-medium text-brand ${shownPhase === 'pending' ? 'sq-demo-press' : ''}`}
                style={{ animationDuration: `${AUTO_APPROVE_MS}ms` }}
              >
                <Check className="size-4" /> Approve
              </span>
            </div>
          </div>
        </div>
      </div>

      <div className="mt-5 flex items-center justify-center gap-2">
        {scenarios.map((x, i) => (
          <span
            key={x.kind}
            className={`h-1.5 rounded-full transition-all ${i === shownIndex ? 'w-6 bg-brand' : 'w-1.5 bg-border'}`}
          />
        ))}
      </div>
    </div>
  )
}

function Status({ phase, done }: { phase: Phase; done: string }) {
  const base = 'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium'

  if (phase === 'done') {
    return (
      <span
        className={`${base} sq-demo-in border-[color-mix(in_oklch,var(--brand)_35%,transparent)] bg-brand-soft text-brand-strong`}
      >
        <Zap className="size-3.5" /> {done}
      </span>
    )
  }

  if (phase === 'approved') {
    return (
      <span className={`${base} sq-demo-in border-border text-foreground`}>
        <Check className="size-3.5 text-brand" /> Approved
      </span>
    )
  }

  return (
    <span
      className={`${base} ${
        phase === 'proposing'
          ? 'border-sky-400/30 bg-sky-400/10 text-sky-300'
          : 'border-orange-400/30 bg-orange-400/10 text-orange-300'
      }`}
    >
      <Clock className="size-3.5" /> {phase === 'proposing' ? 'Incoming' : 'Waiting for you'}
    </span>
  )
}
