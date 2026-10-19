import { DocsBody, DocsDescription, DocsPage, DocsTitle } from 'fumadocs-ui/layouts/docs/page'
import type { Metadata } from 'next'
import { notFound } from 'next/navigation'

import { OpenAPIPage } from '@/components/api-page'
import { pruneSpec } from '@/lib/prune-spec'
import { operationPageDescription, operationPageTitle, referenceEntry, referenceTags } from '@/lib/reference'
import { docsMeta, ldJson, pageJsonLd } from '@/lib/seo'
import { source } from '@/lib/source'

/*
 * One page per API operation (/api/reference/<tag>/<operation>, lib/reference.ts). Its own route so the OpenAPI
 * playground's client code (Shiki, schema resolution: ~130 KB gzipped) loads only here, not with every Markdown page
 * of [...slug] or the tag pages; the page ships only its operation and the components it references (pruneSpec).
 */

function getOperation(tag: string, operation: string) {
  const url = `/api/reference/${tag}/${operation}`
  const op = referenceEntry(url)
  const page = source.getPage(['api', 'reference', tag, operation])

  if (!op || 'operations' in op || page?.type !== 'openapi') {
    return undefined
  }

  return { op, page }
}

export default async function ApiOperationPage(props: PageProps<'/api/reference/[tag]/[operation]'>) {
  const { tag, operation } = await props.params
  const found = getOperation(tag, operation)

  if (!found) {
    notFound()
  }

  const { op, page } = found

  return (
    <DocsPage full toc={[]}>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: ldJson(pageJsonLd({ title: op.title, description: operationPageDescription(op), url: op.url })),
        }}
      />
      <DocsTitle>{op.title}</DocsTitle>
      <DocsDescription className="mb-0">{op.summary}</DocsDescription>
      <p className="text-sm text-fd-muted-foreground">
        Every request sends <code>Authorization: Bearer ...</code>: the access token in local mode, an API key{' '}
        <code>stk_...</code> in cloud mode. Use <strong>Send</strong> to try it against your cloud instance with your
        API key.
      </p>
      <DocsBody>
        <OpenAPIPage {...pruneSpec(page.data.getOpenAPIPageProps())} />
      </DocsBody>
    </DocsPage>
  )
}

export function generateStaticParams() {
  return referenceTags.flatMap((tag) => tag.operations.map((op) => ({ tag: tag.slug, operation: op.slug })))
}

export async function generateMetadata(props: PageProps<'/api/reference/[tag]/[operation]'>): Promise<Metadata> {
  const { tag, operation } = await props.params
  const found = getOperation(tag, operation)

  if (!found) {
    notFound()
  }

  return docsMeta(operationPageTitle(found.op), operationPageDescription(found.op), found.op.url)
}
