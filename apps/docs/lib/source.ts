import type * as PageTree from 'fumadocs-core/page-tree'
import { llms, loader, type LoaderPlugin } from 'fumadocs-core/source'
import { createElement } from 'react'

import { docs } from './collections'
import { env } from './env.mjs'
import { operationPageTitle, referenceEntry, referenceMarkdown, referenceSource } from './reference'
import { docsRoute } from './shared'

// Docs pages + the virtual API reference (lib/reference.ts): /api/reference/<tag> and /api/reference/<tag>/<operation>.
export const source = loader(
  {
    docs: docs.toFumadocsSource(),
    openapi: await referenceSource(),
  },
  {
    baseUrl: docsRoute,
    // No `$ref` (source file paths) on the tree's nodes: nothing reads them, and every page's HTML carries the tree.
    pageTree: { noRef: true },
    plugins: [referenceTreePlugin()],
  },
)

/*
 * Sidebar entries of the operation pages: the title and the method as a data attribute (app/global.css shows it).
 * Every page's HTML carries the whole tree, so this replaces fumadocs-openapi's loaderPlugin(), whose badge
 * element with its classes, plus the summary as the node's description, made up most of 195 operation entries.
 */
function referenceTreePlugin(): LoaderPlugin {
  return {
    name: 'staqly:reference-tree',
    transformPageTree: {
      file(node) {
        const op = referenceEntry(node.url)

        if (!op || 'operations' in op) {
          return node
        }

        return {
          ...node,
          description: undefined,
          name: createElement(
            'span',
            { className: op.deprecated ? 'sq-tree-op line-through' : 'sq-tree-op', 'data-method': op.method },
            op.title,
          ),
        }
      },
    },
  }
}

// A tag's folder becomes its index page alone; other folders keep their children.
function trimReference(nodes: PageTree.Node[]): PageTree.Node[] {
  return nodes.map((n) => {
    if (n.type !== 'folder') {
      return n
    }

    const tag = n.index ? referenceEntry(n.index.url) : undefined

    return tag && 'operations' in tag && n.index ? n.index : { ...n, children: trimReference(n.children) }
  })
}

/**
 * The sidebar of every page outside the API reference: each tag's folder is just a link to its page, without the
 * ~195 operation entries (app/(reference)/layout.tsx shows them).
 */
export function docsPageTree(): PageTree.Root {
  const tree = source.getPageTree()

  return { ...tree, children: trimReference(tree.children) }
}

const operationOf = (node: { type?: string; url?: string }) => {
  const op = node.type === 'page' && node.url ? referenceEntry(node.url) : undefined

  return op && !('operations' in op) ? op : undefined
}

export const docsLlms = llms(source, {
  // Operation entries have an element as their sidebar name (referenceTreePlugin): llms.txt names them in text.
  renderName: (node) => {
    const op = operationOf(node)

    if (op) {
      return operationPageTitle(op)
    }

    return typeof node.name === 'string' ? node.name : ''
  },
  renderDescription: (node) => {
    const op = operationOf(node)

    if (op) {
      return op.summary === op.title ? '' : op.summary
    }

    return 'description' in node && typeof node.description === 'string' ? node.description : ''
  },
  renderPage: async (page) => {
    if (page.type === 'openapi') {
      return (
        referenceMarkdown(page.url, (path) => `${env.siteUrl}${env.basePath}${path}`) ??
        `# ${page.data.title} (${page.url})\n\nSee the OpenAPI contract: /openapi.yaml`
      )
    }

    return `# ${page.data.title} (${page.url})\n\n${await page.data.getText('processed')}`
  },
})
