// The timeline's sound with the Web Audio API: files decoded once, played in step with the preview, and mixed into
// one track for the exported file. Video clips' own sound is part of it, mixed like the sound clips.
import { clipStarts, length, type Sound, type Timeline } from './doc'

const RATE = 48_000
const decoded = new Map<string, Promise<AudioBuffer | null>>()

/** The file's sound, decoded (null for a file without sound, like a silent video). Kept for the next use. */
export function decode(src: string): Promise<AudioBuffer | null> {
  let p = decoded.get(src)

  if (!p) {
    p = fetch(src)
      .then((r) => r.arrayBuffer())
      .then((b) => new OfflineAudioContext(2, 1, RATE).decodeAudioData(b))
      .catch(() => null)
    decoded.set(src, p)
  }

  return p
}

/** Everything that sounds: the sound clips and the video clips with their volume up, on one list. */
function voices(t: Timeline): Sound[] {
  const starts = clipStarts(t)
  const fromVideos = t.clips.flatMap((c, i) =>
    c.kind === 'video' && c.volume > 0
      ? [
          {
            id: c.id,
            mediaId: c.mediaId,
            src: c.src,
            name: c.name,
            start: starts[i]!,
            in: c.in,
            duration: c.duration,
            fileDuration: c.fileDuration ?? c.duration,
            volume: c.volume,
            fadeIn: 0,
            fadeOut: 0,
          },
        ]
      : [],
  )

  return [...t.sounds, ...fromVideos]
}

/**
 * Sets a voice's volume over its time, its fades included, from `from` seconds into it; `when` is the clock time of
 * its local 0 (in the past when it plays from its middle).
 */
function envelope(g: GainNode, s: Sound, when: number, from: number) {
  const v = g.gain
  const at = (local: number) => when + local

  v.setValueAtTime(s.volume * (s.fadeIn > 0 ? Math.min(1, from / s.fadeIn) : 1), at(from))

  if (s.fadeIn > 0 && from < s.fadeIn) {
    v.linearRampToValueAtTime(s.volume, at(s.fadeIn))
  }

  if (s.fadeOut > 0) {
    const down = Math.max(from, s.duration - s.fadeOut)

    v.setValueAtTime(s.volume, at(down))
    v.linearRampToValueAtTime(0, at(s.duration))
  }
}

/** Plays every voice of the timeline from `from` seconds on, in step with `ctx`'s clock; stop() silences it. */
export async function play(ctx: AudioContext, t: Timeline, from: number) {
  const list = voices(t).filter((s) => s.start + s.duration > from)
  const buffers = await Promise.all(list.map((s) => decode(s.src)))
  const now = ctx.currentTime + 0.05
  const nodes: AudioBufferSourceNode[] = []

  for (const [i, s] of list.entries()) {
    const buf = buffers[i]

    if (!buf) {
      continue
    }

    const into = Math.max(0, from - s.start)
    const src = ctx.createBufferSource()
    const g = ctx.createGain()

    src.buffer = buf
    src.connect(g).connect(ctx.destination)
    envelope(g, s, now + Math.max(0, s.start - from) - into, into)
    src.start(now + Math.max(0, s.start - from), s.in + into, s.duration - into)
    nodes.push(src)
  }

  return {
    /** When the playback's time 0 (the timeline's `from`) is on ctx's clock. */
    origin: now,
    stop: () => nodes.forEach((n) => n.stop()),
  }
}

/** The whole timeline's sound as one stereo buffer (silence where nothing plays). */
export async function mix(t: Timeline, onProgress?: (share: number) => void): Promise<AudioBuffer> {
  const len = Math.max(0.1, length(t))
  const ctx = new OfflineAudioContext(2, Math.ceil(len * RATE), RATE)
  const list = voices(t)
  let done = 0

  for (const s of list) {
    const buf = await decode(s.src)

    onProgress?.(++done / Math.max(1, list.length))

    if (!buf) {
      continue
    }

    const src = ctx.createBufferSource()
    const g = ctx.createGain()

    src.buffer = buf
    src.connect(g).connect(ctx.destination)
    envelope(g, s, s.start, 0)
    src.start(s.start, s.in, s.duration)
  }

  return ctx.startRendering()
}

/** The loudest sample in each of `n` slices of the sound, 0 to 1: the waveform drawn on a sound clip. */
export function peaks(buf: AudioBuffer, from: number, duration: number, n: number) {
  const data = buf.getChannelData(0)
  const a = Math.floor(from * buf.sampleRate)
  const step = Math.max(1, Math.floor((duration * buf.sampleRate) / n))
  const out = new Float32Array(n)

  for (let i = 0; i < n; i++) {
    let max = 0
    const start = a + i * step

    // A sparse look at each slice keeps a long file quick.
    for (let j = 0; j < step; j += Math.max(1, Math.floor(step / 64))) {
      const v = Math.abs(data[start + j] ?? 0)

      if (v > max) {
        max = v
      }
    }

    out[i] = max
  }

  return out
}

/** A 16-bit PCM WAV file of a buffer: the sound export where the browser can't encode AAC. */
export function wav(buf: AudioBuffer): Blob {
  const ch = buf.numberOfChannels
  const n = buf.length
  const out = new DataView(new ArrayBuffer(44 + n * ch * 2))
  const text = (at: number, s: string) => [...s].forEach((c, i) => out.setUint8(at + i, c.charCodeAt(0)))

  text(0, 'RIFF')
  out.setUint32(4, 36 + n * ch * 2, true)
  text(8, 'WAVE')
  text(12, 'fmt ')
  out.setUint32(16, 16, true)
  out.setUint16(20, 1, true)
  out.setUint16(22, ch, true)
  out.setUint32(24, buf.sampleRate, true)
  out.setUint32(28, buf.sampleRate * ch * 2, true)
  out.setUint16(32, ch * 2, true)
  out.setUint16(34, 16, true)
  text(36, 'data')
  out.setUint32(40, n * ch * 2, true)

  const channels = Array.from({ length: ch }, (_, c) => buf.getChannelData(c))
  let at = 44

  for (let i = 0; i < n; i++) {
    for (const c of channels) {
      const v = Math.max(-1, Math.min(1, c[i]!))

      out.setInt16(at, v < 0 ? v * 0x8000 : v * 0x7fff, true)
      at += 2
    }
  }

  return new Blob([out], { type: 'audio/wav' })
}
