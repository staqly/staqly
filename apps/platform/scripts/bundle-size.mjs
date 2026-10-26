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
    files: js,
    scripts: js.length,
    jsKB: kb(sum(js, 'raw')),
    jsGzKB: kb(sum(js, 'gz')),
    cssGzKB: kb(sum(css, 'gz')),
  }
})

const all = walk(join(out, '_next/static'))
  .filter((p) => p.endsWith('.js'))
  .map((p) => relative(out, p))

// The design editor (/media/design: Konva, its fonts' CSS, templates) and what only it loads have a budget of
// their own. A chunk is the editor's when its page or lazy-load manifest reaches it (directly, or through chunks
// that name it, like dynamic imports do) and no other page does.
const EDITORS = ['/media/design']
const isEditor = (route) => EDITORS.includes(route)
const lazy = (editor) =>
  walk(join(root, '.next/server/app'))
    .filter((p) => p.endsWith('react-loadable-manifest.json') && EDITORS.some((e) => p.includes(`${e}/`)) === editor)
    .flatMap((p) => Object.values(JSON.parse(readFileSync(p, 'utf8'))).flatMap((v) => v.files ?? []))
    .filter((f) => f.endsWith('.js'))
    .map((f) => `_next/${f}`)
const names = new Map(all.map((a) => [a.split('/').pop(), a]))
const refs = new Map(
  all.map((a) => {
    const text = readFileSync(join(out, a), 'utf8')

    return [a, [...names.keys()].filter((n) => n !== a.split('/').pop() && text.includes(n)).map((n) => names.get(n))]
  }),
)
const reach = (roots) => {
  const seen = new Set(roots)
  const todo = [...roots]

  while (todo.length) {
    for (const r of refs.get(todo.pop()) ?? []) {
      if (!seen.has(r)) {
        seen.add(r)
        todo.push(r)
      }
    }
  }

  return seen
}
const pageFiles = (editor) => rows.filter((r) => isEditor(r.route) === editor).flatMap((r) => r.files)
const dashboard = reach([...pageFiles(false), ...lazy(false)])
const editorOnly = [...reach([...pageFiles(true), ...lazy(true)])].filter((a) => !dashboard.has(a) && refs.has(a))
const editorGz = editorOnly.reduce((n, a) => n + size(a).gz, 0)
const totalGz = all.reduce((n, a) => n + size(a).gz, 0) - editorGz
const largest = all
  .map((a) => ({ chunk: a.replace('_next/static/chunks/', ''), KB: kb(size(a).raw), gzKB: kb(size(a).gz) }))
  .toSorted((a, b) => b.KB - a.KB)
  .slice(0, 10)

if (args.has('--json')) {
  console.log(
    JSON.stringify(
      { rows: rows.map(({ files: _f, ...r }) => r), largest, totalJsGzKB: kb(totalGz), editorJsGzKB: kb(editorGz) },
      null,
      2,
    ),
  )
} else {
  console.table(rows.map(({ files: _f, ...r }) => r))
  console.table(largest)
  console.log(`all JS chunks: ${all.length - editorOnly.length} files, ${kb(totalGz)} KB gzipped`)
  console.log(`design editor only: ${editorOnly.length} files, ${kb(editorGz)} KB gzipped`)
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

  if (kb(editorGz) > budget.editorJsGzKB) {
    failures.push(`design editor JS ${kb(editorGz)} KB gz > budget ${budget.editorJsGzKB} KB`)
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
