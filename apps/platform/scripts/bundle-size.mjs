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

// The editors (/media/edit: the design editor with Konva, its fonts' CSS, templates; /media/video and /media/audio:
// the timeline with Mediabunny) and what only they load have a budget of their own. A chunk is the editor's when its page or lazy-load manifest reaches it (directly, or through chunks
// that name it, like dynamic imports do) and no other page does.
const EDITORS = ['/media/edit', '/media/video', '/media/audio']
const isEditor = (route) => EDITORS.includes(route)
const text = new Map(all.map((a) => [a, readFileSync(join(out, a), 'utf8')]))
const names = new Map(all.map((a) => [a.split('/').pop(), a]))
// The design editor also opens in a drawer over a post (components/design/design-drawer.tsx), so dashboard pages
// lazy-load it too. That load is still the editor's: the chunk with the editor's own code holds its drafts' key
// prefix (a string minifying keeps), and the list of chunks a lazy load fetches together (`Promise.all([...])`)
// that names it is skipped when following what the dashboard loads, as is a lazy-load entry that includes it.
const EDITOR_MARKERS = ['staqly-design-draft:']
const marked = all.filter((a) => EDITOR_MARKERS.some((m) => text.get(a).includes(m))).map((a) => a.split('/').pop())
const withoutEditor = (t) => t.replace(/\[[^[\]]*\]/g, (list) => (marked.some((n) => list.includes(n)) ? '' : list))
const refsOf = (strip) =>
  new Map(
    all.map((a) => {
      const t = strip ? withoutEditor(text.get(a)) : text.get(a)

      return [a, [...names.keys()].filter((n) => n !== a.split('/').pop() && t.includes(n)).map((n) => names.get(n))]
    }),
  )
const refs = refsOf(false)
const dashboardRefs = refsOf(true)
const reach = (roots, edges = refs) => {
  const seen = new Set(roots)
  const todo = [...roots]

  while (todo.length) {
    for (const r of edges.get(todo.pop()) ?? []) {
      if (!seen.has(r)) {
        seen.add(r)
        todo.push(r)
      }
    }
  }

  return seen
}
const lazy = (editor) =>
  walk(join(root, '.next/server/app'))
    .filter((p) => p.endsWith('react-loadable-manifest.json'))
    .flatMap((p) =>
      Object.values(JSON.parse(readFileSync(p, 'utf8'))).flatMap((v) => {
        const files = (v.files ?? []).filter((f) => f.endsWith('.js'))
        const isEditorLoad =
          EDITORS.some((e) => p.includes(`${e}/`)) || files.some((f) => marked.includes(f.split('/').pop()))

        return isEditorLoad === editor ? files.map((f) => `_next/${f}`) : []
      }),
    )
const pageFiles = (editor) => rows.filter((r) => isEditor(r.route) === editor).flatMap((r) => r.files)
const dashboard = reach([...pageFiles(false), ...lazy(false)], dashboardRefs)
const editorSide = reach([...pageFiles(true), ...lazy(true)])
const gzOf = (chunks) => chunks.reduce((n, a) => n + size(a).gz, 0)
// Each page that opens the drawer loads the editor's own chunks plus a grouping of the shared modules that page
// lacks (the checkbox, picture downscaling...): those groupings count with the dashboard, like any other split of
// shared chunks. Printed per load for reference.
const drawerLoads = [
  ...new Set(
    all
      .flatMap((a) => [...text.get(a).matchAll(/\[[^[\]]*\]/g)].map((m) => m[0]))
      .filter((list) => marked.some((n) => list.includes(n))),
  ),
].map((list) => gzOf([...names.keys()].filter((n) => list.includes(n) && n.endsWith('.js')).map((n) => names.get(n))))
const editorOnly = [...editorSide].filter((a) => !dashboard.has(a) && refs.has(a))
const editorGz = gzOf(editorOnly)
const totalGz = gzOf(all) - editorGz
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
  console.log(`editors only: ${editorOnly.length} files, ${kb(editorGz)} KB gzipped`)
  console.log(
    `each load of the design editor (its page, the drawer on each page): ${drawerLoads.map(kb).join(', ')} KB gzipped`,
  )
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
    failures.push(`editors JS ${kb(editorGz)} KB gz > budget ${budget.editorJsGzKB} KB`)
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
