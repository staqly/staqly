import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { CharacterCounter } from '@/components/tools/character-counter'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'character-counter')!

export const metadata: Metadata = pageMeta(
  'Character counter for Instagram, TikTok, YouTube, X, Threads and LinkedIn',
  'A free character counter for every social platform: paste a caption and see where it fits on Instagram (2,200), TikTok (2,200), X (280), Threads (500), LinkedIn (3,000), Bluesky (300) and more, with links handled the way X wants them.',
  '/tools/character-counter',
)

export default function Page() {
  return (
    <main className="flex-1">
      <ToolJsonLd tool={tool} />
      <PageHero
        eyebrow={
          <Link href="/tools" className="hover:text-foreground">
            Free tools
          </Link>
        }
        title="One caption, every platform's limit."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Paste your post and see at a glance where it fits and where it needs a shorter version.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <CharacterCounter />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">How it counts</h2>
          <p>
            One per character, emoji included, the way Staqly checks a post before it goes out. Platforms can weigh some
            characters differently (X counts every link as 23 characters, for example), so leave a little room near a
            limit.
          </p>
        </div>

        <ToolCta title="Write once, let each platform get its own version">
          In Staqly one post goes to every channel you pick, with a shorter text where a platform needs it and these
          limits checked before anything is published.
        </ToolCta>
      </section>
    </main>
  )
}
