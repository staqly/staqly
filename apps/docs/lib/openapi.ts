import { readFileSync } from 'node:fs'
import path from 'node:path'

import type { Document } from 'fumadocs-openapi'
import { createOpenAPI } from 'fumadocs-openapi/server'
import { parse } from 'yaml'

import { env } from './env.mjs'

// The REST contract lives in the API app; the reference is generated from it
// at build time. OPENAPI_SPEC overrides the path (the Dockerfile sets it to the
// spec copied from the `api` build context).
export const specPath = env.openapiSpec ?? path.resolve(process.cwd(), '../api/openapi.yaml')

/**
 * Pages are generated per tag. Tags used by operations but missing from the
 * top-level `tags` list would otherwise get no page, so declare them here.
 */
function loadSpec(): Document {
  const doc = parse(readFileSync(specPath, 'utf8')) as {
    tags?: { name: string }[]
    paths?: Record<string, Record<string, unknown>>
  }
  const declared = new Set((doc.tags ?? []).map((t) => t.name))
  const used = Object.values(doc.paths ?? {}).flatMap((item) =>
    Object.values(item ?? {}).flatMap((op) =>
      op && typeof op === 'object' && Array.isArray((op as { tags?: unknown }).tags)
        ? (op as { tags: string[] }).tags
        : [],
    ),
  )
  const missing = [...new Set(used)].filter((t) => !declared.has(t))

  doc.tags = [...(doc.tags ?? []), ...missing.map((name) => ({ name }))]

  return doc as unknown as Document
}

export const openapi = createOpenAPI({
  input: { staqly: loadSpec },
})
