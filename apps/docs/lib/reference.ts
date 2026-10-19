import { loadSpec, openapi } from './openapi'

/*
 * The API reference: one page per operation (/api/reference/<tag>/<operation>), in a sidebar folder per tag whose
 * index (/api/reference/<tag>) lists the tag's operations. A tag page holding all of its operations was up to 2 MB of
 * HTML and ~19k elements; an operation page ships one operation and the components it references (lib/prune-spec.ts).
 */

type Json = Record<string, unknown>

const METHODS = ['get', 'put', 'post', 'delete', 'options', 'head', 'patch', 'trace', 'query'] as const

type HttpMethod = (typeof METHODS)[number]

export interface ReferenceOperation {
  tag: string
  method: HttpMethod
  path: string
  operationId: string
  /** URL segment: the operationId in kebab case (createPost -> create-post, getXPosts -> get-x-posts). */
  slug: string
  url: string
  /** The summary up to its first clause, for the sidebar and the page heading ("Create a post"). */
  title: string
  /** The whole summary, with the scopes it needs ("Create a post (posts:write; ...)"). */
  summary: string
  description?: string
  deprecated: boolean
  /** The heading id the operation had on its tag page (/api/reference/<tag>#<anchor>), kept on the tag's list. */
  anchor: string
}

export interface ReferenceTag {
  name: string
  slug: string
  url: string
  title: string
  description?: string
  operations: ReferenceOperation[]
}

const BASE = '/api/reference'

const kebab = (id: string) =>
  id
    .replace(/([a-z0-9])([A-Z])/g, '$1-$2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1-$2')
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
    .toLowerCase()

// github-slugger's slug(), which the library uses for heading ids: lower case, punctuation dropped, spaces to "-".
const githubSlug = (text: string) =>
  text
    .toLowerCase()
    .replace(/[^\p{L}\p{M}\p{N}\p{Pc} -]/gu, '')
    .replaceAll(' ', '-')

// The library's default file names for tags: whitespace to "-", lower case.
const tagSlug = (name: string) => name.replace(/\s+/g, '-').toLowerCase()

// Same as fumadocs-openapi's getTagDisplayName (x-displayName, summary, else the name in title case).
const tagTitle = (tag: Json & { name: string }) =>
  typeof tag['x-displayName'] === 'string'
    ? tag['x-displayName']
    : typeof tag.summary === 'string'
      ? tag.summary
      : tag.name.charAt(0).toUpperCase() + tag.name.slice(1)

// "Create a post (posts:write; ...)" -> "Create a post"; "Changing ..., step 1 - send a code" stays whole.
const shortTitle = (summary: string) =>
  summary
    .split(/ \(|; |\. /)[0]
    .replace(/\.$/, '')
    .trim()

function buildModel(): ReferenceTag[] {
  const doc = loadSpec() as unknown as { tags?: (Json & { name: string })[]; paths?: Record<string, Json> }
  const tags = new Map<string, ReferenceTag>()
  const slugs = new Set<string>()

  for (const tag of doc.tags ?? []) {
    const slug = tagSlug(tag.name)

    tags.set(tag.name, {
      name: tag.name,
      slug,
      url: `${BASE}/${slug}`,
      title: tagTitle(tag),
      description: typeof tag.description === 'string' ? tag.description.trim() : undefined,
      operations: [],
    })
  }

  for (const [path, item] of Object.entries(doc.paths ?? {})) {
    for (const method of METHODS) {
      const op = item?.[method] as
        | { operationId?: string; summary?: string; description?: string; tags?: string[]; deprecated?: boolean }
        | undefined

      if (!op) {
        continue
      }

      const where = `${method.toUpperCase()} ${path}`
      const tag = tags.get(op.tags?.[0] ?? '')

      if (!op.operationId || !tag) {
        throw new Error(`API reference: ${where} needs an operationId and a tag (apps/api/openapi.yaml)`)
      }

      const slug = kebab(op.operationId)

      if (slugs.has(slug)) {
        throw new Error(`API reference: two operations would be at .../${slug} (${where})`)
      }

      slugs.add(slug)

      const summary = (op.summary ?? op.operationId).trim()

      tag.operations.push({
        tag: tag.name,
        method,
        path,
        operationId: op.operationId,
        slug,
        url: `${tag.url}/${slug}`,
        title: shortTitle(summary),
        summary,
        description: op.description?.trim() || undefined,
        deprecated: op.deprecated === true,
        anchor: githubSlug(summary),
      })
    }
  }

  return [...tags.values()]
}

export const referenceTags = buildModel()

const byUrl = new Map<string, ReferenceTag | ReferenceOperation>()

for (const tag of referenceTags) {
  byUrl.set(tag.url, tag)

  for (const op of tag.operations) {
    byUrl.set(op.url, op)
  }
}

export function referenceEntry(url: string) {
  return byUrl.get(url)
}

const byEndpoint = new Map(referenceTags.flatMap((t) => t.operations.map((op) => [`${op.method} ${op.path}`, op])))

/** The operation page of an endpoint (`POST`, `/v1/posts`), if the spec has it. */
export function operationFor(method: string, path: string) {
  return byEndpoint.get(`${method.toLowerCase()} ${path}`)
}

/** <title> of an operation page: "Create a post (POST /v1/posts)", unique because method + path is. */
export function operationPageTitle(op: ReferenceOperation) {
  return `${op.title} (${op.method.toUpperCase()} ${op.path})`
}

/** Meta description of an operation page: the summary, then the description's first sentence when there is room. */
export function operationPageDescription(op: ReferenceOperation) {
  const head = `${op.method.toUpperCase()} ${op.path}: ${op.summary.replace(/\.?$/, '.')}`
  const first = op.description
    ?.replace(/\s+/g, ' ')
    .match(/^.*?[.!?](?=\s|$)/)?.[0]
    ?.replace(/`/g, '')

  return first && head.length + first.length < 220 ? `${head} ${first}` : head
}

/** Virtual pages for the loader: a tag folder (meta.json, its index page) and a page per operation. */
export async function referenceSource(): ReturnType<typeof openapi.staticSource> {
  const source = await openapi.staticSource({
    baseDir: BASE.slice(1),
    per: 'custom',
    toPages(builder) {
      for (const tag of referenceTags) {
        builder.create({
          type: 'page',
          path: `${tag.slug}/index.mdx`,
          schemaId: builder.id,
          info: { title: tag.title, description: tag.description },
          // No operations: the index lists them itself (app/(reference)/api/reference/[tag]/page.tsx), and the search
          // index gets their text from the operation pages.
          operations: [],
          webhooks: [],
        })

        for (const op of tag.operations) {
          builder.create({
            type: 'operation',
            path: `${tag.slug}/${op.slug}.mdx`,
            schemaId: builder.id,
            item: { path: op.path, method: op.method },
            info: { title: op.title, description: op.summary, deprecated: op.deprecated },
          })
        }
      }
    },
  })

  // A folder per tag, in the spec's order, collapsed unless the reader is in it.
  const metas = referenceTags.map((tag) => ({
    type: 'meta' as const,
    path: `${BASE.slice(1)}/${tag.slug}/meta.json`,
    data: { title: tag.title, pages: tag.operations.map((op) => op.slug), defaultOpen: false },
  }))

  return { files: [...source.files, ...metas] }
}

const resolve = (doc: Json, node: unknown): Json | undefined => {
  let value = node as Json | undefined
  let hops = 0

  while (value && typeof value.$ref === 'string' && value.$ref.startsWith('#/') && hops++ < 8) {
    value = value.$ref
      .slice(2)
      .split('/')
      .reduce<unknown>(
        (at, part) => (at as Json | undefined)?.[part.replaceAll('~1', '/').replaceAll('~0', '~')],
        doc,
      ) as Json | undefined
  }

  return value
}

const refName = (node: unknown) => {
  const ref = (node as Json | undefined)?.$ref

  return typeof ref === 'string' ? ref.split('/').at(-1) : undefined
}

const oneLine = (text: unknown) => (typeof text === 'string' ? text.replace(/\s+/g, ' ').trim() : '')

let specJson: Json | undefined

/** Markdown of a reference page (llms-full.txt, /<page>.md): an operation's contract, or a tag's operation list. */
export function referenceMarkdown(url: string, absolute: (path: string) => string) {
  const entry = byUrl.get(url)

  if (!entry) {
    return undefined
  }

  if ('operations' in entry) {
    const list = entry.operations.map(
      (op) => `- [${op.title}](${absolute(op.url)}.md): \`${op.method.toUpperCase()} ${op.path}\`. ${op.summary}`,
    )

    return [`# ${entry.title} API reference (${entry.url})`, '', entry.description ?? '', '', ...list].join('\n')
  }

  specJson ??= loadSpec() as unknown as Json

  const doc = specJson
  const item = (doc.paths as Record<string, Json>)[entry.path] ?? {}
  const op = (item[entry.method] ?? {}) as Json
  const params = [...((item.parameters as unknown[]) ?? []), ...((op.parameters as unknown[]) ?? [])]
    .map((p) => resolve(doc, p))
    .filter((p): p is Json => Boolean(p))
  const body = resolve(doc, op.requestBody)
  const responses = Object.entries((op.responses as Json | undefined) ?? {})
  const lines = [
    `# ${entry.title} (${entry.url})`,
    '',
    `\`${entry.method.toUpperCase()} ${entry.path}\` (operationId \`${entry.operationId}\`): ${entry.summary}`,
  ]

  if (entry.description) {
    lines.push('', entry.description)
  }

  if (params.length > 0) {
    lines.push('', '## Parameters', '', '| Name | In | Required | Description |', '|---|---|---|---|')

    for (const p of params) {
      lines.push(
        `| \`${String(p.name)}\` | ${String(p.in)} | ${p.required ? 'yes' : 'no'} | ${oneLine(p.description)} |`,
      )
    }
  }

  if (body?.content) {
    lines.push('', '## Request body', '')

    for (const [type, media] of Object.entries(body.content as Record<string, Json>)) {
      const schema = media.schema as Json | undefined
      const names = [refName(schema), ...((schema?.allOf as unknown[]) ?? []).map(refName)].filter(Boolean)

      lines.push(`- \`${type}\`${names.length > 0 ? `: schema ${names.map((n) => `\`${n}\``).join(', ')}` : ''}`)
    }
  }

  if (responses.length > 0) {
    lines.push('', '## Responses', '')

    for (const [status, raw] of responses) {
      const res = resolve(doc, raw)
      const schemas = Object.values((res?.content as Record<string, Json> | undefined) ?? {})
        .map((m) => refName(m.schema))
        .filter(Boolean)

      lines.push(
        `- ${status}: ${oneLine(res?.description)}${schemas.length > 0 ? ` (\`${schemas.join('`, `')}\`)` : ''}`,
      )
    }
  }

  lines.push('', `Schemas: ${absolute('/openapi.yaml')}`)

  return lines.join('\n')
}
