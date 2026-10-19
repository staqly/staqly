import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { BestTimeTool } from '@/components/tools/best-time'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'best-time-to-post')!

export const metadata: Metadata = pageMeta(
  'Best time to post on Instagram, TikTok, YouTube, X and LinkedIn',
  'Find the best time to post on Instagram, TikTok, YouTube, X, LinkedIn, Facebook, Threads and Pinterest: a free weekly heatmap in your audience’s time zone and yours, ready to copy as a posting schedule.',
  '/tools/best-time-to-post',
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
        title="Best time to post, per platform and time zone."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Pick a platform and where your audience lives. The heatmap shows the hours that usually work best, moved into
          your own time.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <BestTimeTool />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">Where these hours come from</h2>
          <p>
            They are rules of thumb: the patterns that engagement studies across social platforms keep finding, such as
            lunch and evening peaks for leisure apps and work hours for professional networks. They are a starting
            point, not a promise.
          </p>
          <p>
            Your audience is the real answer. Post at two or three of these slots for a few weeks, compare the results,
            then keep the hours that work. Your platform’s own analytics show when your followers are online.
          </p>
        </div>

        <ToolCta title="Post at these hours without setting an alarm">
          A Staqly collection with this schedule publishes the next waiting post at each slot, to X or through your own
          automation, and your agents can keep it full. See{' '}
          <a
            href={`${DOCS}/automation/schedules`}
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            how schedules work
          </a>
          .
        </ToolCta>
      </section>
    </main>
  )
}
