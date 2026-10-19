import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { AspectRatioChecker } from '@/components/tools/aspect-ratio-checker'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'aspect-ratio-checker')!

export const metadata: Metadata = pageMeta(
  'Aspect ratio checker for Instagram, TikTok, YouTube, X and Pinterest',
  'A free aspect ratio checker: drop a picture or video, or type its size, and see how it fits Instagram feed and Reels, TikTok, YouTube and Shorts, X, LinkedIn, Facebook, Threads and Pinterest, with a live crop preview. Nothing is uploaded.',
  '/tools/aspect-ratio-checker',
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
        title="Will it fit? Check the shape before you post."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Drop a picture or video, or type its size, and see what every platform crops, from where, and the closest
          shape that fits.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <AspectRatioChecker />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">Ratios, not pixel sizes</h2>
          <p>
            Platforms resize what you upload, and their recommended pixel sizes change often. The shape matters more: a
            picture in the wrong ratio gets cropped in the feed or shown with bars. The ratios here are the common ones
            for each placement; a platform may accept a range around them.
          </p>
        </div>

        <ToolCta title="Staqly checks every post before it goes out">
          Each channel knows what its platform needs: media count and kinds, a title, a link. The composer says what a
          platform objects to before anything is published. See{' '}
          <a href={`${DOCS}/limits`} className="font-medium text-foreground underline-offset-4 hover:underline">
            platform limits
          </a>
          .
        </ToolCta>
      </section>
    </main>
  )
}
