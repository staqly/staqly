import type { Metadata } from 'next'
import Link from 'next/link'

import { PageHero } from '@/components/site'
import { ScheduleValidatorTool } from '@/components/tools/schedule-validator'
import { ToolCta } from '@/components/tools/shared'
import { ToolJsonLd } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

const tool = TOOLS.find((t) => t.slug === 'schedule-validator')!

export const metadata: Metadata = pageMeta(
  'Social media schedule validator: check your posting plan',
  'Check a posting schedule before it runs: posts per day for Instagram, TikTok, YouTube, X and LinkedIn, posts too close together, quiet days, hours your audience sleeps and daylight saving changes. Free, in your browser.',
  '/tools/schedule-validator',
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
        title="Check your posting schedule before it runs."
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Build a plan or paste one: weekdays and times, or a list of dates. See how busy each platform gets, where
          posts crowd together, when your audience is asleep and what a clock change does to it.
        </p>
      </PageHero>
      <section className={`${wrap} py-10 sm:py-14`}>
        <ScheduleValidatorTool />

        <div className="mt-10 grid max-w-3xl gap-3 text-sm leading-relaxed text-muted-foreground">
          <h2 className="text-base font-semibold text-foreground">How it checks</h2>
          <p>
            The rule is read the way a Staqly collection schedule reads it: times of day on chosen weekdays in one time
            zone, or every so many minutes. Times stay on the schedule’s own clock through a daylight saving change, and
            a time the clocks skip goes out an hour later that day.
          </p>
          <p>
            Posts per day and the gap between posts are rules of thumb most accounts stay within, not limits the
            platforms set; a platform may allow far more. Waking hours are 07:00 to 23:00 in the audience’s time zone.
          </p>
        </div>

        <ToolCta title="Run this schedule for you">
          Give a Staqly collection this rule and it publishes the next waiting post at each time, to X or through your
          own automation; your agents can keep it full. See{' '}
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
