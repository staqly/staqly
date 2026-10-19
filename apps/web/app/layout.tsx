import type { Metadata, Viewport } from 'next'
import type { ReactNode } from 'react'

import { ClosingCta, Footer, Nav } from '@/components/site'
import { HOME_TITLE as title } from '@/lib/seo'
import { DESCRIPTION, SITE_URL } from '@/lib/site'

import { GeistMono, GeistSans } from './fonts'

import './globals.css'

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: { default: title, template: '%s · staqly' },
  description: DESCRIPTION,
  applicationName: 'staqly',
  category: 'technology',
  robots: { index: true, follow: true },
  openGraph: { type: 'website', siteName: 'staqly', locale: 'en_US', title, description: DESCRIPTION },
  twitter: { card: 'summary_large_image', title, description: DESCRIPTION },
}

export const viewport: Viewport = {
  colorScheme: 'dark',
  width: 'device-width',
  initialScale: 1,
  viewportFit: 'cover',
  themeColor: '#16171b',
}

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable} antialiased dark`}>
      <body className="flex min-h-screen flex-col font-sans">
        {/* Every page's <main> has id="main". */}
        <a
          href="#main"
          className="sr-only rounded-lg bg-background px-4 py-2.5 text-sm font-semibold focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-50"
        >
          Skip to content
        </a>
        <Nav />
        {children}
        <ClosingCta />
        <Footer />
      </body>
    </html>
  )
}
