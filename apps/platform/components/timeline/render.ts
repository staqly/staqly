// One frame of a video timeline, drawn on a 2D canvas: the same code draws the preview and every frame of the
// exported file, so what plays in the editor is what gets saved.
import { clipAt, transitionLength, type Caption, type Clip, type Timeline } from './doc'

/** What a clip shows at `local` seconds into it: a picture, or a video's frame (null while it loads). */
export type FrameSource = (clip: Clip, local: number) => CanvasImageSource | null

const ZOOM = 0.12

/** The clip's picture, fitted to the frame, moved by its motion at progress `q` (0 to 1). */
function drawClip(
  ctx: CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D,
  t: Timeline,
  clip: Clip,
  img: CanvasImageSource,
  q: number,
) {
  const W = t.width
  const H = t.height
  const k = clip.fit === 'cover' ? Math.max(W / clip.width, H / clip.height) : Math.min(W / clip.width, H / clip.height)
  let scale = 1
  let dx = 0

  switch (clip.motion) {
    case 'zoom-in':
      scale = 1 + ZOOM * q
      break
    case 'zoom-out':
      scale = 1 + ZOOM * (1 - q)
      break
    case 'pan-left':
      scale = 1 + ZOOM
      dx = (ZOOM / 2) * W * (1 - 2 * q)
      break
    case 'pan-right':
      scale = 1 + ZOOM
      dx = (ZOOM / 2) * W * (2 * q - 1)
      break
  }

  const w = clip.width * k * scale
  const h = clip.height * k * scale

  ctx.drawImage(img, (W - w) / 2 + dx, (H - h) / 2, w, h)
}

function wrap(ctx: CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D, text: string, max: number) {
  const lines: string[] = []

  for (const para of text.split('\n')) {
    let line = ''

    for (const word of para.split(/\s+/)) {
      const next = line ? `${line} ${word}` : word

      if (line && ctx.measureText(next).width > max) {
        lines.push(line)
        line = word
      } else {
        line = next
      }
    }

    lines.push(line)
  }

  return lines
}

function drawCaption(
  ctx: CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D,
  t: Timeline,
  c: Caption,
  at: number,
) {
  const size = c.size * t.height
  // A quick fade at both ends.
  const alpha = Math.min(1, (at - c.start) / 0.2, (c.end - at) / 0.2)

  ctx.save()
  ctx.globalAlpha = Math.max(0, alpha)
  ctx.font = `${c.bold ? 'bold ' : ''}${size}px "${c.font}", sans-serif`
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'

  const lines = wrap(ctx, c.text, t.width * 0.84)
  const lh = size * 1.25
  const top = c.y * t.height - (lines.length * lh) / 2

  if (c.box) {
    const w = Math.max(...lines.map((l) => ctx.measureText(l).width)) + size * 0.9
    const h = lines.length * lh + size * 0.5

    ctx.fillStyle = c.box
    ctx.beginPath()
    ctx.roundRect((t.width - w) / 2, top - size * 0.25, w, h, size * 0.3)
    ctx.fill()
  }

  ctx.fillStyle = c.color

  for (const [i, l] of lines.entries()) {
    ctx.fillText(l, t.width / 2, top + lh * (i + 0.5))
  }

  ctx.restore()
}

/** Draws the timeline at `at` seconds on a canvas of the timeline's size. */
export function drawFrame(
  ctx: CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D,
  t: Timeline,
  at: number,
  source: FrameSource,
) {
  ctx.save()
  ctx.fillStyle = t.background
  ctx.fillRect(0, 0, t.width, t.height)

  const on = clipAt(t, at)

  if (on) {
    const { clip, index, local } = on
    const tl = transitionLength(t, index)
    const p = tl ? Math.min(1, local / tl) : 1
    const prev = t.clips[index - 1]
    const draw = (c: Clip, l: number) => {
      const img = source(c, l)

      if (img) {
        drawClip(ctx, t, c, img, Math.min(1, l / c.duration))
      }
    }

    if (p < 1 && prev) {
      // The clip before stays on its last moment while this one comes in.
      switch (clip.transition) {
        case 'slide':
          ctx.save()
          ctx.translate(-p * t.width, 0)
          draw(prev, prev.duration)
          ctx.restore()
          ctx.save()
          ctx.translate((1 - p) * t.width, 0)
          draw(clip, local)
          ctx.restore()
          break
        case 'zoom': {
          draw(prev, prev.duration)
          ctx.save()
          ctx.globalAlpha = p
          const s = 1.15 - 0.15 * p

          ctx.translate(t.width / 2, t.height / 2)
          ctx.scale(s, s)
          ctx.translate(-t.width / 2, -t.height / 2)
          draw(clip, local)
          ctx.restore()
          break
        }

        default:
          draw(prev, prev.duration)
          ctx.save()
          ctx.globalAlpha = p
          draw(clip, local)
          ctx.restore()
      }
    } else {
      draw(clip, local)
    }
  }

  for (const c of t.captions) {
    if (at >= c.start && at < c.end) {
      drawCaption(ctx, t, c, at)
    }
  }

  ctx.restore()
}
