#!/usr/bin/env node
// Copies each page's Markdown (rendered by the /llms.mdx route) to /<page>.md
// next to its HTML, so LLMs and people can append ".md" to any docs URL
// (index → /index.md). Runs after `next build`; a static export has no rewrites.
import { cp, readdir, stat } from 'node:fs/promises'
import { dirname, join, relative } from 'node:path'

const out = join(import.meta.dirname, '..', 'out')
const src = join(out, 'llms.mdx')
let n = 0

async function walk(dir) {
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const p = join(dir, e.name)

    if (e.isDirectory()) {
      await walk(p)
    } else if (e.name === 'content.md') {
      const slug = relative(src, dirname(p)) || 'index'

      await cp(p, join(out, `${slug}.md`))
      n++
    }
  }
}

await stat(src).catch(() => {
  console.error('out/llms.mdx not found — run `next build` first')
  process.exit(1)
})
await walk(src)
console.log(`✓ markdown: ${n} pages copied to /<page>.md`)
