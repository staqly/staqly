import { Accordion, Accordions } from 'fumadocs-ui/components/accordion'
import { File, Files, Folder } from 'fumadocs-ui/components/files'
import { Step, Steps } from 'fumadocs-ui/components/steps'
import { Tab, Tabs } from 'fumadocs-ui/components/tabs'
import { TypeTable } from 'fumadocs-ui/components/type-table'
import defaultMdxComponents from 'fumadocs-ui/mdx'
import type { MDXComponents } from 'mdx/types'
import type { FunctionComponent, JSX } from 'react'

import { Badge, ComingSoon, Kbd, Method, Required } from '@/components/mdx/bits'
import { Mermaid } from '@/components/mdx/mermaid'
import { env } from '@/lib/env.mjs'

type Anchor = FunctionComponent<JSX.IntrinsicElements['a']>

// A link to one of the site's files (/openapi.yaml, /llms.txt) is a plain <a>: through next/link it would be
// prefetched as a page, a request for /openapi.yaml/__next._tree.txt that 404s.
function withFileLinks(Page: Anchor): Anchor {
  return function Link({ href, children, ...props }) {
    if (href?.startsWith('/') && /\.\w+$/.test(href.split(/[?#]/)[0]!)) {
      return (
        <a href={env.basePath + href} {...props}>
          {children}
        </a>
      )
    }

    return (
      <Page href={href} {...props}>
        {children}
      </Page>
    )
  }
}

export function getMDXComponents(components?: MDXComponents) {
  return {
    ...defaultMdxComponents,
    Tab,
    Tabs,
    Step,
    Steps,
    Accordion,
    Accordions,
    TypeTable,
    File,
    Files,
    Folder,
    Mermaid,
    Badge,
    Kbd,
    Method,
    ComingSoon,
    Required,
    ...components,
    a: withFileLinks((components?.a as Anchor | undefined) ?? defaultMdxComponents.a),
  } satisfies MDXComponents
}

export const useMDXComponents = getMDXComponents

declare global {
  type MDXProvidedComponents = ReturnType<typeof getMDXComponents>
}
