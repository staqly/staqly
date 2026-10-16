'use client'

import { Download, ExternalLink, Eye, FileIcon, ImageOff, Play } from 'lucide-react'
import { useState } from 'react'

import { JsonBlock } from '@/components/common/json-code'
import { TypeIcon } from '@/components/common/type-icon'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { mediaThumbUrl, mediaUrl, type Item, type ThumbWidth } from '@/lib/api'
import { formatBytes, hostOf, prettyJSON } from '@/lib/format'
import { safeHttpUrl } from '@/lib/safe'
import { cn } from '@/lib/utils'

/** URL to render for image/video items: uploaded media first, else the external http(s) URL. */
function itemMediaSrc(item: Item): string | null {
  if (item.media?.id) {
    return mediaUrl(item.media.id)
  }

  if (item.type === 'image' || item.type === 'video') {
    return safeHttpUrl(item.url)
  }

  return null
}

// Types the API can scale down (`?w=`); others (AVIF, SVG-less video, ...) come back unchanged or 422.
const RESIZABLE_IMAGES = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp'])

/** Thumbnail URL for uploaded image media at `width`, or null (external media, videos, other files). */
function itemThumbSrc(item: Item, width: ThumbWidth): string | null {
  if (!item.media?.id || !RESIZABLE_IMAGES.has(item.media.mime_type)) {
    return null
  }

  return mediaThumbUrl(item.media.id, width)
}

/** Media hosted somewhere else (item.url): loading it tells that host who is looking. */
function isExternalMedia(item: Item) {
  return !item.media?.id
}

/** Click-to-load placeholder for external images and videos. */
function ExternalMediaGate({ src, kind, onLoad }: { src: string; kind: 'image' | 'video'; onLoad: () => void }) {
  return (
    <div
      data-testid="external-media-gate"
      className="flex h-full min-h-40 w-full flex-col items-center justify-center gap-3 p-4 text-center text-sm text-muted-foreground"
    >
      <span>
        External {kind} from <span className="font-mono text-foreground">{hostOf(src)}</span>
      </span>
      <Button
        variant="outline"
        size="sm"
        onClick={(e) => {
          // Inside clickable cards: loading the media must not also open the item.
          e.stopPropagation()
          onLoad()
        }}
        onKeyDown={(e) => e.stopPropagation()}
      >
        <Eye /> Load {kind}
      </Button>
    </div>
  )
}

function isVideoMime(item: Item) {
  return item.media?.mime_type?.startsWith('video/') ?? false
}

function BrokenMedia({ label }: { label: string }) {
  return (
    <div className="flex h-full min-h-40 w-full flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <ImageOff className="size-6" />
      {label}
    </div>
  )
}

interface PreviewProps {
  item: Item
  className?: string
  compact?: boolean
  /**
   * Inbox panes: show uploaded images as a ≤1280 px thumbnail (srcSet 640/1280) instead of the original,
   * which can be many MB. The item page and sheet keep the original.
   */
  scaled?: boolean
}

export function ItemPreview(props: PreviewProps) {
  return <ItemPreviewInner key={`${props.item.id}:${props.item.media?.id ?? props.item.url}`} {...props} />
}

function ItemPreviewInner({ item, className, compact = false, scaled = false }: PreviewProps) {
  const src = itemMediaSrc(item)
  const [broken, setBroken] = useState(false)
  const [loadExternal, setLoadExternal] = useState(false)
  const gated = !!src && isExternalMedia(item) && !loadExternal

  if (item.type === 'image' && src) {
    return (
      <div
        data-testid="preview-image"
        // min-h: a tiny image (a 1 px dot) still sits in a visible box instead of collapsing to a line.
        className={cn(
          'bg-checker flex min-h-24 items-center justify-center overflow-hidden rounded-lg border',
          className,
        )}
      >
        {gated ? (
          <ExternalMediaGate src={src} kind="image" onLoad={() => setLoadExternal(true)} />
        ) : broken ? (
          <BrokenMedia label="Image could not be loaded" />
        ) : (
          // A click opens the image at full size in a dialog.
          <ImageZoom src={src} alt={item.title || 'Image'}>
            <img
              src={(scaled && itemThumbSrc(item, 1280)) || src}
              srcSet={
                scaled && itemThumbSrc(item, 640)
                  ? `${itemThumbSrc(item, 640)} 640w, ${itemThumbSrc(item, 1280)} 1280w`
                  : undefined
              }
              sizes={scaled ? '(min-width: 768px) 55vw, 100vw' : undefined}
              alt={item.title || 'Image'}
              onError={() => setBroken(true)}
              className={cn('max-h-full max-w-full object-contain', compact ? 'max-h-[50vh]' : 'max-h-[70vh]')}
            />
          </ImageZoom>
        )}
      </div>
    )
  }

  if ((item.type === 'video' || isVideoMime(item)) && src) {
    // A compact tile with a play button; the video plays in a dialog at its own size. External videos ask
    // first (loading them tells that host who is looking).
    return (
      <div
        data-testid="preview-video"
        className={cn('flex justify-center rounded-lg border bg-muted/30 p-3', className)}
      >
        {gated ? (
          <ExternalMediaGate src={src} kind="video" onLoad={() => setLoadExternal(true)} />
        ) : (
          <VideoTile item={item} src={src} />
        )}
      </div>
    )
  }

  if (item.type === 'json') {
    return (
      <JsonBlock
        testId="preview-json"
        title="JSON content"
        json={item.content ? prettyJSON(item.content) : 'null'}
        className={cn(
          'scrollbar-thin overflow-auto rounded-lg border bg-muted/40 p-4 text-[12.5px]',
          compact ? 'max-h-[50vh]' : 'max-h-[70vh]',
          className,
        )}
      />
    )
  }

  if (item.type === 'link') {
    const raw = item.url || item.content
    const href = safeHttpUrl(raw)

    return (
      <div data-testid="preview-link" className={cn('rounded-lg border bg-card p-4', className)}>
        <div className="flex items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
            <TypeIcon type="link" className="size-5" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              {href ? hostOf(href) : 'No URL'}
            </div>
            <div className="mt-0.5 font-medium">{item.title || raw}</div>
            {href ? (
              <a
                href={href}
                target="_blank"
                rel="noopener noreferrer nofollow"
                className="mt-1 block truncate font-mono text-xs text-brand hover:underline"
              >
                {href}
              </a>
            ) : (
              // Not an http(s) URL (e.g. javascript:): show it, never link it.
              raw && <div className="mt-1 font-mono text-xs break-all text-muted-foreground">{raw}</div>
            )}
            {item.content && item.content !== raw && (
              <p className="mt-3 text-sm whitespace-pre-wrap text-muted-foreground">{item.content}</p>
            )}
          </div>
          {href && (
            <Button asChild variant="outline" size="sm">
              <a href={href} target="_blank" rel="noopener noreferrer nofollow">
                <ExternalLink /> Open
              </a>
            </Button>
          )}
        </div>
      </div>
    )
  }

  if (item.type === 'file' || (item.media && !src)) {
    const m = item.media
    const fileHref = safeHttpUrl(item.url)

    return (
      <div
        data-testid="preview-file"
        className={cn('flex items-center gap-3 rounded-lg border bg-card p-4', className)}
      >
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
          <FileIcon className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">{m?.original_name || item.title || 'File'}</div>
          <div className="text-xs text-muted-foreground">
            {m ? `${m.mime_type} · ${formatBytes(m.size_bytes)}` : item.url ? hostOf(item.url) : 'No file attached'}
          </div>
          {item.content && <p className="mt-2 text-sm whitespace-pre-wrap text-muted-foreground">{item.content}</p>}
        </div>
        {m ? (
          <Button asChild variant="outline" size="sm">
            <a href={mediaUrl(m.id, { download: true })} download={m.original_name}>
              <Download /> Download
            </a>
          </Button>
        ) : fileHref ? (
          <Button asChild variant="outline" size="sm">
            <a href={fileHref} target="_blank" rel="noopener noreferrer nofollow">
              <ExternalLink /> Open
            </a>
          </Button>
        ) : null}
      </div>
    )
  }

  // text (and image/video items missing a source)
  if ((item.type === 'image' || item.type === 'video') && !src) {
    return (
      <div className={cn('rounded-lg border bg-muted/30', className)}>
        <BrokenMedia label={`No ${item.type} attached`} />
      </div>
    )
  }

  return (
    <div
      data-testid="preview-text"
      className={cn(
        'scrollbar-thin overflow-auto rounded-lg border bg-card p-5 text-[14px] leading-relaxed whitespace-pre-wrap',
        compact ? 'max-h-[50vh]' : 'max-h-[70vh]',
        className,
      )}
    >
      {item.content || <span className="text-muted-foreground">—</span>}
    </div>
  )
}

function ImageZoom({ src, alt, children }: { src: string; alt: string; children: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  // Small images (icons, a 1 px dot) are scaled up crisply so there is something to see; photos stay smooth.
  const [tiny, setTiny] = useState(false)

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={`Open ${alt} full size`}
        className="flex max-h-full max-w-full cursor-zoom-in items-center justify-center outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {children}
      </button>
      {open && (
        <Dialog open onOpenChange={setOpen}>
          <DialogContent className="bg-checker p-2 sm:max-w-[min(90vw,1600px)]">
            <DialogTitle className="sr-only">{alt}</DialogTitle>
            <img
              src={src}
              alt={alt}
              onLoad={(e) => setTiny(e.currentTarget.naturalWidth < 128)}
              className={cn(
                'mx-auto max-h-[85vh] max-w-full object-contain',
                tiny && 'min-h-32 [image-rendering:pixelated]',
              )}
            />
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}

function VideoTile({ item, src }: { item: Item; src: string }) {
  const [open, setOpen] = useState(false)
  const name = item.media?.original_name || item.title || 'Video'

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="group flex aspect-video w-full max-w-sm flex-col items-center justify-center gap-2 rounded-md bg-black text-white/90 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        aria-label={`Play ${name}`}
      >
        <span className="flex size-12 items-center justify-center rounded-full bg-white/15 transition-colors group-hover:bg-white/25">
          <Play className="size-5 fill-current" />
        </span>
        <span className="max-w-[90%] truncate text-xs text-white/70">
          {name}
          {item.media ? ` · ${formatBytes(item.media.size_bytes)}` : ''}
        </span>
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="bg-black p-0 sm:max-w-4xl" showCloseButton>
          <DialogTitle className="sr-only">{name}</DialogTitle>
          {open && (
            // Plays only after the click above, never on its own.
            // oxlint-disable-next-line jsx-a11y/media-has-caption -- user-uploaded media; staqly has no caption tracks to offer
            <video src={src} controls autoPlay playsInline className="max-h-[80vh] w-full rounded-xl" />
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}

/** Small square thumbnail for lists, tables and the inbox. */
export function ItemThumb(props: { item: Item; className?: string }) {
  return <ItemThumbInner key={`${props.item.id}:${props.item.media?.id ?? props.item.url}`} {...props} />
}

function ItemThumbInner({ item, className }: { item: Item; className?: string }) {
  // Lists never load external media (it would ping third-party hosts for every row).
  const src = isExternalMedia(item) ? null : itemMediaSrc(item)
  const [broken, setBroken] = useState(false)
  const base = cn(
    'relative flex shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted',
    className,
  )

  if (src && !broken && item.type === 'image') {
    // Thumbnails are at most 48 CSS px: 160 px covers 3× screens.
    return (
      <div className={base}>
        <img
          src={itemThumbSrc(item, 160) ?? src}
          alt=""
          loading="lazy"
          decoding="async"
          className="size-full object-cover"
          onError={() => setBroken(true)}
        />
      </div>
    )
  }

  if (src && !broken && (item.type === 'video' || isVideoMime(item))) {
    return (
      <div className={base}>
        <video
          src={`${src}#t=0.1`}
          muted
          playsInline
          preload="metadata"
          className="size-full object-cover"
          onError={() => setBroken(true)}
        />
        <span className="absolute right-0.5 bottom-0.5 rounded bg-black/60 p-0.5 text-white">
          <TypeIcon type="video" className="size-2.5" />
        </span>
      </div>
    )
  }

  return (
    <div className={cn(base, 'text-muted-foreground')}>
      <TypeIcon type={item.type} className="size-4" />
    </div>
  )
}
