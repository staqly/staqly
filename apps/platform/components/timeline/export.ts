// The timeline as a file, made in the browser: every frame drawn by drawFrame and encoded with WebCodecs, the sound
// mixed by the Web Audio API, both written to MP4 (M4A for a sound) by Mediabunny (MPL-2.0). Nothing is uploaded
// until the file is done.
import {
  ALL_FORMATS,
  AudioBufferSource,
  BufferTarget,
  CanvasSink,
  CanvasSource,
  getFirstEncodableAudioCodec,
  getFirstEncodableVideoCodec,
  Input,
  Mp4OutputFormat,
  Output,
  QUALITY_HIGH,
  UrlSource,
} from 'mediabunny'

import { loadFont } from '@/components/design/fonts'

import { mix, wav } from './audio'
import { clipAt, length, transitionLength, type Clip, type Timeline } from './doc'
import { drawFrame } from './render'

export interface Progress {
  stage: 'preparing' | 'frames' | 'sound' | 'finishing'
  /** 0 to 1 within the stage. */
  share: number
}

/** Whether this browser can make the video file (WebCodecs with an H.264, VP9 or AV1 encoder). */
export async function canExportVideo(width = 1080, height = 1920) {
  return (
    typeof VideoEncoder !== 'undefined' &&
    !!(await getFirstEncodableVideoCodec(['avc', 'vp9', 'av1'], { width, height }))
  )
}

/** Pictures decoded once per export. */
async function bitmap(src: string) {
  return createImageBitmap(await (await fetch(src)).blob())
}

/** The time in a video file a clip shows at `local` seconds into it, kept inside the file. */
const fileTime = (c: Clip, local: number) => Math.min(c.in + local, Math.max(0, (c.fileDuration ?? Infinity) - 0.05))

export async function exportVideo(t: Timeline, onProgress: (p: Progress) => void, signal?: AbortSignal): Promise<Blob> {
  onProgress({ stage: 'preparing', share: 0 })

  // H.264 wants even sizes.
  const W = t.width - (t.width % 2)
  const H = t.height - (t.height % 2)
  const frame = { ...t, width: W, height: H }
  const videoCodec = await getFirstEncodableVideoCodec(['avc', 'vp9', 'av1'], { width: W, height: H })

  if (!videoCodec) {
    throw new Error('This browser cannot make video files. Try a recent Chrome or Edge.')
  }

  const audioCodec = await getFirstEncodableAudioCodec(['aac', 'opus'])
  const canvas = new OffscreenCanvas(W, H)
  const ctx = canvas.getContext('2d')!
  const output = new Output({ format: new Mp4OutputFormat({ fastStart: 'in-memory' }), target: new BufferTarget() })
  const pictures = new CanvasSource(canvas, { codec: videoCodec, bitrate: QUALITY_HIGH, keyFrameInterval: 2 })

  output.addVideoTrack(pictures, { frameRate: t.fps })

  const sound = audioCodec ? new AudioBufferSource({ codec: audioCodec, bitrate: QUALITY_HIGH }) : null

  if (sound) {
    output.addAudioTrack(sound)
  }

  // Everything a frame needs, ready before the first one: pictures, video decoders, the captions' fonts.
  const images = new Map<string, ImageBitmap>()
  const videos = new Map<string, { input: Input; sink: CanvasSink }>()

  await Promise.all([
    ...[...new Set(t.clips.filter((c) => c.kind === 'image').map((c) => c.src))].map(async (src) =>
      images.set(src, await bitmap(src)),
    ),
    ...[...new Set(t.clips.filter((c) => c.kind === 'video').map((c) => c.src))].map(async (src) => {
      const input = new Input({ formats: ALL_FORMATS, source: new UrlSource(src) })
      const track = await input.getPrimaryVideoTrack()

      if (track) {
        videos.set(src, { input, sink: new CanvasSink(track, { poolSize: 3 }) })
      }
    }),
    ...[...new Set(t.captions.map((c) => `${c.font}|${c.bold}`))].map((f) =>
      loadFont(f.split('|')[0]!, f.endsWith('true')).catch(() => undefined),
    ),
  ])

  try {
    // The sound is mixed first and fed alongside the frames: the MP4 interleaves both tracks, so frames alone
    // would wait for sound that never comes.
    const mixed = sound ? await mix(t, (share) => onProgress({ stage: 'sound', share })) : null

    signal?.throwIfAborted()
    await output.start()

    const soundDone = sound && mixed ? sound.add(mixed) : Promise.resolve()
    const total = Math.max(1, Math.ceil(length(t) * t.fps))
    const frames = new Map<string, CanvasImageSource>()

    for (let i = 0; i < total; i++) {
      signal?.throwIfAborted()

      const at = i / t.fps
      const on = clipAt(t, at)

      frames.clear()

      // The video frames this moment shows: its clip's, and the clip before it during a transition.
      if (on) {
        const need: [Clip, number][] = [[on.clip, on.local]]

        if (on.local < transitionLength(t, on.index)) {
          const prev = t.clips[on.index - 1]!

          need.push([prev, prev.duration])
        }

        for (const [c, local] of need) {
          const v = c.kind === 'video' ? videos.get(c.src) : undefined
          const got = v && (await v.sink.getCanvas(fileTime(c, local)))

          if (got) {
            frames.set(c.id, got.canvas)
          }
        }
      }

      drawFrame(ctx, frame, at, (c) => (c.kind === 'image' ? (images.get(c.src) ?? null) : (frames.get(c.id) ?? null)))
      await pictures.add(at, 1 / t.fps)
      onProgress({ stage: 'frames', share: (i + 1) / total })
    }

    await soundDone
    onProgress({ stage: 'finishing', share: 1 })
    await output.finalize()

    return new Blob([(output.target as BufferTarget).buffer!], { type: 'video/mp4' })
  } catch (e) {
    if (output.state === 'started') {
      await output.cancel()
    }

    throw e
  } finally {
    images.forEach((b) => b.close())
    videos.forEach((v) => v.input.dispose())
  }
}

/** The sound timeline as an M4A file (AAC), or WAV where the browser can't encode AAC. */
export async function exportAudio(t: Timeline, onProgress: (p: Progress) => void): Promise<Blob> {
  const mixed = await mix(t, (share) => onProgress({ stage: 'sound', share }))
  const codec = typeof AudioEncoder !== 'undefined' ? await getFirstEncodableAudioCodec(['aac']) : null

  if (!codec) {
    return wav(mixed)
  }

  onProgress({ stage: 'finishing', share: 1 })

  const output = new Output({ format: new Mp4OutputFormat({ fastStart: 'in-memory' }), target: new BufferTarget() })
  const src = new AudioBufferSource({ codec, bitrate: QUALITY_HIGH })

  output.addAudioTrack(src)
  await output.start()
  await src.add(mixed)
  await output.finalize()

  return new Blob([(output.target as BufferTarget).buffer!], { type: 'audio/mp4' })
}
