import { GeistMono } from 'geist/font/mono'
import { GeistSans } from 'geist/font/sans'
import type { Metadata, Viewport } from 'next'
import type { ReactNode } from 'react'

import { ClosingCta, Footer, Nav } from '@/components/site'
import { DESCRIPTION, KEYWORDS, SITE_URL, TAGLINE } from '@/lib/site'

import './globals.css'

const title = `staqly — ${TAGLINE}`

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: { default: title, template: '%s · staqly' },
  description: DESCRIPTION,
  keywords: KEYWORDS,
  applicationName: 'staqly',
  category: 'technology',
  alternates: { canonical: '/' },
  robots: { index: true, follow: true },
  openGraph: { type: 'website', siteName: 'staqly', url: '/', locale: 'en_US', title, description: DESCRIPTION },
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
        <Nav />
        {children}
        <ClosingCta />
        <Footer />
      </body>
    </html>
  )
}
