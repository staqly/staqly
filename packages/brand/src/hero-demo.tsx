'use client'

import { CalendarClock, Check, Loader2, PenLine, Repeat, Terminal, Zap } from 'lucide-react'
import { useEffect, useRef, useState, useSyncExternalStore } from 'react'

import { ProviderTile } from './providers'

type Target = { id: string; channel: string; chars: string; link: string }

type Scenario = {
  agent: string
  prompt: string
  collection: string
  schedule: string
  slot: string
  content: string
  targets: Target[]
  image?: string // a file under the app's public/ (the app serves /demo/golden-hour.svg)
}

// Use cases of the landing page (apps/web/lib/use-cases.ts), as an agent would send them.
const scenarios: Scenario[] = [
  {
    agent: 'claude',
    prompt: 'Write captions for this week’s reels into Reels',
    collection: 'Reels',
    schedule: 'Every day at 18:00',
    slot: 'Today 18:00',
    content: '3 things I wish I knew before my first marathon 🏃 Save this for race week. #running #marathontraining',
    targets: [
      { id: 'tiktok', channel: 'TikTok', chars: '96 / 2200', link: 'tiktok.com/@.../video/73...' },
      { id: 'instagram', channel: 'Instagram', chars: '96 / 2200', link: 'instagram.com/reel/C9...' },
      { id: 'youtube', channel: 'YouTube', chars: '96 / 5000', link: 'youtube.com/shorts/...' },
    ],
  },
  {
    agent: 'n8n',
    prompt: 'Generate today’s Instagram post',
    collection: 'Instagram Avatar',
    schedule: 'Every day at 18:30',
    slot: 'Today 18:30',
    content: 'Chasing the last light of the day ☀️ #goldenhour #citylife',
    targets: [
      { id: 'instagram', channel: 'Instagram', chars: '58 / 2200', link: 'instagram.com/p/C9x...' },
      { id: 'threads', channel: 'Threads', chars: '58 / 500', link: 'threads.net/@.../post/...' },
    ],
    image: '/demo/golden-hour.svg',
  },
  {
    agent: 'codex',
    prompt: 'Announce v1.4, keep the X version short',
    collection: 'Product updates',
    schedule: 'Once',
    slot: 'Thu 10:00',
    content: 'v1.4 is out: schedules per collection, a calendar you can drag, and alerts when a post fails.',
    targets: [
      { id: 'linkedin', channel: 'LinkedIn', chars: '96 / 3000', link: 'linkedin.com/feed/...' },
      { id: 'x', channel: 'X', chars: '41 / 280', link: 'x.com/staqly/...' },
    ],
  },
]

type Phase = 'writing' | 'scheduled' | 'publishing' | 'published'

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
 * Hero demo of the loop: an agent writes a post into a collection, the collection's schedule gives it a slot, and
 * at that time staqly publishes it to every channel and records the links. Display only, nothing is clickable. The
 * loop runs only while the demo is on screen; with reduced motion it shows the finished first scenario and stays
 * still.
 */
export function HeroDemo({ basePath = '' }: { basePath?: string }) {
  const root = useRef<HTMLDivElement>(null)
  const [onScreen, setOnScreen] = useState(false)
  const still = useSyncExternalStore(subscribeReducedMotion, reducedMotion, noReducedMotion)
  const [index, setIndex] = useState(0)
  const [phase, setPhase] = useState<Phase>('writing')
  const shownIndex = still ? 0 : index
  const shownPhase: Phase = still ? 'published' : phase
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
      writing: [STEP_MS * 1.4, () => setPhase('scheduled')],
      scheduled: [STEP_MS * 2.6, () => setPhase('publishing')],
      publishing: [STEP_MS * 1.4, () => setPhase('published')],
      published: [
        STEP_MS * 3,
        () => {
          setIndex((index + 1) % scenarios.length)
          setPhase('writing')
        },
      ],
    }
    const [ms, step] = next[phase]
    const t = setTimeout(step, ms)

    return () => clearTimeout(t)
  }, [phase, index, onScreen, still])

  const published = shownPhase === 'published'

  return (
    <div ref={root} className="relative min-w-0 select-none" aria-hidden>
      {/* The agent's side */}
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
            ⏺ staqly · <span className="text-foreground">create_post</span> → {s.collection}
          </p>
          {shownPhase !== 'writing' && (
            <p className="sq-demo-in truncate text-muted-foreground">
              ⏺ staqly · <span className="text-foreground">schedule</span> →{' '}
              <span className="text-brand-strong">{s.slot}</span>
            </p>
          )}
          {shownPhase !== 'writing' && shownPhase !== 'scheduled' && (
            <p className="sq-demo-in truncate text-muted-foreground">
              ⏺ staqly · <span className="text-foreground">publish</span>{' '}
              {published ? (
                <span className="text-brand-strong">
                  → {s.targets.length} {s.targets.length === 1 ? 'link' : 'links'}
                </span>
              ) : (
                <span className="sq-dots">sending</span>
              )}
            </p>
          )}
        </div>
      </div>

      {/* The calendar's side: the post card */}
      <div className="relative z-10 -mt-9 ml-3 sm:ml-10">
        <div
          key={shownIndex}
          className="sq-demo-card relative rounded-2xl border border-border bg-background/95 p-4 shadow-[0_40px_90px_-30px_rgb(0_0_0/0.6)] backdrop-blur sm:p-5"
        >
          <div className="flex items-center gap-2 text-xs">
            <span className="rounded-full border border-[color-mix(in_oklch,var(--brand)_35%,transparent)] bg-brand-soft px-2.5 py-0.5 font-medium text-brand-strong">
              {s.collection}
            </span>
            <span className="flex min-w-0 items-center gap-1 truncate text-muted-foreground">
              <Repeat className="size-3 shrink-0" /> {s.schedule}
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
            <p className="min-w-0 flex-1 text-sm leading-relaxed">{s.content}</p>
          </div>

          <ul className="mt-4 space-y-1.5">
            {s.targets.map((t) => (
              <li
                key={t.channel}
                className="flex items-center gap-2 rounded-lg border border-border bg-card/60 px-2.5 py-1.5 text-xs"
              >
                <ProviderTile id={t.id} name={t.channel} className="size-5 rounded-md" />
                <span className="font-medium">{t.channel}</span>
                <span className="ml-auto truncate font-mono text-[11px] text-muted-foreground">
                  {published ? t.link : t.chars}
                </span>
                <TargetMark phase={shownPhase} />
              </li>
            ))}
          </ul>

          <div className="mt-4">
            <Status phase={shownPhase} slot={s.slot} />
          </div>
        </div>
      </div>

      <div className="mt-5 flex items-center justify-center gap-2">
        {scenarios.map((x, i) => (
          <span
            key={x.collection}
            className={`h-1.5 rounded-full transition-all ${i === shownIndex ? 'w-6 bg-brand' : 'w-1.5 bg-border'}`}
          />
        ))}
      </div>
    </div>
  )
}

function TargetMark({ phase }: { phase: Phase }) {
  if (phase === 'published') {
    return <Check className="sq-demo-in size-3.5 shrink-0 text-brand" />
  }

  if (phase === 'publishing') {
    return <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
  }

  return <span className="size-3.5 shrink-0" />
}

function Status({ phase, slot }: { phase: Phase; slot: string }) {
  const base = 'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium'

  if (phase === 'published') {
    return (
      <span
        className={`${base} sq-demo-in border-[color-mix(in_oklch,var(--brand)_35%,transparent)] bg-brand-soft text-brand-strong`}
      >
        <Zap className="size-3.5" /> Published
      </span>
    )
  }

  if (phase === 'writing') {
    return (
      <span className={`${base} border-border text-muted-foreground`}>
        <PenLine className="size-3.5" /> Draft
      </span>
    )
  }

  return (
    <span className={`${base} sq-demo-in border-sky-400/30 bg-sky-400/10 text-sky-300`}>
      <CalendarClock className="size-3.5" /> {phase === 'publishing' ? 'Publishing' : `Scheduled · ${slot}`}
    </span>
  )
}
