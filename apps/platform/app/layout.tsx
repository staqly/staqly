import type { Metadata, Viewport } from 'next'

import { Providers } from '@/components/providers'
import { env } from '@/lib/env'

import { GeistMono, GeistSans } from './fonts'

import './globals.css'

const description = 'The local-first social media scheduler for people and their AI agents.'

export const metadata: Metadata = {
  // Absolute base for the Open Graph image URL. The hosted app is staqly.ai;
  // self-hosted builds can point it at their own origin with SITE_URL.
  metadataBase: new URL(env.siteUrl),
  title: { default: 'staqly', template: '%s · staqly' },
  description,
  applicationName: 'staqly',
  appleWebApp: { title: 'staqly', statusBarStyle: 'default' },
  openGraph: { type: 'website', siteName: 'staqly', title: 'staqly', description },
  twitter: { card: 'summary_large_image', title: 'staqly', description },
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

export default function RootLayout({ children }: LayoutProps<'/'>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${GeistSans.variable} ${GeistMono.variable} h-full antialiased`}
    >
      <body className="min-h-full">
        <Providers>{children}</Providers>
      </body>
    </html>
  )
}
