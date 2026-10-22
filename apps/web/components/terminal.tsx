'use client'

import { Terminal } from 'lucide-react'
import { useEffect, useRef, useState, type CSSProperties } from 'react'

const TYPE_MS = 14 // per character of a command
const MAX_TYPE_MS = 550 // long commands type faster
const GAP_MS = 160 // pause before each line

type Line = { text: string; typed: boolean; delay: number; duration: number }

/** Commands (`$ ...` and their `\` continuations) are typed; everything else appears line by line. */
function schedule(code: string): { lines: Line[]; total: number } {
  let t = 0
  let continuing = false
  const lines = code.split('\n').map((text): Line => {
    const typed = text.startsWith('$ ') || continuing
    const chars = typed ? text.length - (text.startsWith('$ ') ? 2 : 0) : 0
    const duration = typed ? Math.min(chars * TYPE_MS, MAX_TYPE_MS) : 0
    const delay = t + (text ? GAP_MS : 80)

    t = delay + duration
    continuing = typed && text.endsWith('\\')

    return { text, typed, delay, duration }
  })

  return { lines, total: t + GAP_MS }
}

/**
 * Terminal-style code block that plays line by line once scrolled into view.
 * Lines starting with `#` are comments, `$ ` gets a green prompt, `→` is output.
 * The full text is always in the markup; the animation is CSS (globals.css, .sq-term).
 */
export function TerminalCard({ title = 'Terminal', code }: { title?: string; code: string }) {
  const ref = useRef<HTMLPreElement>(null)
  const [run, setRun] = useState(false)
  const { lines, total } = schedule(code)

  useEffect(() => {
    const el = ref.current

    if (!el) {
      return
    }

    const io = new IntersectionObserver(
      ([e]) => {
        if (e.isIntersecting) {
          setRun(true)
          io.disconnect()
        }
      },
      { threshold: 0.35 },
    )

    io.observe(el)

    return () => io.disconnect()
  }, [])

  return (
    <div className="min-w-0 overflow-hidden rounded-2xl border border-border bg-background shadow-sm">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2.5 text-xs text-muted-foreground">
        <Terminal className="size-3.5" /> {title}
      </div>
      {/* Focusable so keyboard users can scroll a long line sideways (WCAG 2.1.1, axe scrollable-region-focusable). */}
      <pre
        ref={ref}
        data-run={run || undefined}
        tabIndex={0}
        className="sq-term overflow-x-auto p-4 font-mono text-[12px] leading-6 sm:p-5 sm:text-[13px] sm:leading-7"
      >
        <code>
          {lines.map((l, i) => (
            <span
              key={i}
              className="sq-term-line block min-h-6 sm:min-h-7"
              style={{ '--d': `${l.delay}ms` } as CSSProperties}
            >
              <LineText line={l} />
            </span>
          ))}
          <span className="sq-term-line block" style={{ '--d': `${total}ms` } as CSSProperties} aria-hidden>
            <span className="text-brand">$ </span>
            <span className="sq-caret" />
          </span>
        </code>
      </pre>
    </div>
  )
}

function LineText({ line }: { line: Line }) {
  const { text, typed, delay, duration } = line

  if (typed) {
    const prompt = text.startsWith('$ ')
    const body = prompt ? text.slice(2) : text
    const style = { '--d': `${delay}ms`, '--t': `${duration}ms`, '--n': body.length } as CSSProperties

    return (
      <>
        {prompt && <span className="text-brand">$ </span>}
        <span className="sq-type" style={style}>
          {body}
        </span>
      </>
    )
  }

  if (text.startsWith('#')) {
    return <span className="text-muted-foreground">{text}</span>
  }

  if (text.startsWith('→')) {
    return <span className="text-brand-strong">{text}</span>
  }

  return text
}
