import type { Metadata } from 'next'

import { env } from './env.mjs'
import { repoUrl } from './shared'
import { source } from './source'

/** Absolute URL of a docs path ('/quickstart'); SITE_URL is the docs origin, the base path is added here. */
export function absUrl(path: string) {
  return `${env.siteUrl}${env.basePath}${path === '/' ? '' : path}`
}

export const DOCS_DESCRIPTION =
  'Documentation for Staqly, the local-first social media scheduler for people and their AI agents.'

/*
 * The social card (app/opengraph-image.png). A page that sets its own `openGraph` replaces the root layout's object,
 * file-based image included, so every page names it again.
 */
const OG_IMAGE = {
  url: `${env.basePath}/opengraph-image.png`,
  width: 1200,
  height: 630,
  alt: 'staqly docs: the local-first social media scheduler for people and their AI agents',
}

/** Title, description, canonical URL (no trailing slash: GitHub Pages serves /quickstart from quickstart.html) and cards. */
export function docsMeta(
  title: string,
  description: string | undefined,
  path: string,
  type: 'website' | 'article' = 'article',
): Metadata {
  const url = absUrl(path)

  return {
    title,
    description,
    alternates: { canonical: url },
    openGraph: { type, siteName: 'staqly docs', url, title, description, images: [OG_IMAGE] },
    twitter: { card: 'summary_large_image', title, description, images: [OG_IMAGE] },
  }
}

type TreeNode = ReturnType<typeof source.getPageTree>['children'][number]

/**
 * The folders a page sits in, outermost first, as the sidebar names them ('Connect agents', 'API reference'), with
 * the URL of the folder's own page if it has one. A folder's index page sits in that folder.
 */
export function sectionNames(url: string) {
  const walk = (
    nodes: TreeNode[],
    trail: { name: string; url?: string }[],
  ): { name: string; url?: string }[] | undefined => {
    // A folder inlined into the sidebar ("...agents") has no node of its own: its pages sit under a separator.
    let separator: { name: string } | undefined

    for (const n of nodes) {
      if (n.type === 'separator') {
        separator = typeof n.name === 'string' ? { name: n.name } : undefined
      }

      if (n.type === 'page' && n.url === url) {
        return trail.length === 0 && separator ? [separator] : trail
      }

      if (n.type === 'folder') {
        const here = [...trail, { name: typeof n.name === 'string' ? n.name : '', url: n.index?.url }]

        if (n.index?.url === url) {
          return here
        }

        const found = walk(n.children, here)

        if (found) {
          return found
        }
      }
    }

    return undefined
  }

  return (walk(source.getPageTree().children, []) ?? []).filter((f) => f.name)
}

const titleCount = new Map<string, number>()

// The API reference pages are left out: their <title> already says "API reference".
for (const p of source.getPages()) {
  if (p.type !== 'openapi' && p.data.title) {
    titleCount.set(p.data.title, (titleCount.get(p.data.title) ?? 0) + 1)
  }
}

/**
 * <title> text for a docs page. A generic "Overview" becomes "<Folder> overview", and a title two pages share
 * ("Channels" in the guides and in the dashboard section) gets its folder: search results should tell them apart.
 */
export function pageTitle(title: string | undefined, url: string) {
  if (!title) {
    return 'staqly docs'
  }

  const folder = sectionNames(url).at(-1)?.name

  if (!folder) {
    return title
  }

  if (title === 'Overview') {
    return `${folder} overview`
  }

  return (titleCount.get(title) ?? 0) > 1 && folder !== title ? `${title} (${folder})` : title
}

const ORGANIZATION = {
  '@type': 'Organization',
  '@id': `${env.webUrl}/#organization`,
  name: 'staqly',
  url: env.webUrl,
  sameAs: [repoUrl],
}

/** TechArticle + BreadcrumbList (docs home > folders that have their own page > this page) for a docs page. */
export function pageJsonLd(page: { title?: string; description?: string; url: string }) {
  const url = absUrl(page.url)
  const crumbs = [
    { name: 'staqly docs', url: '/' },
    ...sectionNames(page.url).filter((c) => c.url && c.url !== page.url),
    {
      name: page.title === 'Overview' ? pageTitle(page.title, page.url) : (page.title ?? 'staqly docs'),
      url: page.url,
    },
  ]

  return [
    {
      '@context': 'https://schema.org',
      '@type': 'TechArticle',
      headline: page.title ?? pageTitle(page.title, page.url),
      description: page.description,
      url,
      mainEntityOfPage: url,
      image: absUrl('/opengraph-image.png'),
      inLanguage: 'en',
      isPartOf: { '@type': 'WebSite', '@id': `${absUrl('/')}/#website`, name: 'staqly docs', url: absUrl('/') },
      author: ORGANIZATION,
      publisher: ORGANIZATION,
    },
    {
      '@context': 'https://schema.org',
      '@type': 'BreadcrumbList',
      itemListElement: crumbs.map((c, i) => ({
        '@type': 'ListItem',
        position: i + 1,
        name: c.name,
        item: absUrl(c.url ?? '/'),
      })),
    },
  ]
}

/** WebSite for the docs home. No SearchAction: search is a dialog over a static index, not a URL. */
export const SITE_JSON_LD = {
  '@context': 'https://schema.org',
  '@type': 'WebSite',
  '@id': `${absUrl('/')}/#website`,
  name: 'staqly docs',
  url: absUrl('/'),
  description: DOCS_DESCRIPTION,
  inLanguage: 'en',
  publisher: ORGANIZATION,
}

/** For <script type="application/ld+json">: JSON with `<` escaped so text can never close the script element. */
export function ldJson(data: unknown) {
  return JSON.stringify(data).replace(/</g, '\\u003c')
}
