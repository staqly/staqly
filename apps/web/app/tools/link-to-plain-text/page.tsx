import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { LinkToPlainText } from '@/components/tools/link-to-plain-text'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'link-to-plain-text')!

export const metadata: Metadata = pageMeta(
  'Turn links into plain text (example[.]com) or remove them',
  'A free tool that turns links into readable plain text no platform treats as a link (example[.]com), removes them from a post, or turns plain text back into links. Useful for X, where posts with links cost more and reach fewer people.',
  '/tools/link-to-plain-text',
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
        title="Links to plain text, and back."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Keep an address readable without making it a link, take links out of a post, or restore them.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <LinkToPlainText />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">Why take links out</h2>
          <p>
            The X API charges more for a post with a link, and X shows such posts to fewer people. Writing the address
            as example[.]com keeps it readable for people while no platform turns it into a link. Email addresses and
            file names like notes.txt are left as they are.
          </p>
        </div>

        <ToolCta title="Staqly does this for every post on X">
          Each channel has a setting for links: keep them, remove them or show them as plain text. Your post keeps its
          links everywhere else. See{' '}
          <a href={`${DOCS}/channels/x`} className="font-medium text-foreground underline-offset-4 hover:underline">
            links on X
          </a>
          .
        </ToolCta>
      </section>
    </main>
  )
}
