// The video and sound editor's document: a timeline. A video has a picture track (pictures and video clips back to
// back, each fading or sliding into the next), captions over it and sound clips under it; a sound is the sound clips
// alone. Plain JSON, saved as a media project (/v1/media-projects) next to the file it exports.

export type Fit = 'cover' | 'contain'

/** How a picture moves while it is on screen (the "Ken Burns" look). */
export type Motion = 'none' | 'zoom-in' | 'zoom-out' | 'pan-left' | 'pan-right'

/** How a clip comes in after the one before it. */
export type Transition = 'cut' | 'fade' | 'slide' | 'zoom'

export interface Clip {
  id: string
  kind: 'image' | 'video'
  mediaId: string
  src: string
  name: string
  /** Its own size, for fitting it to the frame. */
  width: number
  height: number
  /** Seconds on the timeline. */
  duration: number
  /** Video: where in the file the clip starts, in seconds (the part before it is trimmed off). */
  in: number
  /** Video: the file's length, so a trim never runs past it. */
  fileDuration?: number
  fit: Fit
  motion: Motion
  transition: Transition
  /** Video: its own sound, 0 to 1. */
  volume: number
}

export interface Caption {
  id: string
  text: string
  /** Seconds on the timeline. */
  start: number
  end: number
  font: string
  /** A share of the frame's height. */
  size: number
  color: string
  bold: boolean
  /** A box behind the words, or none. */
  box: string
  /** Where its middle is, as a share of the frame's height. */
  y: number
}

export interface Sound {
  id: string
  mediaId: string
  src: string
  name: string
  /** Where it starts on the timeline, in seconds. */
  start: number
  /** Where in the file it starts (the part before it is trimmed off). */
  in: number
  /** How long it plays. */
  duration: number
  fileDuration: number
  /** 0 to 1. */
  volume: number
  /** Seconds of rising and falling volume at its ends. */
  fadeIn: number
  fadeOut: number
}

export interface Timeline {
  version: 1
  kind: 'video' | 'audio'
  name: string
  width: number
  height: number
  fps: number
  /** Behind pictures that don't fill the frame. */
  background: string
  clips: Clip[]
  captions: Caption[]
  sounds: Sound[]
}

export const uid = () => Math.random().toString(36).slice(2, 10)

export const PICTURE_SECONDS = 3
export const FADE_SECONDS = 0.5

export function newTimeline(kind: Timeline['kind'], width = 1080, height = 1920): Timeline {
  return {
    version: 1,
    kind,
    name: kind === 'video' ? 'Untitled video' : 'Untitled sound',
    width: kind === 'video' ? width : 0,
    height: kind === 'video' ? height : 0,
    fps: 30,
    background: '#000000',
    clips: [],
    captions: [],
    sounds: [],
  }
}

export function newClip(m: {
  id: string
  kind: 'image' | 'video'
  src: string
  name: string
  width: number
  height: number
  duration?: number
}): Clip {
  const video = m.kind === 'video'

  return {
    id: uid(),
    kind: m.kind,
    mediaId: m.id,
    src: m.src,
    name: m.name,
    width: m.width || 1080,
    height: m.height || 1080,
    duration: video ? Math.max(0.5, m.duration ?? PICTURE_SECONDS) : PICTURE_SECONDS,
    in: 0,
    fileDuration: video ? m.duration : undefined,
    fit: 'cover',
    motion: video ? 'none' : 'zoom-in',
    transition: 'fade',
    volume: video ? 1 : 0,
  }
}

export function newSound(m: { id: string; src: string; name: string; duration: number }, start = 0): Sound {
  return {
    id: uid(),
    mediaId: m.id,
    src: m.src,
    name: m.name,
    start,
    in: 0,
    duration: m.duration,
    fileDuration: m.duration,
    volume: 1,
    fadeIn: 0,
    fadeOut: 0,
  }
}

export function newCaption(t: Timeline, at: number, text = 'Your words here'): Caption {
  return {
    id: uid(),
    text,
    start: at,
    // Two seconds, or to the end of the pictures when they end sooner.
    end: at + Math.max(0.5, Math.min(2, (picturesLength(t) || at + 2) - at)),
    font: 'Montserrat',
    size: 0.045,
    color: '#ffffff',
    bold: true,
    box: 'rgba(0,0,0,0.55)',
    y: 0.8,
  }
}

/** The picture track's length: its clips back to back. */
export const picturesLength = (t: Timeline) => t.clips.reduce((n, c) => n + c.duration, 0)

/** How long the whole timeline plays: the picture track, the captions or the sound, whichever ends last. */
export function length(t: Timeline) {
  return Math.max(picturesLength(t), ...t.captions.map((c) => c.end), ...t.sounds.map((s) => s.start + s.duration), 0)
}

/** Where each clip starts on the timeline. */
export function clipStarts(t: Timeline) {
  let at = 0

  return t.clips.map((c) => {
    const start = at

    at += c.duration

    return start
  })
}

/** The clip on screen at time `at`, with its index and how far into it `at` is. */
export function clipAt(t: Timeline, at: number) {
  const starts = clipStarts(t)

  for (let i = t.clips.length - 1; i >= 0; i--) {
    if (at >= starts[i]!) {
      return { index: i, clip: t.clips[i]!, local: at - starts[i]! }
    }
  }

  return null
}

/** How long a clip's transition in lasts (none for the first clip or a cut), never more than half of either clip. */
export function transitionLength(t: Timeline, i: number) {
  const c = t.clips[i]

  if (!c || i === 0 || c.transition === 'cut') {
    return 0
  }

  return Math.min(FADE_SECONDS, c.duration / 2, t.clips[i - 1]!.duration / 2)
}

/** Sound clips laid out in lanes so overlapping ones sit on different rows. */
export function soundLanes(sounds: Sound[]) {
  const ends: number[] = []
  const lane = new Map<string, number>()

  for (const s of sounds.toSorted((a, b) => a.start - b.start)) {
    let i = ends.findIndex((end) => end <= s.start + 1e-6)

    if (i < 0) {
      i = ends.length
      ends.push(0)
    }

    ends[i] = s.start + s.duration
    lane.set(s.id, i)
  }

  return { lane, count: Math.max(1, ends.length) }
}

/** A volume curve point for a sound at `local` seconds into it: its volume with the fades applied. */
export function gainAt(s: Sound, local: number) {
  const up = s.fadeIn > 0 ? Math.min(1, local / s.fadeIn) : 1
  const down = s.fadeOut > 0 ? Math.min(1, (s.duration - local) / s.fadeOut) : 1

  return s.volume * Math.max(0, Math.min(up, down))
}

/** Splits a sound in two at timeline time `at` (when `at` is inside it). */
export function splitSound(s: Sound, at: number): [Sound, Sound] | null {
  const local = at - s.start

  if (local <= 0.05 || local >= s.duration - 0.05) {
    return null
  }

  return [
    { ...s, duration: local, fadeOut: 0 },
    { ...s, id: uid(), start: at, in: s.in + local, duration: s.duration - local, fadeIn: 0 },
  ]
}

/** Splits a video clip in two at `local` seconds into it. */
export function splitClip(c: Clip, local: number): [Clip, Clip] | null {
  if (local <= 0.1 || local >= c.duration - 0.1) {
    return null
  }

  return [
    { ...c, duration: local },
    { ...c, id: uid(), in: c.kind === 'video' ? c.in + local : 0, duration: c.duration - local, transition: 'cut' },
  ]
}

/** A timeline file of the video and sound editor, or null for anything else (a design, a broken file). */
export function parseTimeline(text: string): Timeline | null {
  try {
    const t = JSON.parse(text) as Partial<Timeline>

    if (t.version !== 1 || (t.kind !== 'video' && t.kind !== 'audio')) {
      return null
    }

    return {
      ...newTimeline(t.kind),
      ...t,
      clips: t.clips ?? [],
      captions: t.captions ?? [],
      sounds: t.sounds ?? [],
    } as Timeline
  } catch {
    return null
  }
}

/** Seconds as people read them on a timeline: 0:07.5, 1:02. */
export function clock(s: number, tenths = true) {
  const m = Math.floor(s / 60)
  const rest = s - m * 60

  return `${m}:${(tenths ? rest.toFixed(1) : Math.floor(rest).toString()).padStart(tenths ? 4 : 2, '0')}`
}

/** Frame sizes for a video, by where it goes. */
export const VIDEO_SIZES = [
  { label: 'Story, Reel, TikTok, Short (9:16)', width: 1080, height: 1920 },
  { label: 'Feed portrait (4:5)', width: 1080, height: 1350 },
  { label: 'Square (1:1)', width: 1080, height: 1080 },
  { label: 'Landscape, YouTube (16:9)', width: 1920, height: 1080 },
]
