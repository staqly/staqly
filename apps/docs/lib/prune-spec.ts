import type { OpenAPIPageProps_Spec } from 'fumadocs-openapi/ui'

type Json = Record<string, unknown>

const METHODS = new Set(['get', 'put', 'post', 'delete', 'options', 'head', 'patch', 'trace', 'query'])

const decode = (part: string) => decodeURIComponent(part).replaceAll('~1', '/').replaceAll('~0', '~')

/**
 * The page's props with only the part of the spec the page shows: its operations and webhooks plus the components
 * they reference (followed through `$ref`s), and every security scheme. fumadocs-openapi hands each page the whole
 * bundled spec, which ends up in the page's HTML and in its RSC payload (~310 KB a page, and the sidebar prefetches
 * the payload of every other reference page).
 */
export function pruneSpec(props: OpenAPIPageProps_Spec): OpenAPIPageProps_Spec {
  const doc = props.payload.bundled as unknown as Json
  const paths = (doc.paths ?? {}) as Record<string, Json>
  const hooks = (doc.webhooks ?? {}) as Record<string, Json>
  const components = (doc.components ?? {}) as Record<string, Record<string, unknown>>
  const keptPaths: Record<string, Json> = {}
  const keptHooks: Record<string, Json> = {}
  const keptComponents: Record<string, Record<string, unknown>> = components.securitySchemes
    ? { securitySchemes: components.securitySchemes }
    : {}
  const seen = new Set<unknown>()

  const walk = (node: unknown) => {
    if (!node || typeof node !== 'object' || seen.has(node)) {
      return
    }

    seen.add(node)

    if (Array.isArray(node)) {
      node.forEach(walk)

      return
    }

    for (const [key, value] of Object.entries(node)) {
      if (key === '$ref' && typeof value === 'string') {
        follow(value)
      } else {
        walk(value)
      }
    }
  }

  // Keeps what a local ref points at: the whole component, or the whole path item.
  const follow = (ref: string) => {
    if (!ref.startsWith('#/')) {
      return
    }

    const [section, kind, name] = ref.slice(2).split('/').map(decode)

    if (section === 'components' && kind && name && components[kind]?.[name] !== undefined) {
      if (keptComponents[kind]?.[name] === undefined) {
        keptComponents[kind] = { ...keptComponents[kind], [name]: components[kind][name] }
        walk(components[kind][name])
      }
    } else if (section === 'paths' && kind && paths[kind]) {
      keptPaths[kind] = { ...keptPaths[kind], ...paths[kind] }
      walk(paths[kind])
    }
  }

  // A path item keeps its shared fields (parameters, servers...) and only the methods this page shows.
  for (const { path, method } of props.operations ?? []) {
    const item = paths[path]

    if (item?.[method]) {
      const shared = Object.fromEntries(Object.entries(item).filter(([key]) => !METHODS.has(key)))

      keptPaths[path] = { ...shared, ...keptPaths[path], [method]: item[method] }
      walk(shared)
      walk(item[method])
    }
  }

  for (const { name } of props.webhooks ?? []) {
    if (hooks[name]) {
      keptHooks[name] = hooks[name]
      walk(hooks[name])
    }
  }

  walk(doc.security)

  const bundled = {
    ...doc,
    paths: keptPaths,
    ...(doc.webhooks ? { webhooks: keptHooks } : {}),
    components: keptComponents,
  }

  return { ...props, payload: { ...props.payload, bundled: bundled as unknown as typeof props.payload.bundled } }
}
