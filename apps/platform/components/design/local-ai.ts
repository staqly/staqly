// AI on the device, for when the server's AI is not there or this month's allowance is used up: removing an
// object (LaMa, Carve/LaMa-ONNX, Apache-2.0) and upscaling four times (Real-ESRGAN general x4v3, BSD-3-Clause).
// Both run with ONNX Runtime Web (pinned, from jsDelivr: the editors' CSP allows exactly this version) on the
// processor, WebAssembly; nothing is uploaded. The models download on first use and the browser keeps them.

const ORT = 'https://cdn.jsdelivr.net/npm/onnxruntime-web@1.27.0/dist/ort.min.mjs'
const MODELS = {
  // 512 x 512 in and out; the picture is fitted to it and only the painted area is put back.
  erase: 'https://huggingface.co/Carve/LaMa-ONNX/resolve/main/lama_fp32.onnx',
  // Any size in, four times the size out.
  upscale: 'https://huggingface.co/vlad-grig/realesr-general-x4v3-onnx/resolve/main/onnx/model.onnx',
}
const CACHE = 'staqly-design-models'
const LAMA = 512

/** Upscaling starts from at most this many pixels on the long side (four times that out: ~3000 px). */
export const UPSCALE_FROM = 768

// oxlint-disable-next-line typescript/no-explicit-any -- ONNX Runtime loads from the CDN, without types
type Ort = any

let ort: Promise<Ort> | null = null
const sessions = new Map<keyof typeof MODELS, Promise<Ort>>()

/** The model file, from the browser's cache or downloaded (with its progress) and kept there. */
async function modelBytes(url: string, onProgress?: (share: number) => void) {
  const cache = await caches.open(CACHE).catch(() => null)
  const hit = await cache?.match(url)

  if (hit) {
    return new Uint8Array(await hit.arrayBuffer())
  }

  const res = await fetch(url)

  if (!res.ok || !res.body) {
    throw new Error(`The model did not download (${res.status})`)
  }

  const total = Number(res.headers.get('content-length')) || 0
  const parts: Uint8Array[] = []
  let got = 0
  const reader = res.body.getReader()

  for (;;) {
    const { done, value } = await reader.read()

    if (done) {
      break
    }

    parts.push(value)
    got += value.length

    if (total) {
      onProgress?.(got / total)
    }
  }

  const bytes = new Uint8Array(got)
  let at = 0

  for (const p of parts) {
    bytes.set(p, at)
    at += p.length
  }

  await cache?.put(url, new Response(bytes)).catch(() => undefined)

  return bytes
}

function session(name: keyof typeof MODELS, onProgress?: (share: number) => void) {
  let s = sessions.get(name)

  if (!s) {
    s = (async () => {
      ort ??= import(/* webpackIgnore: true */ ORT)

      const rt = await ort

      return rt.InferenceSession.create(await modelBytes(MODELS[name], onProgress), { executionProviders: ['wasm'] })
    })().catch((e: unknown) => {
      sessions.delete(name)

      throw e
    })
    sessions.set(name, s)
  }

  return s
}

function canvas(w: number, h: number) {
  const c = new OffscreenCanvas(w, h)

  return { c, ctx: c.getContext('2d', { willReadFrequently: true })! }
}

/** RGBA pixels as the planar 0..1 floats the models take. */
function planar(data: Uint8ClampedArray, n: number) {
  const out = new Float32Array(3 * n)

  for (let i = 0; i < n; i++) {
    out[i] = data[i * 4]! / 255
    out[n + i] = data[i * 4 + 1]! / 255
    out[2 * n + i] = data[i * 4 + 2]! / 255
  }

  return out
}

/** Planar floats back to opaque RGBA (scale: 255 for 0..1 output, 1 when it is 0..255 already). */
function rgba(t: Float32Array, n: number, scale: number) {
  const px = new Uint8ClampedArray(n * 4)

  for (let i = 0; i < n; i++) {
    px[i * 4] = t[i]! * scale
    px[i * 4 + 1] = t[n + i]! * scale
    px[i * 4 + 2] = t[2 * n + i]! * scale
    px[i * 4 + 3] = 255
  }

  return px
}

/**
 * Removes what the mask covers (white: the area, like the AI tools' masks) and fills it in. LaMa works at 512 px:
 * its fill is scaled back and put only where the mask is (softened at the edge), so the rest keeps every pixel.
 */
export async function eraseOnDevice(picture: Blob, mask: Blob, onProgress?: (share: number) => void): Promise<Blob> {
  const rt = await (ort ??= import(/* webpackIgnore: true */ ORT))
  const s = await session('erase', onProgress)
  const [img, msk] = await Promise.all([createImageBitmap(picture), createImageBitmap(mask)])
  const { width: W, height: H } = img
  const small = canvas(LAMA, LAMA)

  small.ctx.drawImage(img, 0, 0, LAMA, LAMA)

  const n = LAMA * LAMA
  const holes = canvas(LAMA, LAMA)

  holes.ctx.drawImage(msk, 0, 0, LAMA, LAMA)

  const m = holes.ctx.getImageData(0, 0, LAMA, LAMA).data
  const maskData = new Float32Array(n)

  for (let i = 0; i < n; i++) {
    maskData[i] = (m[i * 4]! + m[i * 4 + 1]! + m[i * 4 + 2]!) / 3 > 127 && m[i * 4 + 3]! > 0 ? 1 : 0
  }

  const feeds: Record<string, unknown> = {}

  for (const name of s.inputNames as string[]) {
    feeds[name] = name.includes('mask')
      ? new rt.Tensor('float32', maskData, [1, 1, LAMA, LAMA])
      : new rt.Tensor('float32', planar(small.ctx.getImageData(0, 0, LAMA, LAMA).data, n), [1, 3, LAMA, LAMA])
  }

  const out = (await s.run(feeds))[s.outputNames[0]].data as Float32Array
  // This port answers 0..255; a 0..1 one would too after scaling.
  let hi = 0

  for (let i = 0; i < out.length; i += 101) {
    hi = Math.max(hi, out[i]!)
  }

  const filled = canvas(LAMA, LAMA)

  filled.ctx.putImageData(new ImageData(rgba(out, n, hi > 1.5 ? 1 : 255), LAMA, LAMA), 0, 0)

  // The fill at full size, kept only where the mask is (as alpha, a little soft at the edge).
  const layer = canvas(W, H)

  layer.ctx.drawImage(filled.c, 0, 0, W, H)

  const alpha = canvas(W, H)

  alpha.ctx.filter = `blur(${Math.max(1, Math.round(Math.max(W, H) / 400))}px)`
  alpha.ctx.drawImage(msk, 0, 0, W, H)

  const a = alpha.ctx.getImageData(0, 0, W, H)

  for (let i = 0; i < a.data.length; i += 4) {
    a.data[i + 3] = Math.round(((a.data[i]! + a.data[i + 1]! + a.data[i + 2]!) / 3) * (a.data[i + 3]! / 255))
  }

  alpha.ctx.filter = 'none'
  alpha.ctx.putImageData(a, 0, 0)
  layer.ctx.globalCompositeOperation = 'destination-in'
  layer.ctx.drawImage(alpha.c, 0, 0)

  const result = canvas(W, H)

  result.ctx.drawImage(img, 0, 0)
  result.ctx.drawImage(layer.c, 0, 0)
  img.close()
  msk.close()

  return result.c.convertToBlob({ type: 'image/png' })
}

/** Four times the size (from at most UPSCALE_FROM pixels on the long side), sharper than stretching. */
export async function upscaleOnDevice(picture: Blob, onProgress?: (share: number) => void): Promise<Blob> {
  const rt = await (ort ??= import(/* webpackIgnore: true */ ORT))
  const s = await session('upscale', onProgress)
  const img = await createImageBitmap(picture)
  const k = Math.min(1, UPSCALE_FROM / Math.max(img.width, img.height))
  const w = Math.max(1, Math.round(img.width * k))
  const h = Math.max(1, Math.round(img.height * k))
  const src = canvas(w, h)

  src.ctx.drawImage(img, 0, 0, w, h)
  img.close()

  const out = (
    await s.run({
      [s.inputNames[0]]: new rt.Tensor('float32', planar(src.ctx.getImageData(0, 0, w, h).data, w * h), [1, 3, h, w]),
    })
  )[s.outputNames[0]]
  const [, , H, W] = out.dims as number[]
  const result = canvas(W!, H!)

  result.ctx.putImageData(new ImageData(rgba(out.data as Float32Array, W! * H!, 255), W!, H!), 0, 0)

  return result.c.convertToBlob({ type: 'image/png' })
}
