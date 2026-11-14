'use client'

import { ProviderIcon } from '@staqly/brand/providers'
import { useId, useState } from 'react'

import { fieldClass, Select } from '@/components/tools/shared'
import { applyLinks, type LinksMode } from '@/lib/links'
import { LIMITS } from '@/lib/tools'

const SAMPLE =
  'Three things I wish I knew before my first marathon. Save this for race week! Full plan: https://example.com/marathon-plan #running #marathontraining'

// One per character (code point), the way Staqly counts before publishing; an emoji counts once.
const count = (s: string) => [...s].length

export function CharacterCounter() {
  const id = useId()
  const [text, setText] = useState(SAMPLE)
  const [xLinks, setXLinks] = useState<LinksMode>('remove')
  const x = applyLinks(xLinks, text)
  const hashtags = text.match(/(^|\s)#[\p{L}\p{N}_]+/gu)?.length ?? 0

  const words = text.trim() ? text.trim().split(/\s+/).length : 0
  const overCount = LIMITS.filter((l) => (l.id === 'x' ? count(x.text) : count(text)) > l.max).length

  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.25fr)]">
      {/* The post stays in view while the platforms scroll by. */}
      <div className="grid gap-4 rounded-2xl border border-border bg-card p-5 sm:p-6 lg:sticky lg:top-24">
        <label htmlFor={`${id}-text`} className="text-sm font-medium">
          Your post
        </label>
        <textarea
          id={`${id}-text`}
          className={`${fieldClass} min-h-48 resize-y leading-relaxed`}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Paste or write a caption"
        />
        <dl className="grid grid-cols-3 gap-2 text-center" aria-live="polite">
          {(
            [
              ['Characters', count(text)],
              ['Words', words],
              ['Hashtags', hashtags],
            ] as const
          ).map(([label, n]) => (
            <div
              key={label}
              className="flex flex-col-reverse rounded-xl border border-border bg-background px-2 py-2.5"
            >
              {/* dt first for the markup, shown under the number. */}
              <dt className="text-xs text-muted-foreground">{label}</dt>
              <dd className="font-mono text-lg font-semibold tabular-nums">{n.toLocaleString('en-US')}</dd>
            </div>
          ))}
        </dl>
        {hashtags > 30 && <p className="text-sm text-red-400">Instagram takes at most 30 hashtags.</p>}
        <label className="grid gap-1.5 text-sm font-medium">
          Links on X
          <Select value={xLinks} onChange={(e) => setXLinks(e.target.value as LinksMode)}>
            <option value="remove">Remove links</option>
            <option value="defang">Show links as plain text</option>
          </Select>
          <span className="text-xs font-normal leading-relaxed text-muted-foreground">
            The X API charges more for posts with links and X shows them to fewer people, so Staqly never sends links
            there as links.
          </span>
        </label>
      </div>

      <section aria-label="Fit per platform" className="grid gap-3">
        <p className="text-sm text-muted-foreground" aria-live="polite">
          {overCount === 0 ? (
            <>Fits every platform below.</>
          ) : (
            <span className="text-red-400">
              Too long for {overCount} {overCount === 1 ? 'platform' : 'platforms'}.
            </span>
          )}
        </p>
        <ul className="grid gap-2 sm:grid-cols-2">
          {LIMITS.map((l) => {
            const n = l.id === 'x' ? count(x.text) : count(text)
            const over = n > l.max
            const pct = Math.min(100, (n / l.max) * 100)

            return (
              <li
                key={l.id}
                className={`flex flex-col rounded-xl border bg-card px-3.5 py-3 ${over ? 'border-red-400/50' : 'border-border'}`}
              >
                <div className="flex items-center gap-2 text-sm">
                  <ProviderIcon id={l.id} name={l.name} className="size-4" />
                  <span className="min-w-0 truncate font-medium">{l.name}</span>
                  <span
                    className={`ml-auto shrink-0 font-mono text-xs tabular-nums ${over ? 'text-red-400' : 'text-muted-foreground'}`}
                  >
                    {n.toLocaleString('en-US')}/{l.max.toLocaleString('en-US')}
                  </span>
                </div>
                {/* The numbers above say it in words; the bar is for the eye. */}
                <div aria-hidden className="mt-2 h-1 overflow-hidden rounded-full bg-muted">
                  <div
                    className={`h-full rounded-full ${over ? 'bg-red-400' : pct > 90 ? 'bg-amber-400' : 'bg-brand'}`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
                {over && (
                  <p className="mt-1.5 text-xs text-red-400">
                    {(n - l.max).toLocaleString('en-US')} over: shorten it, or give {l.name} its own version.
                  </p>
                )}
                {l.id === 'x' && x.found.length > 0 && (
                  <p className="mt-1.5 truncate text-xs text-muted-foreground" title={x.found.join(', ')}>
                    {xLinks === 'remove' ? 'Removed' : 'Shown as plain text'}: {x.found.join(', ')}
                  </p>
                )}
                {l.note && <p className="mt-1.5 text-xs leading-snug text-muted-foreground">{l.note}</p>}
              </li>
            )
          })}
        </ul>
      </section>
    </div>
  )
}
