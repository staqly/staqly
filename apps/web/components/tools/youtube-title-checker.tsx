'use client'

import { AlertTriangle, CheckCircle2, ImageUp, XCircle } from 'lucide-react'
import { useEffect, useId, useState, type ReactNode } from 'react'

import { fieldClass } from '@/components/tools/shared'
import { MAX_TITLE, SAFE_CUT, SEARCH_CUT, checkTitle, cutTitle, titleLength, type Level } from '@/lib/youtube-title'

const SAMPLE = 'I baked bread every day for 30 days: what actually made it better'

const ICON: Record<Level, ReactNode> = {
  ok: <CheckCircle2 className="size-4 text-brand" aria-hidden />,
  warn: <AlertTriangle className="size-4 text-amber-300" aria-hidden />,
  error: <XCircle className="size-4 text-red-400" aria-hidden />,
}

// YouTube's own type: Roboto where the visitor has it, then the usual fallbacks.
const yt = { fontFamily: 'Roboto, Arial, sans-serif' }

function Thumb({ url, className }: { url: string; className: string }) {
  return (
    <span className={`relative block shrink-0 overflow-hidden rounded-xl bg-[#0b3d2a] ${className}`}>
      {url ? (
        // oxlint-disable-next-line nextjs/no-img-element
        <img src={url} alt="" className="size-full object-cover" />
      ) : (
        <span aria-hidden className="block size-full bg-linear-to-br from-[#1ed27b]/60 to-[#0b3d2a]" />
      )}
      <span className="absolute right-1.5 bottom-1.5 rounded bg-black/80 px-1 text-[11px] font-medium text-white">
        12:34
      </span>
    </span>
  )
}

/**
 * The title in the size, weight and width YouTube gives it in each place, cut after two lines by the browser itself
 * (line-clamp), so the cut falls where the visitor's fonts put it. Each preview keeps its real width and scrolls
 * sideways on a narrow screen rather than squeezing.
 */
function Previews({ title, channel, thumb }: { title: string; channel: string; thumb: string }) {
  const meta = <span className="text-[#606060]">{channel || 'Your channel'} · 1.2K views · 2 days ago</span>

  return (
    <div className="grid gap-4">
      <Place name="Home, on a computer">
        <div className="w-[360px] text-[#0f0f0f]" style={yt}>
          <Thumb url={thumb} className="aspect-video w-full" />
          <div className="mt-3 flex gap-3">
            <span aria-hidden className="size-9 shrink-0 rounded-full bg-[#0b3d2a]" />
            <div className="min-w-0">
              <p className="line-clamp-2 text-base leading-[22px] font-medium">{title}</p>
              <p className="mt-1 text-sm">{meta}</p>
            </div>
          </div>
        </div>
      </Place>
      <Place name="Search, on a computer">
        <div className="flex w-[860px] gap-4 text-[#0f0f0f]" style={yt}>
          <Thumb url={thumb} className="aspect-video w-[360px]" />
          <div className="min-w-0 flex-1">
            <p className="line-clamp-2 text-lg leading-[26px]">{title}</p>
            <p className="mt-1 text-xs">{meta}</p>
          </div>
        </div>
      </Place>
      <div className="grid gap-4 sm:grid-cols-2">
        <Place name="Up next, beside a video">
          <div className="flex w-[402px] gap-2 text-[#0f0f0f]" style={yt}>
            <Thumb url={thumb} className="aspect-video w-[168px] rounded-lg" />
            <div className="min-w-0 flex-1">
              <p className="line-clamp-2 text-sm leading-5 font-medium">{title}</p>
              <p className="mt-1 text-xs">{meta}</p>
            </div>
          </div>
        </Place>
        <Place name="Home, on a phone">
          <div className="w-[360px] text-[#0f0f0f]" style={yt}>
            <Thumb url={thumb} className="aspect-video w-full rounded-none" />
            <div className="flex gap-3 px-3 pt-3">
              <span aria-hidden className="size-9 shrink-0 rounded-full bg-[#0b3d2a]" />
              <div className="min-w-0">
                <p className="line-clamp-2 text-sm leading-5">{title}</p>
                <p className="mt-1 text-xs">{meta}</p>
              </div>
            </div>
          </div>
        </Place>
      </div>
    </div>
  )
}

function Place({ name, children }: { name: string; children: ReactNode }) {
  return (
    <figure className="grid min-w-0 gap-2">
      <figcaption className="text-xs font-medium text-muted-foreground">{name}</figcaption>
      {/* oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex */}
      <div
        tabIndex={0}
        className="overflow-x-auto rounded-xl border border-border bg-white p-3 focus-visible:ring-2 focus-visible:ring-brand focus-visible:outline-none"
      >
        {children}
      </div>
    </figure>
  )
}

export function YoutubeTitleChecker() {
  const id = useId()
  const [title, setTitle] = useState(SAMPLE)
  const [channel, setChannel] = useState('')
  const [thumb, setThumb] = useState('')
  const n = titleLength(title)
  const checks = checkTitle(title)

  // The thumbnail's object URL lives as long as it is shown.
  useEffect(
    () => () => {
      if (thumb) {
        URL.revokeObjectURL(thumb)
      }
    },
    [thumb],
  )

  return (
    <div className="grid gap-8">
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <div className="grid gap-4 rounded-2xl border border-border bg-card p-5 sm:p-6">
          <div className="grid gap-1.5">
            <label htmlFor={`${id}-title`} className="text-sm font-medium">
              Video title
            </label>
            <textarea
              id={`${id}-title`}
              rows={2}
              className={`${fieldClass} resize-y text-base leading-relaxed`}
              value={title}
              onChange={(e) => setTitle(e.target.value.replace(/\n/g, ' '))}
              placeholder="Write the title you plan to use"
            />
            <div aria-hidden className="relative mt-1 h-1.5 overflow-hidden rounded-full bg-muted">
              <div
                className={`h-full rounded-full ${n > MAX_TITLE ? 'bg-red-400' : n > SEARCH_CUT ? 'bg-amber-400' : 'bg-brand'}`}
                style={{ width: `${Math.min(100, n)}%` }}
              />
              <span className="absolute inset-y-0 w-px bg-foreground/50" style={{ left: `${SAFE_CUT}%` }} />
              <span className="absolute inset-y-0 w-px bg-foreground/50" style={{ left: `${SEARCH_CUT}%` }} />
            </div>
            <p className="font-mono text-xs text-muted-foreground tabular-nums" aria-live="polite">
              <span className={n > MAX_TITLE ? 'text-red-400' : ''}>
                {n}/{MAX_TITLE}
              </span>
              <span className="font-sans">
                {' '}
                characters · marks at {SAFE_CUT} and {SEARCH_CUT}
              </span>
            </p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="grid gap-1.5 text-sm font-medium">
              Channel name (optional)
              <input
                className={fieldClass}
                value={channel}
                onChange={(e) => setChannel(e.target.value)}
                placeholder="Your channel"
              />
            </label>
            <div className="grid gap-1.5">
              <span className="text-sm font-medium">Thumbnail (optional)</span>
              <label
                htmlFor={`${id}-thumb`}
                className="flex h-10 cursor-pointer items-center justify-center gap-2 rounded-xl border border-dashed border-border bg-background px-3 text-sm text-muted-foreground transition-colors hover:border-foreground/30 has-focus-visible:ring-2 has-focus-visible:ring-brand pointer-coarse:h-11"
              >
                <ImageUp className="size-4 text-brand" aria-hidden />
                {thumb ? 'Change the picture' : 'Pick a picture'}
                <input
                  id={`${id}-thumb`}
                  type="file"
                  accept="image/*"
                  className="sr-only"
                  onChange={(e) => {
                    const file = e.target.files?.[0]

                    if (file?.type.startsWith('image/')) {
                      setThumb(URL.createObjectURL(file))
                    }

                    e.target.value = ''
                  }}
                />
              </label>
            </div>
          </div>
        </div>

        <section aria-labelledby={`${id}-checks`} className="grid gap-3">
          <h2 id={`${id}-checks`} className="text-lg font-semibold tracking-tight">
            Checks
          </h2>
          <ul className="grid gap-2" aria-live="polite">
            {checks.map((c) => (
              <li key={c.title} className="flex gap-3 rounded-xl border border-border bg-card px-3.5 py-3">
                <span className="mt-0.5">{ICON[c.level]}</span>
                <span className="grid gap-0.5">
                  <span className="text-sm font-medium">{c.title}</span>
                  <span className="text-xs leading-relaxed text-muted-foreground">{c.detail}</span>
                </span>
              </li>
            ))}
          </ul>
          {n > SAFE_CUT && (
            <div className="rounded-xl border border-border bg-background p-3.5">
              <p className="text-xs font-medium text-muted-foreground">What every placement shows, phones included</p>
              <p className="mt-1 text-sm">{cutTitle(title, SAFE_CUT)}</p>
            </div>
          )}
        </section>
      </div>

      <section aria-labelledby={`${id}-previews`} className="grid gap-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id={`${id}-previews`} className="text-lg font-semibold tracking-tight">
            Where it shows
          </h2>
          <p className="text-xs text-muted-foreground">Real widths; the cut falls where your fonts put it.</p>
        </div>
        <Previews title={title || 'Your title'} channel={channel} thumb={thumb} />
      </section>
    </div>
  )
}
