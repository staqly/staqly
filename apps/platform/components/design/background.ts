// Background removal on the device: transformers.js (pinned, from jsDelivr: the design editor's CSP in
// apps/api/internal/web/web.go allows exactly this version) runs InSPyReNet (MIT) in the browser. Nothing is
// uploaded; the first use downloads the model (~200 MB on a graphics card, ~400 MB without: the half-precision
// model answers NaN on WebAssembly), which the browser keeps for next time.

export const TRANSFORMERS_CDN = 'https://cdn.jsdelivr.net/npm/@huggingface/transformers@4.2.0'
const MODEL = 'OS-Software/InSPyReNet-SwinB-Plus-Ultra-ONNX'

// oxlint-disable-next-line typescript/no-explicit-any -- transformers.js loads from the CDN, without types
type Tf = any

/**
 * Whether the device draws with a graphics card, read from WebGL's renderer name. Not by asking WebGPU for an
 * adapter: holding one (a software one especially) stalled ONNX Runtime's WebAssembly session after it.
 */
function realGpu() {
  if (!('gpu' in navigator)) {
    return false
  }

  const gl = document.createElement('canvas').getContext('webgl')
  const ext = gl?.getExtension('WEBGL_debug_renderer_info')
  const renderer = ext ? String(gl!.getParameter(ext.UNMASKED_RENDERER_WEBGL)) : ''

  gl?.getExtension('WEBGL_lose_context')?.loseContext()

  return !!renderer && !/swiftshader|llvmpipe|softpipe|software|basic render|microsoft basic/i.test(renderer)
}

let loading: Promise<{ tf: Tf; model: Tf; processor: Tf }> | null = null

function load(onProgress?: (share: number) => void) {
  const progress_callback = (p: { status: string; loaded?: number; total?: number }) => {
    if (p.status === 'progress' && p.total) {
      onProgress?.((p.loaded ?? 0) / p.total)
    }
  }

  loading ??= (async () => {
    const tf: Tf = await import(/* webpackIgnore: true */ TRANSFORMERS_CDN)

    tf.env.cacheKey = 'staqly-design-models'
    tf.env.logLevel = 'error'

    const processor = await tf.AutoProcessor.from_pretrained(MODEL)
    const open = (device: 'webgpu' | 'wasm') =>
      tf.AutoModel.from_pretrained(MODEL, { device, dtype: device === 'webgpu' ? 'fp16' : 'fp32', progress_callback })
    // WebGPU on a real graphics card (seconds), WebAssembly otherwise (slower, same result). A software adapter
    // (no GPU, a VM, a blocklisted driver) runs a model like this for many minutes: WebAssembly is faster there.
    const model = realGpu() ? await open('webgpu').catch(() => open('wasm')) : await open('wasm')

    return { tf, model, processor }
  })().catch((e: unknown) => {
    loading = null

    throw e
  })

  return loading
}

/** The picture with its background see-through, as a PNG the size of the original. */
export async function removeBackground(blob: Blob, onProgress?: (share: number) => void): Promise<Blob> {
  const { tf, model, processor } = await load(onProgress)
  const bmp = await createImageBitmap(blob)
  const { width: w, height: h } = bmp
  const src = new OffscreenCanvas(w, h)
  const ctx = src.getContext('2d')!

  ctx.drawImage(bmp, 0, 0)
  bmp.close()

  const image = new tf.RawImage(ctx.getImageData(0, 0, w, h).data, w, h, 4)
  const { pixel_values } = await processor(image)
  const names: string[] = model.sessions?.model?.inputNames ?? model.session?.inputNames ?? ['input']
  const out = await model(Object.fromEntries(names.map((n) => [n, pixel_values])))
  const tensor = Object.values(out).find((t): t is Tf => !!(t as Tf)?.dims)

  if (!tensor) {
    throw new Error('The model gave no mask')
  }

  // The mask is the last two dimensions; models answer either probabilities (0..1) or logits.
  const mh: number = tensor.dims.at(-2)
  const mw: number = tensor.dims.at(-1)
  const data = tensor.data as Float32Array
  const off = data.length - mw * mh
  let lo = Infinity
  let hi = -Infinity

  for (let i = 0; i < Math.min(1000, mw * mh); i++) {
    lo = Math.min(lo, data[off + i]!)
    hi = Math.max(hi, data[off + i]!)
  }

  if (!Number.isFinite(lo) || !Number.isFinite(hi)) {
    throw new Error('The model gave no usable mask on this device')
  }

  const logits = lo < -0.5 || hi > 1.5
  const mask = new ImageData(mw, mh)

  for (let i = 0; i < mw * mh; i++) {
    const v = data[off + i]!
    const a = logits ? 1 / (1 + Math.exp(-v)) : v

    mask.data[i * 4 + 3] = Math.round(Math.min(1, Math.max(0, a)) * 255)
  }

  const m = new OffscreenCanvas(mw, mh)

  m.getContext('2d')!.putImageData(mask, 0, 0)
  // The mask, stretched to the picture, keeps only the pixels it covers.
  ctx.globalCompositeOperation = 'destination-in'
  ctx.drawImage(m, 0, 0, w, h)

  return src.convertToBlob({ type: 'image/png' })
}
