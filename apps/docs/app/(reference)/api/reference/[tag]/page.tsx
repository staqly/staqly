import { DocsBody, DocsDescription, DocsPage, DocsTitle } from 'fumadocs-ui/layouts/docs/page'
import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'

import { referenceEntry, referenceTags } from '@/lib/reference'
import { docsMeta, ldJson, pageJsonLd } from '@/lib/seo'

/*
 * A tag's page (/api/reference/<tag>): the index of its sidebar folder, listing its operations, each on its own page
 * (./[operation]/page.tsx). No OpenAPI client code here. Each entry keeps the id the operation's heading had when
 * the whole tag was one page, so old links (/api/reference/posts#create-a-post-...) still land on it.
 */

function getTag(tag: string) {
  const entry = referenceEntry(`/api/reference/${tag}`)

  return entry && 'operations' in entry ? entry : undefined
}

export default async function ApiReferenceTagPage(props: PageProps<'/api/reference/[tag]'>) {
  const { tag: slug } = await props.params
  const tag = getTag(slug)

  if (!tag) {
    notFound()
  }

  return (
    <DocsPage toc={[]}>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: ldJson(pageJsonLd({ title: tag.title, description: tag.description, url: tag.url })),
        }}
      />
      <DocsTitle>{tag.title}</DocsTitle>
      <DocsDescription>{tag.description}</DocsDescription>
      <DocsBody>
        <p>
          {tag.operations.length} operations, generated from <code>apps/api/openapi.yaml</code> (OpenAPI 3.2). Each page
          has the parameters, request body, responses and code samples, and a <strong>Send</strong> button to try the
          request against your cloud instance.
        </p>
        <ul className="not-prose flex list-none flex-col gap-2 p-0">
          {tag.operations.map((op) => (
            <li key={op.slug} id={op.anchor} className="scroll-mt-24">
              <Link
                href={op.url}
                prefetch={false}
                className="flex flex-col gap-1 rounded-lg border bg-fd-card px-4 py-3 transition-colors hover:bg-fd-accent [:target>&]:border-fd-primary"
              >
                <span className="sq-endpoint text-sm">
                  <span className={`sq-method sq-method-${op.method}`}>{op.method.toUpperCase()}</span>
                  <code>{op.path}</code>
                </span>
                <span className="font-medium text-fd-foreground">
                  {op.title}
                  {op.deprecated ? <span className="ms-2 text-xs text-fd-muted-foreground">(deprecated)</span> : null}
                </span>
                {op.summary === op.title ? null : (
                  <span className="text-sm text-fd-muted-foreground">{op.summary}</span>
                )}
              </Link>
            </li>
          ))}
        </ul>
      </DocsBody>
    </DocsPage>
  )
}

export function generateStaticParams() {
  return referenceTags.map((tag) => ({ tag: tag.slug }))
}

export async function generateMetadata(props: PageProps<'/api/reference/[tag]'>): Promise<Metadata> {
  const { tag: slug } = await props.params
  const tag = getTag(slug)

  if (!tag) {
    notFound()
  }

  // "Posts API reference", not "Posts": the guides have pages called Posts, Channels, Webhooks too.
  return docsMeta(`${tag.title} API reference`, tag.description, tag.url)
}
