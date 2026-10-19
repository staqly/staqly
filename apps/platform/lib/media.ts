import type { Media, MediaKind } from '@/lib/api'

/**
 * Picture, video or audio, from the stored type; "file" only for documents uploaded before the library took
 * media alone.
 */
export function mediaKind(mime: string): MediaKind | 'file' {
  for (const k of ['image', 'video', 'audio'] as const) {
    if (mime.startsWith(`${k}/`)) {
      return k
    }
  }

  return 'file'
}

/** 0:15, 3:07, 1:02:03 */
export function formatDuration(ms: number) {
  const total = Math.round(ms / 1000)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = String(total % 60).padStart(2, '0')

  return h ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

const gcd = (a: number, b: number): number => (b ? gcd(b, a % b) : a)

/** 1080x1350 -> "4:5"; sizes within 1% of a common shape read as that shape. */
export function ratioLabel(width: number, height: number) {
  if (!width || !height) {
    return ''
  }

  const near = COMMON.find((c) => Math.abs(width / height / (c.w / c.h) - 1) < 0.01)

  if (near) {
    return near.label
  }

  const d = gcd(width, height)

  return `${width / d}:${height / d}`
}

const COMMON = [
  { label: '9:16', w: 9, h: 16 },
  { label: '2:3', w: 2, h: 3 },
  { label: '4:5', w: 4, h: 5 },
  { label: '1:1', w: 1, h: 1 },
  { label: '16:9', w: 16, h: 9 },
  { label: '1.91:1', w: 1.91, h: 1 },
]

/** Where a picture or video fits without cropping, by placement (shared with the media editor). */
export { PLACEMENTS } from '@staqly/brand/placements'

/** Fits (under 1% cropped), crops a little (up to 25%) or crops a lot. */
export function fitVerdict(crop: number): { tone: 'fits' | 'some' | 'much'; label: string } {
  if (crop < 0.01) {
    return { tone: 'fits', label: 'Fits' }
  }

  return { tone: crop <= 0.25 ? 'some' : 'much', label: `Crops ${Math.round(crop * 100)}%` }
}

/** square, portrait or landscape (within 1% of square reads as square). */
export function shapeName(width: number, height: number) {
  const r = width / height

  return Math.abs(r - 1) < 0.01 ? 'square' : r < 1 ? 'portrait' : 'landscape'
}

/** image/png -> "PNG image", video/quicktime -> "QUICKTIME video", application/pdf -> "PDF file". */
export function typeLabel(mime: string) {
  const [top, sub = ''] = mime.split('/')
  const name = sub
    .replace(/^(x-|vnd\.)/, '')
    .split(/[.+;]/)[0]
    .toUpperCase()
  const noun = top === 'image' ? 'image' : top === 'video' ? 'video' : top === 'audio' ? 'audio' : 'file'

  return name ? `${name === 'JPEG' ? 'JPG' : name} ${noun}` : noun
}

/** Folders in tree order (parents before children, siblings by name) with their depth. */
export function folderTree<T extends { id: string; parent_id: string; name: string }>(folders: T[]) {
  const ids = new Set(folders.map((f) => f.id))
  const byParent = new Map<string, T[]>()

  for (const f of folders) {
    // A folder whose parent is gone shows at the top level.
    const parent = ids.has(f.parent_id) ? f.parent_id : ''

    byParent.set(parent, [...(byParent.get(parent) ?? []), f])
  }

  const out: (T & { depth: number; hasChildren: boolean })[] = []
  const walk = (parent: string, depth: number) => {
    for (const f of (byParent.get(parent) ?? []).toSorted((a, b) => a.name.localeCompare(b.name))) {
      out.push({ ...f, depth, hasChildren: byParent.has(f.id) })
      walk(f.id, depth + 1)
    }
  }

  walk('', 0)

  return out
}

/** How much of the file a placement crops when it fills the frame: 0 = fits as it is. */
export function cropShare(width: number, height: number, w: number, h: number) {
  const r = width / height
  const target = w / h

  return Math.max(0, 1 - Math.min(r, target) / Math.max(r, target))
}

/** A video's size and length, read by the browser (the server has no video tools). */
export function measureVideo(file: File): Promise<{ width: number; height: number; duration_ms: number } | null> {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file)
    // A video element reads audio files too (width and height stay 0), so audio gets its length the same way.
    const video = document.createElement('video')
    const done = (v: { width: number; height: number; duration_ms: number } | null) => {
      URL.revokeObjectURL(url)
      resolve(v)
    }

    video.preload = 'metadata'
    video.muted = true
    video.addEventListener('loadedmetadata', () =>
      done({
        width: video.videoWidth,
        height: video.videoHeight,
        duration_ms: Number.isFinite(video.duration) ? Math.round(video.duration * 1000) : 0,
      }),
    )
    video.addEventListener('error', () => done(null))
    video.src = url
  })
}

/** The size and length line under a file: "1080 x 1350 · 0:15 · 2.4 MB". */
export function mediaFacts(m: Media, bytes: (n: number) => string) {
  return [
    m.width && m.height ? `${m.width} x ${m.height}` : '',
    m.duration_ms ? formatDuration(m.duration_ms) : '',
    bytes(m.size_bytes),
  ]
    .filter(Boolean)
    .join(' · ')
}
