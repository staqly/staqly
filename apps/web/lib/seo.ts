import type { Metadata } from 'next'

import { DESCRIPTION, GITHUB, SITE_URL, TAGLINE } from './site'

/** The home page's title, also the default for pages without one. */
export const HOME_TITLE = `staqly — ${TAGLINE}`

/*
 * The social card (app/opengraph-image.png). A page that sets its own `openGraph`
 * replaces the root layout's object, file-based image included, so every page names the image again.
 */
const OG_IMAGE = {
  url: '/opengraph-image.png',
  width: 1200,
  height: 630,
  alt: `staqly: ${TAGLINE.charAt(0).toLowerCase()}${TAGLINE.slice(1)}`,
}

/** Per-page title, description, canonical URL and social cards (the root layout supplies the rest). */
export function pageMeta(title: string, description: string, path: string): Metadata {
  return {
    title,
    description,
    alternates: { canonical: path },
    openGraph: { type: 'website', siteName: 'staqly', url: path, title, description, images: [OG_IMAGE] },
    twitter: { card: 'summary_large_image', title, description, images: [OG_IMAGE] },
  }
}

/** The publisher of the site, referenced by id from the other structured data. */
export const ORGANIZATION = {
  '@type': 'Organization',
  '@id': `${SITE_URL}/#organization`,
  name: 'staqly',
  url: SITE_URL,
  logo: `${SITE_URL}/apple-icon.png`,
  sameAs: [GITHUB],
}

/** Organization and WebSite for the home page (no SearchAction: the site has no search URL). */
export const SITE_LD = [
  ORGANIZATION,
  {
    '@type': 'WebSite',
    '@id': `${SITE_URL}/#website`,
    name: 'staqly',
    url: SITE_URL,
    description: DESCRIPTION,
    inLanguage: 'en',
    publisher: { '@id': ORGANIZATION['@id'] },
  },
]

/** BreadcrumbList for a page whose visible breadcrumb is `items` ([name, path]; the last one is the page itself). */
export function breadcrumbLd(items: [string, string][]) {
  return {
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: items.map(([name, path], i) => ({
      '@type': 'ListItem',
      position: i + 1,
      name,
      item: `${SITE_URL}${path}`,
    })),
  }
}

/** For <script type="application/ld+json">: JSON with `<` escaped so text can never close the script element. */
export function ldJson(data: unknown) {
  return JSON.stringify(data).replace(/</g, '\\u003c')
}
