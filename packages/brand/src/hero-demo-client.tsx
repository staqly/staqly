'use client'

import { CalendarClock, Check, Loader2, PenLine, Repeat, Terminal, Zap } from 'lucide-react'
import { useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'

import { scenarios } from './hero-scenarios'

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
export function HeroDemoClient({ basePath = '', tiles }: { basePath?: string; tiles: Record<string, ReactNode> }) {
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
      {/* The agent's side. Both cards are opaque on phones: two stacked backdrop blurs cost a phone far more than the
          10% see-through shows. */}
      <div className="relative z-0 rounded-2xl border border-border bg-card shadow-[0_30px_80px_-30px_rgb(0_0_0/0.5)] sm:bg-card/90 sm:backdrop-blur">
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
          className="min-h-[10.5rem] space-y-1.5 overflow-hidden px-4 pt-3 pb-12 font-mono text-[12px] leading-relaxed whitespace-nowrap sm:text-[12.5px]"
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

      {/* The calendar's side: the post card. Every scenario's card sits in the same grid cell (only the shown one
          visible), so the demo is always as tall as its tallest card and nothing around it moves. */}
      <div className="relative z-10 -mt-9 ml-3 grid grid-cols-1 sm:ml-10">
        {scenarios.map((x, i) => (
          <PostCard
            key={i === shownIndex ? `shown-${i}` : i}
            s={x}
            phase={i === shownIndex ? shownPhase : 'writing'}
            shown={i === shownIndex}
            tiles={tiles}
            basePath={basePath}
          />
        ))}
      </div>

      <div className="mt-5 flex items-center justify-center gap-2">
        {scenarios.map((x, i) => (
          <span
            key={x.collection}
            className={`h-1.5 rounded-full transition-colors ${i === shownIndex ? 'w-6 bg-brand' : 'w-1.5 bg-border'}`}
          />
        ))}
      </div>
    </div>
  )
}

function PostCard({
  s,
  phase,
  shown,
  tiles,
  basePath,
}: {
  s: (typeof scenarios)[number]
  phase: Phase
  shown: boolean
  tiles: Record<string, ReactNode>
  basePath: string
}) {
  const published = phase === 'published'

  return (
    <div
      className={`relative col-start-1 row-start-1 rounded-2xl border border-border bg-background p-4 shadow-[0_40px_90px_-30px_rgb(0_0_0/0.6)] sm:bg-background/95 sm:p-5 sm:backdrop-blur ${shown ? 'sq-demo-card' : 'invisible'}`}
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
            key={t.id + t.channel}
            className="flex items-center gap-2 rounded-lg border border-border bg-card/60 px-2.5 py-1.5 text-xs"
          >
            {tiles[t.id]}
            <span className="shrink-0 font-medium whitespace-nowrap">{t.channel}</span>
            {t.via && <span className="shrink-0 whitespace-nowrap text-muted-foreground">via {t.via}</span>}
            <span className="ml-auto min-w-0 truncate font-mono text-[11px] text-muted-foreground">
              {published ? t.link : t.chars}
            </span>
            <TargetMark phase={phase} />
          </li>
        ))}
      </ul>

      <div className="mt-4">
        <Status phase={phase} slot={s.slot} />
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
