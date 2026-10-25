// Picture plumbing for the AI tools, on canvases in the browser: the AI provider gets a PNG of a sensible size.

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

  return c.convertToBlob({ type: 'image/png' })
}
