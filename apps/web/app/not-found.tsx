import { ArrowRight } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { DOCS, arrowLink, btnPrimary, btnSecondary } from '@/lib/site'

// The export's 404.html: hosts serve it with status 404, and Next adds <meta name="robots" content="noindex">.
export const metadata: Metadata = {
  title: 'Page not found',
  description: 'This page does not exist on staqly.ai. Start from the home page, pricing or the docs.',
}

export default function NotFound() {
  return (
    <main id="main" className="flex-1">
      <PageHero eyebrow="404" title="This page isn't here">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          The link may be old or mistyped. These are good places to start.
        </p>
        <div className="mt-9 flex flex-wrap items-center gap-3">
          <Link href="/" className={btnPrimary}>
            Home <ArrowRight className="size-4" />
          </Link>
          <Link href="/pricing" className={btnSecondary}>
            Pricing
          </Link>
          <a href={DOCS} className={`${arrowLink} ml-2`}>
            Documentation <ArrowRight className="size-4" />
          </a>
        </div>
      </PageHero>
    </main>
  )
}
