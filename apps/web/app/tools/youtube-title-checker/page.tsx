import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { YoutubeTitleChecker } from '@/components/tools/youtube-title-checker'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'youtube-title-checker')!

export const metadata: Metadata = pageMeta(
  'YouTube title checker: length, truncation and preview',
  'A free YouTube title checker: check a title against the 100-character limit and the characters YouTube refuses, and preview it on home, search and up next, on a computer and a phone, with your own thumbnail.',
  '/tools/youtube-title-checker',
)

export default function Page() {
  return (
    <main id="main" className="flex-1">
      <ToolJsonLd tool={tool} />
      <PageHero
        eyebrow={
          <Link href="/tools" className="hover:text-foreground">
            Free tools
          </Link>
        }
        title="See your title where people will."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Type a title and see where YouTube cuts it, on home, in search and beside other videos, with your thumbnail if
          you have one.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <YoutubeTitleChecker />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">Front-load the title</h2>
          <p>
            YouTube takes up to 100 characters, but no placement shows them all: search on a computer shows about 70,
            phones and the list beside a video fewer. Put the words that make someone click, and the ones they search
            for, in the first 50. The previews use the real widths and let your browser cut the text, so the cut falls
            about where it will on YouTube.
          </p>
        </div>

        <ToolCta title="Staqly checks the title before it uploads">
          Schedule a video to YouTube with its title, description, tags and playlist; staqly flags a title YouTube would
          refuse before anything is uploaded. See{' '}
          <a
            href={`${DOCS}/channels/youtube`}
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            YouTube
          </a>
          .
        </ToolCta>
      </section>
    </main>
  )
}
