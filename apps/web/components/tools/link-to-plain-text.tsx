'use client'

import { ArrowRight } from 'lucide-react'
import { useId, useState } from 'react'

import { CopyButton, fieldClass } from '@/components/tools/shared'
import { applyLinks, refang } from '@/lib/links'

type Mode = 'defang' | 'remove' | 'refang'

// [mode, title, what it does, an example before and after]
const MODES: [Mode, string, string, string, string][] = [
  ['defang', 'To plain text', 'Readable, but not a link.', 'example.com/page', 'example[.]com/page'],
  ['remove', 'Remove links', 'Taken out; the spaces close up.', 'see example.com now', 'see now'],
  ['refang', 'Back to links', 'Plain text becomes links again.', 'hxxps://example[.]com', 'https://example.com'],
]

const SAMPLE = 'Our launch notes are at https://www.example.com/blog/v1.4 and the docs at docs.example.com.'

export function LinkToPlainText() {
  const id = useId()
  const [text, setText] = useState(SAMPLE)
  const [mode, setMode] = useState<Mode>('defang')
  const result = mode === 'refang' ? { text: refang(text), found: [] } : applyLinks(mode, text)

  return (
    <div className="grid gap-6">
      <fieldset className="grid gap-2">
        <legend className="mb-2 text-sm font-medium">What to do with the links</legend>
        <div className="grid gap-2 sm:grid-cols-3">
          {MODES.map(([value, label, help, before, after]) => (
            <label
              key={value}
              htmlFor={`${id}-${value}`}
              className="group relative grid cursor-pointer gap-3 rounded-xl border border-border bg-card p-4 transition-colors hover:border-foreground/20 has-checked:border-brand has-checked:bg-brand-soft/30 has-focus-visible:ring-2 has-focus-visible:ring-brand"
            >
              <input
                id={`${id}-${value}`}
                type="radio"
                name={`${id}-mode`}
                value={value}
                checked={mode === value}
                onChange={() => setMode(value)}
                className="peer sr-only"
              />
              {/* Our own radio mark, top right; the native one stays for keyboards and screen readers. */}
              <span
                aria-hidden
                className="absolute top-4 right-4 flex size-4 items-center justify-center rounded-full border border-border peer-checked:border-brand peer-checked:bg-brand"
              >
                <span className="size-1.5 rounded-full bg-background" />
              </span>
              <span className="grid gap-0.5 pr-6">
                <span className="text-sm font-semibold tracking-tight">{label}</span>
                <span className="text-sm text-muted-foreground">{help}</span>
              </span>
              <span className="flex flex-wrap items-center gap-1.5 font-mono text-xs">
                <code className="rounded-md bg-background px-1.5 py-0.5 text-muted-foreground">{before}</code>
                <ArrowRight aria-hidden className="size-3 text-muted-foreground" />
                <code className="rounded-md bg-background px-1.5 py-0.5 text-foreground">{after}</code>
              </span>
            </label>
          ))}
        </div>
      </fieldset>

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="grid content-start gap-1.5">
          <label htmlFor={`${id}-in`} className="text-sm font-medium">
            Your text
          </label>
          <textarea
            id={`${id}-in`}
            className={`${fieldClass} min-h-48 resize-y leading-relaxed`}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        </div>
        <div className="grid content-start gap-1.5">
          <label htmlFor={`${id}-out`} className="text-sm font-medium">
            Result
          </label>
          <div className="relative">
            <textarea
              id={`${id}-out`}
              readOnly
              className={`${fieldClass} min-h-48 resize-y bg-card pr-12 leading-relaxed`}
              value={result.text}
            />
            <CopyButton text={result.text} label="Copy result" className="absolute top-2 right-2" />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground" aria-live="polite">
              {mode === 'refang'
                ? 'Links restored.'
                : `${result.found.length} ${result.found.length === 1 ? 'link' : 'links'} changed.`}
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
