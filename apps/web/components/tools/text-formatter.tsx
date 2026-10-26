'use client'

import { List, ListOrdered, RemoveFormatting } from 'lucide-react'
import { useId, useLayoutEffect, useRef, useState } from 'react'

import { CopyButton, fieldClass } from '@/components/tools/shared'
import {
  STYLES,
  applyStyle,
  foldPreview,
  toPlain,
  toggleList,
  toggleRange,
  visibleLength,
  type Style,
} from '@/lib/text-format'

const SAMPLE = `Three things I learned shipping our first product:

Talk to customers before you write code.
Ship small, ship often.
Say no to most feature requests.

Which one do you disagree with?`

// LinkedIn's limit for a post, and about where its feed folds a post behind "...see more".
const LIMIT = 3000
const FOLDS = { computer: { chars: 210, lines: 3 }, phone: { chars: 140, lines: 3 } } as const

const tool =
  'inline-flex h-9 items-center justify-center gap-1.5 rounded-lg border border-border bg-background px-2.5 text-sm transition-colors hover:border-foreground/20 hover:bg-accent focus-visible:ring-2 focus-visible:ring-brand focus-visible:outline-none pointer-coarse:h-11'

export function TextFormatter() {
  const id = useId()
  const area = useRef<HTMLTextAreaElement>(null)
  const [text, setText] = useState(SAMPLE)
  const [device, setDevice] = useState<keyof typeof FOLDS>('computer')
  // The selection to restore once the changed text is on screen.
  const [select, setSelect] = useState<[number, number] | null>(null)

  useLayoutEffect(() => {
    if (select && area.current) {
      area.current.focus()
      area.current.setSelectionRange(select[0], select[1])
      setSelect(null)
    }
  }, [select])

  function style(s: Style) {
    const el = area.current
    const r = toggleRange(text, el?.selectionStart ?? 0, el?.selectionEnd ?? 0, s)

    setText(r.text)
    setSelect([r.start, r.end])
  }

  function list(kind: 'bullet' | 'number') {
    setText(toggleList(text, kind))
  }

  const n = visibleLength(text)
  const fold = foldPreview(text, FOLDS[device].chars, FOLDS[device].lines)

  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
      <div className="grid gap-4 rounded-2xl border border-border bg-card p-5 sm:p-6">
        <div role="toolbar" aria-label="Text styles" aria-controls={`${id}-text`} className="flex flex-wrap gap-1.5">
          {STYLES.map((s) => (
            <button
              key={s.id}
              type="button"
              className={tool}
              title={`${s.label}: select words first, or it styles everything`}
              aria-label={s.label}
              // Keep the selection in the text box: the button must not take focus on press.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => style(s.id)}
            >
              <span aria-hidden>{applyStyle(s.sample, s.id)}</span>
            </button>
          ))}
          <button
            type="button"
            className={tool}
            aria-label="Bulleted list"
            title="Bulleted list"
            onClick={() => list('bullet')}
          >
            <List className="size-4" aria-hidden />
          </button>
          <button
            type="button"
            className={tool}
            aria-label="Numbered list"
            title="Numbered list"
            onClick={() => list('number')}
          >
            <ListOrdered className="size-4" aria-hidden />
          </button>
          <button
            type="button"
            className={tool}
            aria-label="Clear formatting"
            title="Clear formatting: plain letters again"
            onClick={() => setText(toPlain(text))}
          >
            <RemoveFormatting className="size-4" aria-hidden />
          </button>
        </div>

        <div className="grid gap-1.5">
          <label htmlFor={`${id}-text`} className="text-sm font-medium">
            Your post
          </label>
          <div className="relative">
            <textarea
              ref={area}
              id={`${id}-text`}
              className={`${fieldClass} min-h-64 resize-y pr-12 leading-relaxed`}
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Write or paste your post, select words, then pick a style"
            />
            <CopyButton text={text} label="Copy the post" className="absolute top-2 right-2" />
          </div>
          <p className="text-xs text-muted-foreground" aria-live="polite">
            <span className={n > LIMIT ? 'text-red-400' : ''}>
              {n.toLocaleString('en-US')}/{LIMIT.toLocaleString('en-US')} characters
            </span>
            {n > LIMIT ? ': too long for LinkedIn.' : '.'} Select words and pick a style; press it again to take it off.
          </p>
        </div>
      </div>

      <section aria-labelledby={`${id}-preview`} className="grid gap-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id={`${id}-preview`} className="text-lg font-semibold tracking-tight">
            In the feed
          </h2>
          <fieldset className="inline-flex rounded-xl border border-border bg-background p-1">
            <legend className="sr-only">Preview on</legend>
            {(
              [
                ['computer', 'Computer'],
                ['phone', 'Phone'],
              ] as const
            ).map(([value, label]) => (
              <label
                key={value}
                className="cursor-pointer rounded-lg px-3 py-1.5 text-sm text-muted-foreground transition-colors has-checked:bg-card has-checked:text-foreground has-focus-visible:ring-2 has-focus-visible:ring-brand pointer-coarse:py-2.5"
              >
                <input
                  type="radio"
                  name={`${id}-device`}
                  value={value}
                  checked={device === value}
                  onChange={() => setDevice(value)}
                  className="sr-only"
                />
                {label}
              </label>
            ))}
          </fieldset>
        </div>
        <article
          className={`rounded-xl border border-border bg-white p-4 text-[#191919] ${device === 'phone' ? 'mx-auto w-full max-w-[360px]' : ''}`}
          aria-label="Preview of the post"
        >
          <header className="flex items-center gap-2.5">
            <span aria-hidden className="size-10 rounded-full bg-[#0b3d2a]" />
            <span className="grid">
              <span className="text-sm font-semibold">Jane Doe</span>
              <span className="text-xs text-[#666]">Founder at Example Co · 1h</span>
            </span>
          </header>
          <p className="mt-3 text-sm leading-normal break-words whitespace-pre-wrap">
            {fold.shown}
            {fold.cut && <span className="text-[#666]"> ...see more</span>}
          </p>
        </article>
        <p className="text-xs leading-relaxed text-muted-foreground">
          {fold.cut
            ? `The feed shows about this much before "...see more": make the opening count.`
            : 'The whole post shows without "...see more".'}{' '}
          Where it folds depends on the screen; this is about where.
        </p>
      </section>
    </div>
  )
}
