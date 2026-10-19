import { DocsBody, DocsDescription, DocsPage, DocsTitle } from 'fumadocs-ui/layouts/docs/page'
import type { Metadata } from 'next'
import { notFound } from 'next/navigation'

import { OpenAPIPage } from '@/components/api-page'
import { source } from '@/lib/source'

/*
 * The generated API reference pages (one per OpenAPI tag, /api/reference/<tag>). They have their own route so the
 * OpenAPI playground's client code (Shiki, schema resolution: ~130 KB gzipped) loads only here, not with every
 * Markdown page of [...slug]. The reference index (/api/reference) is a Markdown page and stays there.
 */

function getApiPage(tag: string) {
  const page = source.getPage(['api', 'reference', tag])

  return page?.type === 'openapi' ? page : undefined
}

export default async function ApiReferencePage(props: PageProps<'/api/reference/[tag]'>) {
  const { tag } = await props.params
  const page = getApiPage(tag)

  if (!page) {
    notFound()
  }

  return (
    <DocsPage full toc={page.data.toc}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>
        Generated from <code>apps/api/openapi.yaml</code> (OpenAPI 3.2). Local mode needs no credentials; cloud mode
        expects <code>Authorization: Bearer stk_...</code>. Use <strong>Send</strong> to try a request against your
        cloud instance with your API key.
      </DocsDescription>
      <DocsBody>
        <OpenAPIPage {...page.data.getOpenAPIPageProps()} />
      </DocsBody>
    </DocsPage>
  )
}

export function generateStaticParams() {
  return source
    .getPages()
    .filter((p) => p.type === 'openapi')
    .map((p) => {
      const [api, reference, tag, ...rest] = p.slugs

      if (api !== 'api' || reference !== 'reference' || !tag || rest.length > 0) {
        throw new Error(`OpenAPI page outside /api/reference/<tag>: /${p.slugs.join('/')}`)
      }

      return { tag }
    })
}

export async function generateMetadata(props: PageProps<'/api/reference/[tag]'>): Promise<Metadata> {
  const { tag } = await props.params
  const page = getApiPage(tag)

  if (!page) {
    notFound()
  }

  return { title: page.data.title, description: page.data.description }
}
