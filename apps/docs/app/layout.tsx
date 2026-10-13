import { GeistMono } from 'geist/font/mono'
import { GeistSans } from 'geist/font/sans'
import type { Metadata, Viewport } from 'next'

import { Provider } from '@/components/provider'

import './global.css'

const description = 'Documentation for staqly — the local-first workspace for everything your AI agents create.'

export const metadata: Metadata = {
  // Absolute base for the Open Graph image URL; override with SITE_URL where the docs are hosted.
  metadataBase: new URL(process.env.SITE_URL ?? 'https://staqly.ai'),
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
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#fcfcfd' },
    { media: '(prefers-color-scheme: dark)', color: '#16171b' },
  ],
}

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable} antialiased`} suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        <Provider>{children}</Provider>
      </body>
    </html>
  )
}
