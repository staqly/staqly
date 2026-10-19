// Picture plumbing for the editor's AI and channel tools, on canvases in the browser: the AI provider gets a
// PNG of a sensible size, and what comes back is fitted to the picture again.
import { padRect, type Rect } from './fit'

export async function bitmapOf(blob: Blob): Promise<ImageBitmap> {
  return createImageBitmap(blob)
}

function canvas(w: number, h: number) {
  const c = new OffscreenCanvas(Math.max(1, Math.round(w)), Math.max(1, Math.round(h)))
  const ctx = c.getContext('2d')

  if (!ctx) {
    throw new Error('This browser cannot draw pictures here')
  }

  return { c, ctx }
}

const png = (c: OffscreenCanvas) => c.convertToBlob({ type: 'image/png' })

/** The image model's size closest in shape to w x h (gpt-image: square, landscape or portrait). */
export function aiSize(model: string, w: number, h: number): string {
  const r = w / h

  if (model.startsWith('gpt-image')) {
    return r > 1.2 ? '1536x1024' : r < 0.83 ? '1024x1536' : '1024x1024'
  }

  if (model === 'dall-e-3') {
    return r > 1.2 ? '1792x1024' : r < 0.83 ? '1024x1792' : '1024x1024'
  }

  return '1024x1024'
}

export const sizeOf = (s: string) => {
  const [w, h] = s.split('x').map(Number)

  return { w: w || 1024, h: h || 1024 }
}

/** A copy at most maxEdge pixels on its long side (AI requests stay small and fast). */
export async function downscale(blob: Blob, maxEdge = 2048): Promise<Blob> {
  const bmp = await bitmapOf(blob)
  const s = Math.min(1, maxEdge / Math.max(bmp.width, bmp.height))

  if (s === 1 && blob.type === 'image/png') {
    bmp.close()

    return blob
  }

  const { c, ctx } = canvas(bmp.width * s, bmp.height * s)

  ctx.drawImage(bmp, 0, 0, c.width, c.height)
  bmp.close()

  return png(c)
}

/** Fills w x h with the picture (cropping its edges when the shapes differ), as a PNG. */
export async function cover(blob: Blob, w: number, h: number): Promise<Blob> {
  const bmp = await bitmapOf(blob)
  const s = Math.max(w / bmp.width, h / bmp.height)
  const { c, ctx } = canvas(w, h)

  ctx.drawImage(bmp, (w - bmp.width * s) / 2, (h - bmp.height * s) / 2, bmp.width * s, bmp.height * s)
  bmp.close()

  return png(c)
}

/** The rect of the picture as a new file, at most maxEdge on its long side. */
export async function cropTo(blob: Blob, rect: Rect, out: { w: number; h: number }, type = 'image/png') {
  const bmp = await bitmapOf(blob)
  const { c, ctx } = canvas(out.w, out.h)

  ctx.drawImage(bmp, rect.x, rect.y, rect.w, rect.h, 0, 0, c.width, c.height)
  bmp.close()

  return c.convertToBlob({ type, quality: 0.92 })
}

export type PadFill = { kind: 'color'; color: string } | { kind: 'transparent' } | { kind: 'blur' }

/** The whole picture on a canvas of the target ratio, the rest filled with a colour, nothing, or itself blurred. */
export async function padTo(blob: Blob, ratio: number, fill: PadFill, maxEdge = 4096): Promise<Blob> {
  const bmp = await bitmapOf(blob)
  const { canvas: size, at } = padRect(bmp.width, bmp.height, ratio)
  const s = Math.min(1, maxEdge / Math.max(size.w, size.h))
  const { c, ctx } = canvas(size.w * s, size.h * s)

  if (fill.kind === 'color') {
    ctx.fillStyle = fill.color
    ctx.fillRect(0, 0, c.width, c.height)
  } else if (fill.kind === 'blur') {
    const k = Math.max(c.width / bmp.width, c.height / bmp.height)

    ctx.filter = `blur(${Math.round(Math.max(c.width, c.height) / 40)}px) brightness(0.9)`
    ctx.drawImage(bmp, (c.width - bmp.width * k) / 2, (c.height - bmp.height * k) / 2, bmp.width * k, bmp.height * k)
    ctx.filter = 'none'
  }

  ctx.drawImage(bmp, at.x * s, at.y * s, at.w * s, at.h * s)
  bmp.close()

  return png(c)
}

/**
 * For AI expand: the picture centred on a canvas of the target ratio in the model's size, transparent around
 * it, and the mask (transparent where the model may paint, i.e. everywhere but the picture).
 */
export async function expandInputs(blob: Blob, ratio: number, size: string) {
  const bmp = await bitmapOf(blob)
  const out = sizeOf(size)
  const { canvas: padded, at } = padRect(bmp.width, bmp.height, ratio)
  const s = Math.min(out.w / padded.w, out.h / padded.h)
  const w = padded.w * s
  const h = padded.h * s
  const x = (out.w - w) / 2 + at.x * s
  const y = (out.h - h) / 2 + at.y * s
  const image = canvas(out.w, out.h)

  image.ctx.drawImage(bmp, x, y, at.w * s, at.h * s)
  bmp.close()

  const mask = canvas(out.w, out.h)

  mask.ctx.fillStyle = '#000'
  mask.ctx.fillRect(x, y, at.w * s, at.h * s)

  return {
    image: await png(image.c),
    mask: await png(mask.c),
    // Where the target shape sits in the model's output, to cut it out of the result.
    frame: { x: (out.w - w) / 2, y: (out.h - h) / 2, w, h },
    target: padded,
  }
}

/** A mask from painted strokes: transparent where painted (what may change), opaque elsewhere. */
export async function maskFrom(painted: HTMLCanvasElement, w: number, h: number): Promise<Blob> {
  const { c, ctx } = canvas(w, h)

  ctx.fillStyle = '#000'
  ctx.fillRect(0, 0, w, h)
  ctx.globalCompositeOperation = 'destination-out'
  ctx.drawImage(painted, 0, 0, w, h)

  return png(c)
}

/** Facts the channel check needs: size, bytes in the format it would be saved in, see-through pixels, and where
 * the detail is (from a small copy). */
export async function pictureFacts(blob: Blob) {
  const bmp = await bitmapOf(blob)
  const s = Math.min(1, 256 / Math.max(bmp.width, bmp.height))
  const { c, ctx } = canvas(bmp.width * s, bmp.height * s)

  ctx.drawImage(bmp, 0, 0, c.width, c.height)
  const data = ctx.getImageData(0, 0, c.width, c.height).data
  let alpha = false

  for (let i = 3; i < data.length; i += 4) {
    if (data[i]! < 250) {
      alpha = true
      break
    }
  }

  const facts = { width: bmp.width, height: bmp.height, alpha, small: { data, w: c.width, h: c.height } }

  bmp.close()

  return facts
}

/** Encodes as the platform takes it: JPEG when it takes nothing else (lower quality until it fits maxBytes). */
export async function encodeFor(blob: Blob, formats: string[] | undefined, maxBytes: number | undefined) {
  const jpegOnly = !!formats?.length && formats.every((f) => f === 'image/jpeg')

  if (!jpegOnly && (!maxBytes || blob.size <= maxBytes)) {
    return blob
  }

  const bmp = await bitmapOf(blob)
  const { c, ctx } = canvas(bmp.width, bmp.height)

  ctx.fillStyle = '#fff' // JPEG has no see-through pixels: white like the platforms show them
  ctx.fillRect(0, 0, c.width, c.height)
  ctx.drawImage(bmp, 0, 0)
  bmp.close()

  for (const quality of [0.92, 0.85, 0.75, 0.6]) {
    const out = await c.convertToBlob({ type: 'image/jpeg', quality })

    if (!maxBytes || out.size <= maxBytes) {
      return out
    }
  }

  return c.convertToBlob({ type: 'image/jpeg', quality: 0.5 })
}
