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

import { OpenAPIPage } from '@/components/api-page'
import { getMDXComponents } from '@/components/mdx'
import { editUrl, getPageMarkdownUrl } from '@/lib/shared'
import { source } from '@/lib/source'

export default async function Page(props: PageProps<'/[...slug]'>) {
  const params = await props.params
  const page = source.getPage(params.slug)

  if (!page) {
    notFound()
  }

  if (page.type === 'openapi') {
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

  const MDX = page.data.body
  const markdownUrl = getPageMarkdownUrl(page).url

  return (
    <DocsPage toc={page.data.toc} full={page.data.full} tableOfContent={{ style: 'clerk' }}>
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
  return source.generateParams()
}

export async function generateMetadata(props: PageProps<'/[...slug]'>): Promise<Metadata> {
  const params = await props.params
  const page = source.getPage(params.slug)

  if (!page) {
    notFound()
  }

  return {
    title: page.data.title,
    description: page.data.description,
  }
}
