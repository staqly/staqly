'use client'

import { Play } from 'lucide-react'
import { useState } from 'react'

import { mediaThumbUrl, mediaUrl, type Channel, type Media } from '@/lib/api'
import { charCount } from '@/lib/posts'
import { safeImageSrc } from '@/lib/safe'
import { cn } from '@/lib/utils'

import { displayLink, foldText, segments, splitAtLimit } from './preview-text'

/** A comment under the post, as the channel gets it. */
export interface PreviewComment {
  key: string
  text: string
  media: Media[]
}

/** What one template draws: the channel, its text and media as it receives them, and the comments. */
export interface PreviewProps {
  channel: Channel
  providerName: string
  text: string
  media: Media[]
  comments: PreviewComment[]
  /** The platform's character limit (0: none known). */
  maxChars: number
  /** Counts the text the platform's way (X counts links and emoji its own way). */
  count: (text: string) => number
  settings: Record<string, string>
}

/** The platform's look sets its own font stack (the system UI font), apart from the dashboard's Geist. */
export const SYSTEM_FONT = "[font-family:system-ui,-apple-system,'Segoe_UI',Roboto,Helvetica,Arial,sans-serif]"

/** "brand" for a handle written "@brand" or "@brand@mastodon.social" (Instagram, Threads and TikTok show it bare). */
export function bareHandle(channel: Channel) {
  const h = (channel.handle ?? '').replace(/^@/, '').split('@')[0]

  return h || channel.name.toLowerCase().replace(/\s+/g, '')
}

/** "@brand", or "@brand@instance" as given; made from the name when the channel has no handle. */
export function atHandle(channel: Channel) {
  const h = channel.handle ?? ''

  return h ? (h.startsWith('@') ? h : `@${h}`) : `@${bareHandle(channel)}`
}

/** The channel's picture as the platform shows it (no staqly badge); its first letter when it has none. */
export function Avatar({ channel, className }: { channel: Channel; className?: string }) {
  const src = safeImageSrc(channel.avatar_url)

  return (
    <span aria-hidden className={cn('inline-flex size-10 shrink-0 overflow-hidden rounded-full', className)}>
      {src ? (
        <img src={src} alt="" className="size-full object-cover" referrerPolicy="no-referrer" />
      ) : (
        <span className="grid size-full place-items-center bg-muted text-[0.95em] font-semibold text-foreground uppercase">
          {channel.name.match(/[\p{L}\p{N}]/u)?.[0] ?? '?'}
        </span>
      )}
    </span>
  )
}

/** Text with links, hashtags and mentions in the platform's colour. Links are not live: this is a picture of a post. */
export function Rich({
  text,
  accent,
  shortLinks,
}: {
  text: string
  /** Classes for links, hashtags and mentions. */
  accent: string
  /** Show links shortened, as X, Bluesky and Mastodon do. */
  shortLinks?: boolean
}) {
  return (
    <>
      {segments(text).map((s, i) =>
        s.kind === 'text' ? (
          s.text
        ) : (
          // oxlint-disable-next-line react/no-array-index-key -- runs of one text, in order; nothing reorders them
          <span key={i} className={accent}>
            {s.kind === 'link' && shortLinks ? displayLink(s.text) : s.text}
          </span>
        ),
      )}
    </>
  )
}

/**
 * The post's text: past the platform's limit the rest is marked (the platform would refuse it), and a long text
 * folds the way the feed folds it, with the platform's "more" opening the rest.
 */
export function PostText({
  text,
  max = 0,
  count = charCount,
  fold,
  more = '...more',
  accent,
  moreClass,
  shortLinks,
  className,
  lead,
}: {
  text: string
  max?: number
  count?: (t: string) => number
  /** Fold after so many characters and lines. */
  fold?: [number, number]
  more?: string
  accent: string
  moreClass?: string
  shortLinks?: boolean
  className?: string
  /** Before the text on the same line (Instagram's handle). */
  lead?: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [kept, over] = splitAtLimit(text, max, count)
  const folded = fold && !open && !over ? foldText(kept, fold[0], fold[1]) : null

  if (!text.trim() && !lead) {
    return null
  }

  return (
    <div className={cn('wrap-anywhere whitespace-pre-wrap', className)}>
      {lead}
      <Rich text={folded ?? kept} accent={accent} shortLinks={shortLinks} />
      {folded != null && (
        <>
          {more.startsWith('...') ? '' : ' '}
          <button
            type="button"
            onClick={() => setOpen(true)}
            className={cn(
              'cursor-pointer rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:-my-3 pointer-coarse:py-3',
              moreClass,
            )}
          >
            {more}
          </button>
        </>
      )}
      {over && (
        <>
          <mark className="rounded-xs bg-destructive/20 text-inherit">{over}</mark>
          <span className="mt-2 block text-xs font-medium whitespace-normal text-destructive">
            {count(text) - max} over the limit of {max}: the platform refuses this text.
          </span>
        </>
      )}
    </div>
  )
}

/** A video's first frame (the server keeps no poster images) under a play glyph, and its length when known. */
function VideoPoster({ media, className }: { media: Media; className?: string }) {
  const seconds = Math.round((media.duration_ms ?? 0) / 1000)

  return (
    <span className={cn('relative block size-full bg-black', className)}>
      <video
        src={`${mediaUrl(media.id)}#t=0.1`}
        preload="metadata"
        muted
        playsInline
        tabIndex={-1}
        aria-hidden
        className="pointer-events-none size-full object-cover"
      />
      <span className="absolute inset-0 grid place-items-center">
        <span className="grid size-12 place-items-center rounded-full bg-black/60 text-white">
          <Play className="size-6 translate-x-px fill-current" aria-hidden />
        </span>
      </span>
      {seconds > 0 && (
        <span className="absolute right-1.5 bottom-1.5 rounded bg-black/75 px-1 text-xs font-medium text-white tabular-nums">
          {Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, '0')}
        </span>
      )}
    </span>
  )
}

export function isVideo(m: Media) {
  return m.mime_type.startsWith('video/')
}

/** One picture or video filling its box. */
export function MediaItem({ media, className, width = 640 }: { media: Media; className?: string; width?: 640 | 1280 }) {
  if (isVideo(media)) {
    return <VideoPoster media={media} className={className} />
  }

  if (!media.mime_type.startsWith('image/')) {
    return null
  }

  return (
    <img
      src={mediaThumbUrl(media.id, width)}
      alt={media.alt_text}
      className={cn('size-full object-cover', className)}
    />
  )
}

/** A single picture's shape, kept within what the platform shows (it crops taller or wider ones). */
export function ratioOf(m: Media | undefined, min: number, max: number, fallback = 1) {
  if (!m?.width || !m.height) {
    return fallback
  }

  return Math.min(max, Math.max(min, m.width / m.height))
}

/**
 * Pictures as a feed lays them out: one in its own shape, two side by side, three as one tall and two small, four
 * and more in a 2x2 grid (with "+n" on the last when `more` is set, as Facebook shows it).
 */
export function MediaGrid({
  media,
  className,
  min = 0.75,
  max = 1.91,
  more,
}: {
  media: Media[]
  className?: string
  min?: number
  max?: number
  more?: boolean
}) {
  const shown = media.filter((m) => isVideo(m) || m.mime_type.startsWith('image/'))

  if (!shown.length) {
    return null
  }

  if (shown.length === 1) {
    return (
      <div className={cn('overflow-hidden', className)} style={{ aspectRatio: ratioOf(shown[0], min, max, 16 / 9) }}>
        <MediaItem media={shown[0]!} width={1280} />
      </div>
    )
  }

  const four = shown.slice(0, 4)
  const rest = shown.length - four.length

  return (
    <div
      className={cn(
        'grid aspect-video grid-cols-2 gap-0.5 overflow-hidden',
        four.length > 2 && 'grid-rows-2',
        className,
      )}
    >
      {four.map((m, i) => (
        <span key={m.id} className={cn('relative min-h-0', four.length === 3 && i === 0 && 'row-span-2')}>
          <MediaItem media={m} />
          {more && rest > 0 && i === 3 && (
            <span className="absolute inset-0 grid place-items-center bg-black/55 text-2xl font-semibold text-white">
              +{rest}
            </span>
          )}
        </span>
      ))}
    </div>
  )
}

/** A carousel as the first item with a "1/3" counter and dots under it (Instagram, TikTok photos). */
export function Carousel({
  media,
  ratio,
  dotClass = 'bg-(--pv-line)',
  activeDotClass = 'bg-(--pv-dot)',
  dotsInside,
}: {
  media: Media[]
  ratio: number
  dotClass?: string
  activeDotClass?: string
  dotsInside?: boolean
}) {
  const [at, setAt] = useState(0)
  const item = media[Math.min(at, media.length - 1)]

  if (!item) {
    return null
  }

  const dots = media.length > 1 && (
    <span
      className={cn('flex justify-center gap-1', dotsInside ? 'absolute inset-x-0 bottom-3' : 'pt-3')}
      role="group"
      aria-label="Pictures in the carousel"
    >
      {media.map((m, i) => (
        <button
          key={m.id}
          type="button"
          aria-label={`Show ${i + 1} of ${media.length}`}
          aria-pressed={i === at}
          onClick={() => setAt(i)}
          className={cn(
            'size-1.5 rounded-full outline-none focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:after:absolute pointer-coarse:after:-inset-3',
            'relative',
            i === at ? activeDotClass : dotClass,
          )}
        />
      ))}
    </span>
  )

  return (
    <div>
      <div className="relative overflow-hidden" style={{ aspectRatio: ratio }}>
        <MediaItem media={item} width={1280} />
        {media.length > 1 && (
          <span className="absolute top-3 right-3 rounded-full bg-black/70 px-2 py-0.5 text-xs font-medium text-white tabular-nums">
            {at + 1}/{media.length}
          </span>
        )}
        {dotsInside && dots}
      </div>
      {!dotsInside && dots}
    </div>
  )
}

/** Several pictures side by side in a strip that scrolls (Threads, LinkedIn's document-like carousels). */
export function MediaStripView({ media, className }: { media: Media[]; className?: string }) {
  if (media.length < 2) {
    return <MediaGrid media={media} className={cn('rounded-lg', className)} />
  }

  return (
    <div
      // A scrolling region keyboards can reach (axe's scrollable-region-focusable).
      tabIndex={0}
      aria-label="Pictures, scroll sideways"
      role="group"
      className={cn(
        'flex snap-x gap-1.5 overflow-x-auto pb-1 outline-none focus-visible:ring-2 focus-visible:ring-ring',
        className,
      )}
    >
      {media.map((m) => (
        <span
          key={m.id}
          className="h-56 shrink-0 snap-start overflow-hidden rounded-lg"
          style={{ aspectRatio: ratioOf(m, 0.75, 1.5, 0.8) }}
        >
          <MediaItem media={m} />
        </span>
      ))}
    </div>
  )
}
