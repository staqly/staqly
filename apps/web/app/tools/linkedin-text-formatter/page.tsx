import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { ToolCta } from '@/components/tools/shared'
import { TextFormatter } from '@/components/tools/text-formatter'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'linkedin-text-formatter')!

export const metadata: Metadata = pageMeta(
  'LinkedIn text formatter: bold, italic and lists for your posts',
  'A free LinkedIn text formatter: bold, italic, underline, strikethrough, script, monospace and lists for LinkedIn, X, Instagram and Threads, with a preview of where the feed folds your post behind see more.',
  '/tools/linkedin-text-formatter',
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
        title="Bold and italic where there is no button for it."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Select words, pick a style, copy the post. Works on LinkedIn, X, Instagram, Threads and anywhere else that
          only takes plain text.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <TextFormatter />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">How it works, and when not to</h2>
          <p>
            LinkedIn posts have no formatting, so the styles here swap letters for look-alike Unicode symbols: they
            paste anywhere plain text goes. That is also their cost. Screen readers may read them letter by letter or
            skip them, and search may not match the styled words, so keep styles for a word or a heading, never whole
            paragraphs or the words people search for. Clear formatting turns everything back into plain letters.
          </p>
        </div>

        <ToolCta title="One post, every channel, each in its own words">
          Write a post once in staqly and give LinkedIn, X or Instagram their own version where they need it, then
          schedule them together. See{' '}
          <a
            href={`${DOCS}/channels/linkedin`}
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            LinkedIn
          </a>
          .
        </ToolCta>
      </section>
    </main>
  )
}
