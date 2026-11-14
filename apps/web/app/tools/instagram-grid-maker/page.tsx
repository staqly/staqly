import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { ImageSplitter } from '@/components/tools/image-splitter'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'instagram-grid-maker')!

export const metadata: Metadata = pageMeta(
  'Instagram grid maker: split a picture into a profile grid',
  'A free Instagram grid maker: cut one picture into 3 to 15 posts that join up on your profile, as 4:5, 3:4 or square posts, numbered in the order you post them and downloaded as one ZIP. Nothing is uploaded.',
  '/tools/instagram-grid-maker',
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
        title="One picture, a whole profile."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Cut a picture into rows of three posts that join up on your Instagram profile. Pick the shape, move the crop,
          download every post numbered in the order to publish it.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <ImageSplitter mode="grid" />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">Post them in order</h2>
          <p>
            Instagram puts the newest post top left, so the grid fills from the bottom right: publish post 1 first and
            the last number last, without other posts in between. The profile shows every post at 3:4, trimming the
            sides of wider ones; the 4:5 and square posts here carry the trimmed strip of their neighbours, so the
            picture joins up on the profile and each post still looks whole in the feed.
          </p>
        </div>

        <ToolCta title="Schedule the whole grid in one go">
          Put every post on the calendar in the right order, minutes apart, and staqly publishes them to Instagram for
          you. See{' '}
          <a
            href={`${DOCS}/channels/instagram`}
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            Instagram
          </a>
          .
        </ToolCta>
      </section>
    </main>
  )
}
