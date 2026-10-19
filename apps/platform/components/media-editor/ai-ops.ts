// AI operations shared by the AI panel and Optimize: a request to the workspace's provider through the
// staqly server, fitted back to the picture.
import { api } from '@/lib/api'

import { aiSize, bitmapOf, cropTo, expandInputs, sizeOf } from './raster'

/** The picture extended to a ratio by the image model (the original kept in the middle), at most 4096 px. */
export async function aiExpand(blob: Blob, ratio: number, model: string, signal?: AbortSignal): Promise<Blob> {
  const size = aiSize(model, ratio * 1000, 1000)
  const inputs = await expandInputs(blob, ratio, size)
  const out = await api.aiImage(
    'edit',
    {
      prompt: 'Extend the scene into the empty area so it looks like one picture. Keep everything already there.',
      size,
      image: inputs.image,
      mask: inputs.mask,
    },
    signal,
  )
  // The model answers at the size asked for; if not, the frame scales with what came back.
  const got = await bitmapOf(out)
  const k = got.width / sizeOf(size).w

  got.close()

  const scale = Math.min(1, 4096 / Math.max(inputs.target.w, inputs.target.h))

  return cropTo(
    out,
    { x: inputs.frame.x * k, y: inputs.frame.y * k, w: inputs.frame.w * k, h: inputs.frame.h * k },
    { w: Math.round(inputs.target.w * scale), h: Math.round(inputs.target.h * scale) },
  )
}
