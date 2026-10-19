// Per-route first-load JS/CSS of the static export (out/): every <script src>
// and stylesheet an exported page loads before it is interactive, raw and
// gzipped. `--json` prints JSON; `--check` fails when a route or the whole
// export goes over its budget (bundle-budget.json). Run after `pnpm build`.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { gzipSync } from 'node:zlib'

const root = new URL('..', import.meta.url).pathname
const out = join(root, 'out')
const args = new Set(process.argv.slice(2))

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name)

    return statSync(p).isDirectory() ? walk(p) : [p]
  })
}

const sizes = new Map()

function size(asset) {
  if (!sizes.has(asset)) {
    const buf = readFileSync(join(out, asset))

    sizes.set(asset, { raw: buf.length, gz: gzipSync(buf, { level: 9 }).length })
  }

  return sizes.get(asset)
}

const kb = (n) => +(n / 1024).toFixed(1)
const sum = (list, k) => list.reduce((n, a) => n + size(a)[k], 0)

const pages = walk(out)
  .filter((p) => p.endsWith('.html') && !p.includes('/_next/'))
  .map((p) => relative(out, p))
  .filter((p) => !['404.html', '_not-found.html'].includes(p))
  .toSorted()

const rows = pages.map((page) => {
  const html = readFileSync(join(out, page), 'utf8')
  const js = [...new Set([...html.matchAll(/<script[^>]*\ssrc="\/(_next\/static\/[^"]+\.js)"/g)].map((m) => m[1]))]
  const css = [...new Set([...html.matchAll(/<link[^>]*\shref="\/(_next\/static\/[^"]+\.css)"/g)].map((m) => m[1]))]
  const route = '/' + page.replace(/\.html$/, '').replace(/(^|\/)index$/, '')

  return {
    route: route.length > 1 ? route.replace(/\/$/, '') : '/',
    scripts: js.length,
    jsKB: kb(sum(js, 'raw')),
    jsGzKB: kb(sum(js, 'gz')),
    cssGzKB: kb(sum(css, 'gz')),
  }
})

const all = walk(join(out, '_next/static'))
  .filter((p) => p.endsWith('.js'))
  .map((p) => relative(out, p))
const totalGz = all.reduce((n, a) => n + size(a).gz, 0)
const largest = all
  .map((a) => ({ chunk: a.replace('_next/static/chunks/', ''), KB: kb(size(a).raw), gzKB: kb(size(a).gz) }))
  .toSorted((a, b) => b.KB - a.KB)
  .slice(0, 10)

if (args.has('--json')) {
  console.log(JSON.stringify({ rows, largest, totalJsGzKB: kb(totalGz) }, null, 2))
} else {
  console.table(rows)
  console.table(largest)
  console.log(`all JS chunks: ${all.length} files, ${kb(totalGz)} KB gzipped`)
}

if (args.has('--check')) {
  const budget = JSON.parse(readFileSync(join(root, 'bundle-budget.json'), 'utf8'))
  const failures = rows.flatMap((r) => {
    const limit = budget.routes[r.route] ?? budget.defaultRouteJsGzKB
    const errs = []

    if (r.jsGzKB > limit) {
      errs.push(`${r.route}: first-load JS ${r.jsGzKB} KB gz > budget ${limit} KB`)
    }

    if (r.cssGzKB > budget.cssGzKB) {
      errs.push(`${r.route}: CSS ${r.cssGzKB} KB gz > budget ${budget.cssGzKB} KB`)
    }

    return errs
  })

  if (kb(totalGz) > budget.totalJsGzKB) {
    failures.push(`all JS ${kb(totalGz)} KB gz > budget ${budget.totalJsGzKB} KB`)
  }

  if (rows.length === 0) {
    failures.push('no exported pages found in out/: run `pnpm build` first')
  }

  if (failures.length > 0) {
    console.error(`\nBundle budget exceeded (apps/platform/bundle-budget.json):\n  ${failures.join('\n  ')}`)
    process.exit(1)
  }

  console.log('bundle budget ok')
}
