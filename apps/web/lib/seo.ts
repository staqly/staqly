import type { Metadata } from 'next'

/** Per-page title, description, canonical URL and social cards (the root layout supplies the rest). */
export function pageMeta(title: string, description: string, path: string): Metadata {
  return {
    title,
    description,
    alternates: { canonical: path },
    openGraph: { type: 'website', siteName: 'staqly', url: path, title, description },
    twitter: { card: 'summary_large_image', title, description },
  }
}
