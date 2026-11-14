import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { ImageSplitter } from '@/components/tools/image-splitter'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'instagram-carousel-splitter')!

export const metadata: Metadata = pageMeta(
  'Instagram carousel splitter: a seamless panorama in slides',
  'A free Instagram carousel splitter: turn a wide picture or panorama into 2 to 20 slides that run on seamlessly when swiped, in 4:5 or square, with a swipe preview and a ZIP of every slide. Nothing is uploaded.',
  '/tools/instagram-carousel-splitter',
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
        title="A panorama that keeps going when swiped."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Turn a wide picture into carousel slides that join up edge to edge. See the cuts, swipe the preview, download
          every slide in order.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <ImageSplitter mode="carousel" />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">Seamless slides</h2>
          <p>
            Each slide is the next stretch of the same picture, so the image runs on when someone swipes. The number of
            slides is picked from your picture&apos;s width, to crop as little as possible; change it and move the crop
            if the cuts fall on a face or a word. Slides come out at 1080 pixels wide, as JPEGs, the format Instagram
            wants.
          </p>
        </div>

        <ToolCta title="Schedule the carousel, not just the slides">
          Add the slides to one post in staqly, pick a time, and it goes out to Instagram as a carousel, with the same
          text on your other channels. See{' '}
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
