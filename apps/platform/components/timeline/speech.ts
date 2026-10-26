// Captions from speech, on the device: Whisper (base, many languages; Xenova/whisper-base, Apache-2.0) runs in the
// browser with the same pinned transformers.js as the design editor's background removal. Nothing is uploaded; the
// first use downloads the model (about 80 MB), which the browser keeps for next time.
import { TRANSFORMERS_CDN } from '@/components/design/background'

const MODEL = 'Xenova/whisper-base'

// oxlint-disable-next-line typescript/no-explicit-any -- transformers.js loads from the CDN, without types
type Tf = any

let loading: Promise<Tf> | null = null

function load(onProgress?: (share: number) => void) {
  loading ??= (async () => {
    const tf: Tf = await import(/* webpackIgnore: true */ TRANSFORMERS_CDN)

    tf.env.cacheKey = 'staqly-design-models'

    // WebAssembly, quantized: the half-precision models answer NaN there, and q8 is small and quick enough.
    return tf.pipeline('automatic-speech-recognition', MODEL, {
      device: 'wasm',
      dtype: 'q8',
      progress_callback: (p: { status: string; loaded?: number; total?: number }) => {
        if (p.status === 'progress' && p.total) {
          onProgress?.((p.loaded ?? 0) / p.total)
        }
      },
    })
  })().catch((e: unknown) => {
    loading = null

    throw e
  })

  return loading
}

/** The sound at 16 kHz mono, what Whisper listens to. */
async function mono16k(buf: AudioBuffer) {
  const ctx = new OfflineAudioContext(1, Math.ceil(buf.duration * 16_000), 16_000)
  const src = ctx.createBufferSource()

  src.buffer = buf
  src.connect(ctx.destination)
  src.start()

  return (await ctx.startRendering()).getChannelData(0)
}

export interface Spoken {
  text: string
  start: number
  end: number
}

/** What is said in the sound, in pieces with their times (a sentence or a phrase each). */
export async function transcribe(buf: AudioBuffer, onProgress?: (share: number) => void): Promise<Spoken[]> {
  const asr = await load(onProgress)
  const out = await asr(await mono16k(buf), { return_timestamps: true, chunk_length_s: 30, stride_length_s: 5 })
  const chunks: { text: string; timestamp: [number, number | null] }[] = out.chunks ?? []

  return chunks
    .map((c) => ({ text: c.text.trim(), start: c.timestamp[0], end: c.timestamp[1] ?? buf.duration }))
    .filter((c) => c.text && c.end > c.start)
}

/** Long pieces in parts of at most `words` words, their time shared by length. */
export function splitSpoken(pieces: Spoken[], words = 8): Spoken[] {
  return pieces.flatMap((p) => {
    const list = p.text.split(/\s+/)

    if (list.length <= words) {
      return [p]
    }

    const parts = Math.ceil(list.length / words)
    const each = (p.end - p.start) / list.length

    return Array.from({ length: parts }, (_, i) => {
      const from = i * words
      const to = Math.min(list.length, from + words)

      return { text: list.slice(from, to).join(' '), start: p.start + from * each, end: p.start + to * each }
    })
  })
}
