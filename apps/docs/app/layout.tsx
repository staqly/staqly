import type { Metadata, Viewport } from 'next'

import { Provider } from '@/components/provider'
import { env } from '@/lib/env.mjs'
import { DOCS_DESCRIPTION as description } from '@/lib/seo'

import { GeistMono, GeistSans } from './fonts'

import './global.css'

export const metadata: Metadata = {
  // Absolute base for the Open Graph image URL; override with SITE_URL where the docs are hosted.
  metadataBase: new URL(env.siteUrl),
  title: { default: 'staqly docs', template: '%s · staqly docs' },
  description,
  applicationName: 'staqly docs',
  openGraph: { type: 'website', siteName: 'staqly docs', title: 'staqly docs', description },
  twitter: { card: 'summary_large_image', title: 'staqly docs', description },
}

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  viewportFit: 'cover',
  colorScheme: 'dark',
  themeColor: '#16171b',
}

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html
      lang="en"
      className={`dark ${GeistSans.variable} ${GeistMono.variable} antialiased`}
      style={{ colorScheme: 'dark' }}
      suppressHydrationWarning
    >
      <body className="flex min-h-screen flex-col">
        <Provider>{children}</Provider>
      </body>
    </html>
  )
}
