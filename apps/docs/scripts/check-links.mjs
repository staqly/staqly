#!/usr/bin/env node
// Verifies every internal link in the static export (out/): the target page or
// asset must exist, and a #fragment must match an id on the target page.
// Runs after `next build`; exits non-zero on any broken link.
import { readdir, readFile, stat } from 'node:fs/promises'
import { join, relative } from 'node:path'

const root = join(import.meta.dirname, '..', 'out')
// Built with DOCS_BASE_PATH (e.g. /staqly), every root-relative link must carry it.
const basePath = process.env.DOCS_BASE_PATH || ''

async function walk(dir) {
  const out = []

  for (const e of await readdir(dir, { withFileTypes: true })) {
    const p = join(dir, e.name)

    if (e.isDirectory()) {
      out.push(...(await walk(p)))
    } else {
      out.push(p)
    }
  }

  return out
}

async function exists(p) {
  try {
    return (await stat(p)).isFile()
  } catch {
    return false
  }
}

async function resolvePage(pathname) {
  const base = join(root, decodeURIComponent(pathname))

  for (const c of [base, `${base}.html`, join(base, 'index.html')]) {
    if (await exists(c)) {
      return c
    }
  }

  return null
}

const files = await walk(root).catch(() => {
  console.error('out/ not found — run `next build` first')
  process.exit(1)
})
const html = files.filter((f) => f.endsWith('.html'))
const idCache = new Map()

async function ids(file) {
  if (!idCache.has(file)) {
    const src = await readFile(file, 'utf8')

    idCache.set(file, new Set([...src.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1])))
  }

  return idCache.get(file)
}

const broken = []
let checked = 0

for (const page of html) {
  const src = await readFile(page, 'utf8')
  // Only real anchors/assets in markup, not strings inside the RSC payload.
  const markup = src.replace(/<script[\s\S]*?<\/script>/g, '')

  for (const m of markup.matchAll(/\s(?:href|src)="([^"]*)"/g)) {
    const raw = m[1].replaceAll('&amp;', '&')

    if (!raw || /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(raw)) {
      continue
    } // external, mailto:, data:

    const u = new URL(raw, `http://x${basePath}/${relative(root, page).replace(/(index)?\.html$/, '')}`)
    const inBase = u.pathname === basePath || u.pathname.startsWith(`${basePath}/`)
    const target = raw.startsWith('#') ? page : inBase ? await resolvePage(u.pathname.slice(basePath.length)) : null

    checked++

    if (!target) {
      broken.push(`${relative(root, page)} → ${raw} (missing page)`)
      continue
    }

    const frag = decodeURIComponent(u.hash.slice(1))

    if (frag && target.endsWith('.html') && !(await ids(target)).has(frag)) {
      broken.push(`${relative(root, page)} → ${raw} (missing #${frag})`)
    }
  }
}

if (broken.length) {
  console.error(`✗ ${broken.length} broken internal link(s):\n  ${[...new Set(broken)].join('\n  ')}`)
  process.exit(1)
}

console.log(`✓ link check: ${checked} internal links across ${html.length} pages, none broken`)
