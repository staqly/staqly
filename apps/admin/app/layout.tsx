import { GeistSans } from 'geist/font/sans'
import type { Metadata, Viewport } from 'next'

import { Providers } from '@/app/providers'

import './globals.css'

export const metadata: Metadata = {
  title: { default: 'staqly admin', template: '%s · staqly admin' },
  robots: { index: false, follow: false },
}

export const viewport: Viewport = { width: 'device-width', initialScale: 1 }

export default function RootLayout({ children }: LayoutProps<'/'>) {
  return (
    <html lang="en" suppressHydrationWarning className={`${GeistSans.variable} h-full antialiased`}>
      <body className="min-h-full">
        <Providers>{children}</Providers>
      </body>
    </html>
  )
}
