// The media editor's static files, copied before `next build` / `next dev` into public/ext (git-ignored),
// which the dashboard serves at /ext: the wasm codecs from node_modules, OpenGPEX's codec workers
// (lib/opengpex/core/files/workers) and the GIF libraries, bundled as scripts the GIF codec loads. Adapted from OpenGPEX's
// scripts/postinstall-exts.mjs (GPL-3.0-only); RAW (LibRaw) and PDF/EPS (Ghostscript) are left out.
import { copyFileSync, cpSync, existsSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { build } from 'esbuild'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const ext = join(root, 'public/ext')
const nm = (p) => join(root, 'node_modules', p)

const files = [
  ['@resvg/resvg-wasm/index_bg.wasm', 'wasm/resvg/resvg.wasm'],
  ['@resvg/resvg-wasm/index.mjs', 'wasm/resvg/index.js'],
  ['wasm-vips/lib/vips.js', 'wasm/vips/vips.js'],
  ['wasm-vips/lib/vips.wasm', 'wasm/vips/vips.wasm'],
  ['wasm-vips/lib/vips-heif.wasm', 'wasm/vips/vips-heif.wasm'],
  ['@jsquash/avif/codec/enc/avif_enc.wasm', 'wasm/avif/avif_enc.wasm'],
  ['@jsquash/avif/codec/enc/avif_enc.js', 'wasm/avif/avif_enc.js'],
  ['@jsquash/avif/codec/dec/avif_dec.wasm', 'wasm/avif/dec/avif_dec.wasm'],
  ['@jsquash/avif/codec/dec/avif_dec.js', 'wasm/avif/dec/avif_dec.js'],
  ['libheif-js/libheif-wasm/libheif.js', 'wasm/libheif/libheif.js'],
  ['libheif-js/libheif-wasm/libheif.wasm', 'wasm/libheif/libheif.wasm'],
]
const workers = [
  ['resvg-worker.js', 'wasm/resvg/resvg-worker.js'],
  ['vips-worker.js', 'wasm/vips/vips-worker.js'],
  ['avif-worker.js', 'wasm/avif/avif-worker.js'],
  ['heic-worker.js', 'wasm/libheif/heic-worker.js'],
]
// Loaded with a <script> tag as globals: bundled to IIFE.
const bundles = [
  ['gifuct-js/lib/index.js', 'js/gifuct-js.js', 'gifuctJs'],
  ['gifenc/dist/gifenc.esm.js', 'js/gifenc.js', 'gifenc'],
]

const place = (to) => {
  mkdirSync(dirname(join(ext, to)), { recursive: true })

  return join(ext, to)
}

rmSync(ext, { recursive: true, force: true })

for (const [from, to] of files) {
  copyFileSync(nm(from), place(to))
}

for (const [from, to] of workers) {
  copyFileSync(join(root, 'lib/opengpex/core/files/workers', from), place(to))
}

for (const [from, to, globalName] of bundles) {
  await build({
    entryPoints: [nm(from)],
    bundle: true,
    format: 'iife',
    globalName,
    minify: true,
    target: 'es2020',
    outfile: place(to),
    logLevel: 'warning',
  })
}

// OpenGPEX's mark, shown with its name, version and source link in the editor's menu.
cpSync(join(root, 'lib/opengpex/logo.svg'), place('opengpex-logo.svg'))

if (!existsSync(join(ext, 'wasm/vips/vips.wasm'))) {
  throw new Error('editor assets incomplete')
}

console.log(`editor assets: ${files.length + workers.length + bundles.length + 1} files in public/ext`)
