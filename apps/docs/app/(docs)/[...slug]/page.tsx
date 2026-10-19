import {
  DocsBody,
  DocsDescription,
  DocsPage,
  DocsTitle,
  EditOnGitHub,
  MarkdownCopyButton,
  ViewOptionsPopover,
} from 'fumadocs-ui/layouts/docs/page'
import { createRelativeLink } from 'fumadocs-ui/mdx'
import type { Metadata } from 'next'
import { notFound } from 'next/navigation'

import { getMDXComponents } from '@/components/mdx'
import { docsMeta, ldJson, pageJsonLd, pageTitle } from '@/lib/seo'
import { editUrl, getPageMarkdownUrl } from '@/lib/shared'
import { source } from '@/lib/source'

export default async function Page(props: PageProps<'/[...slug]'>) {
  const params = await props.params
  const page = source.getPage(params.slug)

  // OpenAPI pages have their own route: api/reference/[tag].
  if (!page || page.type === 'openapi') {
    notFound()
  }

  const MDX = page.data.body
  const markdownUrl = getPageMarkdownUrl(page).url

  return (
    <DocsPage toc={page.data.toc} full={page.data.full} tableOfContent={{ style: 'clerk' }}>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: ldJson(pageJsonLd({ ...page.data, url: page.url })) }}
      />
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription className="mb-0">{page.data.description}</DocsDescription>
      <div className="flex flex-row flex-wrap items-center gap-2 border-b pb-6">
        <MarkdownCopyButton markdownUrl={markdownUrl} />
        <ViewOptionsPopover markdownUrl={markdownUrl} githubUrl={editUrl(page.path)} />
        <EditOnGitHub href={editUrl(page.path)} />
      </div>
      <DocsBody>
        <MDX
          components={getMDXComponents({
            a: createRelativeLink(source, page),
          })}
        />
      </DocsBody>
    </DocsPage>
  )
}

export async function generateStaticParams() {
  return source
    .getPages()
    .filter((p) => p.type !== 'openapi')
    .map((p) => ({ slug: p.slugs }))
}

export async function generateMetadata(props: PageProps<'/[...slug]'>): Promise<Metadata> {
  const params = await props.params
  const page = source.getPage(params.slug)

  if (!page) {
    notFound()
  }

  return docsMeta(pageTitle(page.data.title, page.url), page.data.description, page.url)
}
