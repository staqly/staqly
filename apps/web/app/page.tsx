import { HeroDemo } from '@staqly/brand/hero-demo'
import { ProviderTile } from '@staqly/brand/providers'
import { ArrowRight, Check, Plus, Sparkles } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { PhoneShot, Shot } from '@/components/shot'
import { Eyebrow, SectionHead, TrialNote } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { ToolPreview } from '@/components/tool-preview'
import { ToolCards } from '@/components/tools/tool-cards'
import { UseCaseCards } from '@/components/use-case-cards'
import { CHANNELS } from '@/lib/channels'
import { PLANS, TRIAL_DAYS } from '@/lib/plans'
import { HOME_TITLE, SITE_LD, ldJson, pageMeta } from '@/lib/seo'
import {
  DESCRIPTION,
  DOCS,
  GITHUB,
  INSTALL,
  SIGNUP,
  SITE_URL,
  arrowLink,
  btnPrimary,
  btnSecondary,
  wrap,
  wrapWide,
} from '@/lib/site'
import { TOOLS } from '@/lib/tools'
import { allCases } from '@/lib/use-cases'

function Hero() {
  return (
    <section className="relative isolate overflow-hidden">
      <div aria-hidden className="sq-grid-bg absolute inset-0 -z-10" />
      <div
        aria-hidden
        className="sq-glow absolute -top-48 left-1/2 -z-10 h-[34rem] w-[min(64rem,160vw)] -translate-x-1/2"
      />
      <div className={`${wrap} flex flex-col items-center pt-14 text-center sm:pt-20`}>
        <p className="inline-flex max-w-full items-center gap-2 rounded-full border border-border bg-card/80 px-3 py-1.5 text-xs font-medium text-muted-foreground shadow-sm backdrop-blur">
          <span className="size-1.5 shrink-0 rounded-full bg-brand" /> Social media scheduler for you and your AI agents
        </p>
        <h1 className="mt-7 max-w-4xl text-[2.6rem] leading-[1.02] font-semibold tracking-[-0.05em] text-balance sm:text-6xl lg:text-[4.6rem]">
          Your posts on every platform, <span className="sq-gradient-text">right on time.</span>
        </h1>
        <p className="mt-6 max-w-2xl text-lg leading-relaxed text-pretty text-muted-foreground sm:text-xl">
          Write or design a post once, pick the channels, give it a time or a weekly rhythm. Staqly publishes it to
          Instagram, LinkedIn, X, TikTok, YouTube and 15 more, and lets you or your AI fill the calendar ahead.
        </p>
        <div className="mt-9 flex flex-wrap justify-center gap-3">
          <a href={SIGNUP} className={`${btnPrimary} h-12 px-6 text-base`}>
            Start free trial <ArrowRight className="size-4" />
          </a>
          <Link href="/#how" className={`${btnSecondary} h-12 px-6 text-base`}>
            See how it works
          </Link>
        </div>
        <TrialNote className="mt-5" />
      </div>

      {/* The product: the real calendar, with the loop (an agent writes, the schedule publishes) and the phone
          floating over it. Phones get the loop alone; a whole dashboard would be too small to read there. */}
      <div className={`${wrapWide} relative mt-14 pb-16 sm:mt-20 sm:pb-24`}>
        <div aria-hidden className="sq-halo absolute inset-x-0 -top-10 bottom-0 -z-10" />
        <div className="hidden sm:block lg:mr-40 lg:ml-24">
          <Shot
            name="calendar"
            eager
            alt="The staqly calendar, week view: a tip at 09:00 every weekday on Threads, X and LinkedIn, a product launch on Monday at 13:00 and a live Q&A on Wednesday at 17:00"
            sizes="(min-width: 80rem) 1000px, (min-width: 64rem) calc(100vw - 20rem), calc(100vw - 4rem)"
          />
        </div>
        <div className="mx-auto max-w-md sm:absolute sm:right-8 sm:-bottom-2 sm:w-[25rem] lg:right-8 lg:bottom-6 xl:right-[max(2rem,calc(50%-38rem))]">
          <HeroDemo />
        </div>
        <div className="absolute bottom-10 left-8 hidden w-44 lg:block xl:left-[max(2rem,calc(50%-38rem))]">
          <PhoneShot className="sq-float" />
        </div>
      </div>
    </section>
  )
}

function ChannelStrip() {
  const tiles = CHANNELS.filter(([id]) => id !== 'custom')

  return (
    <section aria-labelledby="strip" className="border-y border-border bg-card/40 py-8">
      <p id="strip" className="px-5 text-center text-sm text-balance text-muted-foreground">
        Publishes straight to the platforms your audience uses, and anywhere else through n8n or a script
      </p>
      <div className="sq-marquee-wrap mt-6 overflow-hidden">
        <ul className="sq-marquee flex w-max gap-3">
          {[...tiles, ...tiles].map(([id, name], i) => (
            <li
              key={`${id}-${i}`}
              aria-hidden={i >= tiles.length || undefined}
              className="flex h-12 items-center gap-2.5 rounded-xl border border-border bg-background px-3.5 text-sm font-medium whitespace-nowrap"
            >
              <ProviderTile id={id} name={name} className="size-7 shrink-0 rounded-md" />
              {name}
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}

const audiences: { who: string; title: string; body: string }[] = [
  {
    who: 'Creators',
    title: 'Post every day without living in five apps.',
    body: 'Batch the week’s reels and captions on Sunday. A schedule sends one out every evening at 18:00, on Instagram, TikTok and Threads at once.',
  },
  {
    who: 'Small teams and agencies',
    title: 'One calendar for every account and every client.',
    body: 'Channel groups per brand, teammates with their own roles, and a preview link a client can comment on without an account.',
  },
  {
    who: 'People with AI agents',
    title: 'Let your agent fill the calendar.',
    body: 'Claude Code, Codex, n8n or a script write posts into Staqly. It keeps the rhythm and asks for more when the queue runs dry.',
  },
]

function Audiences() {
  return (
    <section className={`${wrap} py-20 sm:py-28`}>
      <SectionHead eyebrow="Who it’s for" title="Built for people who post more than they have time for." />
      <div className="mt-12 grid gap-10 md:grid-cols-3 md:gap-8">
        {audiences.map((a, i) => (
          <div key={a.who} className="sq-reveal border-t-2 border-brand/70 pt-6">
            <p className="font-mono text-xs tracking-wider text-brand-strong uppercase">
              0{i + 1} · {a.who}
            </p>
            <h3 className="mt-3 text-xl leading-snug font-semibold tracking-tight text-balance">{a.title}</h3>
            <p className="mt-3 leading-relaxed text-muted-foreground">{a.body}</p>
          </div>
        ))}
      </div>
    </section>
  )
}

function Bullets({ items }: { items: ReactNode[] }) {
  return (
    <ul className="mt-6 space-y-3">
      {items.map((b, i) => (
        <li key={i} className="flex gap-3 text-[15px] leading-relaxed">
          <Check className="mt-1 size-4 shrink-0 text-brand" /> <span>{b}</span>
        </li>
      ))}
    </ul>
  )
}

function Chip({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center rounded-full border border-border bg-card px-3 py-1 font-mono text-xs text-muted-foreground">
      {children}
    </span>
  )
}

const steps: { title: string; body: string; bullets: ReactNode[]; chips?: string[]; shot: ReactNode }[] = [
  {
    title: 'Write it once',
    body: 'Pick the channels and write one text. Open a channel’s tab to shorten it for X or give it a title for YouTube; a counter per platform and a check before publishing catch what each one would refuse.',
    bullets: [
      'Pictures, videos and a thread of comments under the post',
      'Design and Video right under the text, for the visual',
      'Tags that become hashtags where the platform has none',
    ],
    shot: (
      <Shot
        name="composer"
        alt="The post composer: a title, four channels (Bluesky, a newsletter, LinkedIn and X) with a tab each for its own text, the shared text with a 145/300 counter, and Upload, From library, Design and Video under it"
        sizes="(min-width: 64rem) 640px, calc(100vw - 2.5rem)"
        crop="aspect-[1060/715] object-top"
      />
    ),
  },
  {
    title: 'Give it a time, or a rhythm',
    body: 'Drop a post on a day, or let a collection keep the pace: every weekday at 09:00, every evening at 18:00, the next waiting post each time. Drag a post to move it, from your desk or your phone.',
    bullets: [
      'Day, week, month and list views, in your time zone',
      'Schedules per collection that survive daylight saving',
      'Repeat a post every week or every month',
    ],
    chips: ['Weekdays at 09:00', 'next waiting post', 'LinkedIn · X · Threads'],
    shot: (
      <>
        <Shot
          name="calendar-week"
          alt="The calendar's week view with a scheduled tip at 09:00 every weekday and two product posts"
          sizes="(min-width: 64rem) 640px, calc(100vw - 2.5rem)"
          className="hidden sm:block"
        />
        <PhoneShot className="mx-auto w-60 sm:hidden" />
      </>
    ),
  },
  {
    title: 'Staqly publishes, and tells you',
    body: 'At its time every channel goes out on its own and Staqly keeps the link. If a platform says no, the post moves to Needs attention with the reason and a Retry for that channel; the others are not held back.',
    bullets: [
      'Failed channels retry on their own first',
      'Alerts on Telegram, Slack, Discord, ntfy or email',
      'The full history of every post: who wrote it, where it went',
    ],
    shot: (
      <Shot
        name="posts"
        alt="A collection of posts: 8 scheduled, 4 drafts, 20 published and 2 failed, the failed ones under Needs attention, the upcoming ones with their channels and times"
        sizes="(min-width: 64rem) 640px, calc(100vw - 2.5rem)"
        crop="aspect-[1060/620] object-top"
      />
    ),
  },
]

function HowItWorks() {
  return (
    <section id="how" className="scroll-mt-16 border-y border-border bg-card/40">
      <div className={`${wrap} py-20 sm:py-28`}>
        <SectionHead eyebrow="How it works" title="From idea to published in three steps." center>
          No copying the same post into four apps, no alarm for 9 am. Here is the real dashboard.
        </SectionHead>
        <ol className="mt-16 space-y-20 sm:mt-20 sm:space-y-28">
          {steps.map((s, i) => (
            <li
              key={s.title}
              className="grid items-center gap-10 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] lg:gap-14"
            >
              <div className={i % 2 ? 'lg:order-2' : ''}>
                <span className="sq-gradient-text font-mono text-5xl font-semibold tracking-tight">0{i + 1}</span>
                <h3 className="mt-3 text-3xl font-semibold tracking-[-0.03em] text-balance">{s.title}</h3>
                <p className="mt-4 text-lg leading-relaxed text-pretty text-muted-foreground">{s.body}</p>
                {s.chips && (
                  <div className="mt-5 flex flex-wrap gap-2">
                    {s.chips.map((c) => (
                      <Chip key={c}>{c}</Chip>
                    ))}
                  </div>
                )}
                <Bullets items={s.bullets} />
              </div>
              <div className={`sq-reveal min-w-0 ${i % 2 ? 'lg:order-1' : ''}`}>{s.shot}</div>
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}

function Ai() {
  return (
    <section id="ai" className={`${wrap} scroll-mt-16 py-20 sm:py-28`}>
      <SectionHead
        eyebrow="AI, on your terms"
        title={
          <>
            AI drafts the posts. <span className="sq-gradient-text">You decide what goes out.</span>
          </>
        }
      >
        Use the assistant built into the dashboard, or connect the agent you already work with. Either way the posts
        land on your calendar first.
      </SectionHead>
      <div className="mt-12 grid gap-6 lg:grid-cols-2 [&>*]:min-w-0">
        <article className="sq-reveal flex flex-col overflow-hidden rounded-3xl border border-border bg-card">
          <div className="p-7 sm:p-9">
            <p className="inline-flex items-center gap-2 text-sm font-semibold text-brand-strong">
              <Sparkles className="size-4" /> The built-in assistant
            </p>
            <h3 className="mt-3 text-2xl font-semibold tracking-tight text-balance">
              Ask in plain words. Every change waits for your Allow.
            </h3>
            <p className="mt-3 leading-relaxed text-muted-foreground">
              “Which days next week are empty?” “Draft three posts to fill them.” Each change shows up with Allow and
              Deny before it happens.
            </p>
          </div>
          <div className="flex flex-1 flex-col pl-7 sm:pl-9">
            <Shot
              name="assistant"
              alt="The AI Assistant panel beside the dashboard with suggestions such as 'Which days next week have nothing scheduled?' and 'Draft three posts to fill the empty days'"
              sizes="(min-width: 64rem) 540px, calc(100vw - 4rem)"
              crop="absolute inset-0 h-full object-[50%_30%]"
              className="flex min-h-80 flex-1 flex-col [&>picture]:relative [&>picture]:flex-1 rounded-r-none rounded-b-none border-r-0 border-b-0 sm:rounded-r-none sm:rounded-b-none"
            />
          </div>
        </article>
        <article className="sq-reveal flex flex-col rounded-3xl border border-border bg-card p-7 sm:p-9">
          <p className="text-sm font-semibold text-brand-strong">Your own agent</p>
          <h3 className="mt-3 text-2xl font-semibold tracking-tight text-balance">
            Claude Code, Codex, Cursor or n8n, connected in one line.
          </h3>
          <p className="mt-3 leading-relaxed text-muted-foreground">
            It writes posts, puts them on the calendar and refills a collection when a schedule runs dry. Let it write
            but not publish, and nothing goes out you didn’t schedule.
          </p>
          <div className="min-w-0 text-left">
            <ToolPreview agents />
          </div>
          <Link href="/agents" className={`mt-6 ${arrowLink} pointer-coarse:mt-3.5`}>
            Connect your agent <ArrowRight className="size-4" />
          </Link>
        </article>
      </div>
    </section>
  )
}

function Create() {
  return (
    <section className="border-y border-border bg-card/40">
      <div className={`${wrap} grid items-center gap-12 py-20 sm:py-28 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]`}>
        <div>
          <SectionHead eyebrow="Create" title="Make the picture and the video right where you post.">
            A design editor and a video editor run in your browser and save straight to your media library, ready for
            the next post.
          </SectionHead>
          <Bullets
            items={[
              'Templates for quotes, announcements, carousels and thumbnails, in a size for every channel',
              'Your brand kit: colours, fonts and logos for everyone on the team',
              'Remove a background on your own device; AI for new pictures, edits and upscaling',
              'Videos from your clips and pictures, with music and captions from speech',
            ]}
          />
          <a href={`${DOCS}/dashboard/design-editor`} className={`mt-7 ${arrowLink} pointer-coarse:mt-4.5`}>
            See the editors <ArrowRight className="size-4" />
          </a>
        </div>
        <div className="sq-reveal min-w-0">
          <Shot
            name="design-editor"
            alt="The design editor with the template list on the left and a 'Big news' announcement on the page"
            sizes="(min-width: 64rem) 660px, calc(100vw - 2.5rem)"
          />
        </div>
      </div>
    </section>
  )
}

function Duo() {
  return (
    <section className={`${wrap} py-20 sm:py-28`}>
      <div className="grid gap-6 lg:grid-cols-2 [&>*]:min-w-0">
        <article className="sq-reveal flex flex-col overflow-hidden rounded-3xl border border-border bg-card">
          <div className="p-7 sm:p-9">
            <Eyebrow>Share a preview</Eyebrow>
            <h3 className="mt-3 text-2xl font-semibold tracking-tight text-balance">
              Get the client’s OK before it goes out.
            </h3>
            <p className="mt-3 leading-relaxed text-muted-foreground">
              Send a link that shows the post as each channel will. Anyone can comment on a word or the whole post, no
              account needed, and you revoke the link when you’re done.
            </p>
          </div>
          <div className="mt-auto pl-7 sm:pl-9">
            <Shot
              name="preview-link"
              alt="A shared preview: the post as Bluesky, X, LinkedIn and a newsletter will show it, with a client's comment on the words '120 new components'"
              sizes="(min-width: 64rem) 540px, calc(100vw - 4rem)"
              className="max-h-96 rounded-r-none rounded-b-none border-r-0 border-b-0 sm:rounded-r-none sm:rounded-b-none"
            />
          </div>
        </article>
        <article className="sq-reveal flex flex-col overflow-hidden rounded-3xl border border-border bg-card">
          <div className="p-7 sm:p-9">
            <Eyebrow>Analytics</Eyebrow>
            <h3 className="mt-3 text-2xl font-semibold tracking-tight text-balance">
              See what went out, and what worked.
            </h3>
            <p className="mt-3 leading-relaxed text-muted-foreground">
              Posts published, success rate, failures and their reasons, views and engagements per channel, and the
              hours your posts do best.
            </p>
          </div>
          <div className="mt-auto pl-7 sm:pl-9">
            <Shot
              name="analytics"
              alt="Analytics for 30 days: 21 posts published, 96.8% success rate, 2 failures, 16 upcoming, a chart of published and failed per day and a table per channel"
              sizes="(min-width: 64rem) 540px, calc(100vw - 4rem)"
              className="max-h-96 rounded-r-none rounded-b-none border-r-0 border-b-0 sm:rounded-r-none sm:rounded-b-none"
            />
          </div>
        </article>
      </div>
    </section>
  )
}

function Channels() {
  return (
    <section id="channels" className="scroll-mt-16 border-y border-border bg-card/40">
      <div className={`${wrap} py-20 sm:py-28`}>
        <SectionHead eyebrow="Channels" title="Every platform your audience is on.">
          Connect an account once, by signing in or pasting a token, and pick it on any post. Anything not on the list
          goes through your own n8n workflow or script, on the same calendar.
        </SectionHead>
        {/* Icons only; hovering one grows it into a card with the name, over its neighbours so the row never reflows.
            Touch screens can't hover: there every card shows its name. */}
        <ul className="mt-10 flex flex-wrap gap-3 sm:mt-12">
          {CHANNELS.map(([id, name]) => (
            <li key={id} className="relative size-14 hover:z-10 pointer-coarse:size-auto">
              <div className="group absolute top-0 left-0 flex h-14 items-center rounded-2xl border border-[color-mix(in_oklch,var(--brand)_40%,transparent)] bg-card px-3 font-semibold tracking-tight transition-shadow hover:shadow-lg pointer-coarse:static">
                <ProviderTile id={id} name={name} className="size-8 shrink-0 rounded-lg" />
                <span
                  aria-hidden
                  className="max-w-0 overflow-hidden whitespace-nowrap opacity-0 transition-all duration-300 ease-out group-hover:ml-3 group-hover:max-w-48 group-hover:opacity-100 motion-reduce:transition-none pointer-coarse:ml-3 pointer-coarse:max-w-48 pointer-coarse:opacity-100"
                >
                  {name}
                </span>
              </div>
            </li>
          ))}
        </ul>
        <a href={`${DOCS}/channels`} className={`mt-6 ${arrowLink} pointer-coarse:mt-3.5`}>
          All channels and what each supports <ArrowRight className="size-4" />
        </a>
      </div>
    </section>
  )
}

const safety: [string, string][] = [
  [
    'Writing is not publishing',
    'A drafting agent gets permission to write and fills collections. Only your schedules, you, or a key you allow to publish put a word on your channels.',
  ],
  [
    'Credentials stay sealed',
    'Platform logins are encrypted with a key that stays on the server Staqly runs on, and no API call ever returns them.',
  ],
  [
    'Every post on record',
    'Who wrote it, when it was scheduled, which channel published it with which link, and why one failed.',
  ],
  [
    'Scoped keys, signed webhooks',
    'Each API key sees one workspace and only the permissions you gave it; your automations can verify every event.',
  ],
]

function Trust() {
  return (
    <section id="open-source" className={`${wrap} scroll-mt-16 py-20 sm:py-28`}>
      <SectionHead eyebrow="Safe by design" title="Your accounts, your keys, your say.">
        Nothing goes out that is not on your calendar, and your logins stay encrypted.
      </SectionHead>
      <dl className="mt-12 grid gap-x-8 gap-y-8 sm:grid-cols-2 lg:grid-cols-4">
        {safety.map(([t, b]) => (
          <div key={t} className="border-l-2 border-brand/60 pl-5">
            <dt className="font-semibold tracking-tight">{t}</dt>
            <dd className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{b}</dd>
          </div>
        ))}
      </dl>
      <div className="sq-reveal mt-16 grid gap-10 rounded-3xl border border-dashed border-border p-7 sm:p-10 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:items-center">
        <div>
          <Eyebrow>Open source</Eyebrow>
          <h3 className="mt-3 text-2xl font-semibold tracking-tight">AGPL-3.0, self-hostable, no lock-in.</h3>
          <p className="mt-3 leading-relaxed text-muted-foreground">
            The whole app is open source. Prefer your own machine? Run the free Community Edition: one binary or one
            Docker container, no plan limits, your data on your disk.
          </p>
          <div className="mt-6 flex flex-wrap gap-x-6 gap-y-2">
            <a href={`${DOCS}/quickstart`} className={arrowLink}>
              Community Edition quickstart <ArrowRight className="size-4" />
            </a>
            <a href={GITHUB} className={arrowLink}>
              Source on GitHub <ArrowRight className="size-4" />
            </a>
          </div>
        </div>
        <TerminalCard
          code={`$ ${INSTALL}
$ staqly service install   # runs in the background, starts with your computer`}
        />
      </div>
    </section>
  )
}

function Explore() {
  return (
    <section className="border-y border-border bg-card/40">
      <div className={`${wrap} py-20 sm:py-28`}>
        <div className="flex flex-wrap items-end justify-between gap-4">
          <SectionHead eyebrow="Use cases" title="See it applied." />
          <Link href="/use-cases" className={arrowLink}>
            All use cases <ArrowRight className="size-4" />
          </Link>
        </div>
        <div className="mt-10">
          <UseCaseCards items={allCases.slice(0, 3)} />
        </div>

        <div className="mt-24 flex flex-wrap items-end justify-between gap-4">
          <SectionHead eyebrow="Free tools" title="Try a piece of it now, no sign-up." />
          <Link href="/tools" className={arrowLink}>
            All free tools <ArrowRight className="size-4" />
          </Link>
        </div>
        <div className="mt-10">
          {/* Six fill two rows; the rest are a click away. */}
          <ToolCards items={TOOLS.slice(0, 6)} />
        </div>
      </div>
    </section>
  )
}

const faqs: [string, string][] = [
  [
    'What is Staqly, in one sentence?',
    'A social media scheduler: you (or your AI) write and design posts once, pick the channels and a time or a weekly rhythm, and Staqly publishes them to every platform you connected, retries failures and keeps the links.',
  ],
  [
    'Which platforms can it publish to?',
    "X, LinkedIn, Instagram, Facebook, Threads, TikTok, YouTube, Pinterest, Bluesky, Mastodon, Telegram, Discord, Slack, Reddit, WordPress, Medium and more, directly. Anything else goes through your own automation: an n8n workflow or a script that publishes the post and sends the link back. The composer knows each platform's limits and settings.",
  ],
  [
    'Can I use it without an AI agent?',
    'Yes. The dashboard has a composer, a calendar you drag posts around in, per-channel previews, a media library and design and video editors. The assistant and agents are optional.',
  ],
  [
    'Does AI publish without me?',
    'Only if you let it. The built-in assistant asks you to Allow every change. For your own agents, writing and publishing are separate permissions: a drafting agent fills collections, and only your schedules, you or a key with the publish permission put a post on your channels.',
  ],
  [
    'What happens when a platform is down?',
    'Publishing is a queued job. A failed channel retries on its own, the others are not held back, and you get an alert with the platform’s error and a Retry for that channel.',
  ],
  [
    'What does it cost?',
    `Plans are per user and start at €15 a month (€12 billed yearly), excl. VAT; each plan sets how many channels, new posts a month, AI allowance and storage the workspace shares. Starter has a ${TRIAL_DAYS}-day free trial. The AI assistant, the editors, MCP and the API come with every plan.`,
  ],
  [
    'What makes it different from other schedulers?',
    'It is built for agents as much as for people: Claude Code, Codex or n8n write posts over MCP or REST, collections carry their own schedules, and when a collection runs dry the schedule asks your agent for more. It is also open source and runs on your own machine if you want.',
  ],
  [
    'Can I host it myself?',
    'Yes. Staqly is AGPL-3.0: one binary or one Docker container, no plan limits, your data on your disk. It runs one workspace and prints an access token at start for your browser and agents; OAuth platforms such as X need your own developer app. The hosted version saves you backups and updates.',
  ],
]

function Faq() {
  const more: [string, string, string][] = [
    ['Docs', DOCS, 'Setup, every feature, the API and MCP'],
    ['Pricing', '/pricing', 'Plans, the trial and the Community Edition'],
    ['GitHub', GITHUB, 'The source, issues and releases'],
  ]

  return (
    <section id="faq" className="scroll-mt-16 border-t border-border bg-card/40">
      <div className={`${wrap} grid gap-12 py-20 sm:py-28 lg:grid-cols-[minmax(0,0.75fr)_minmax(0,1.25fr)] lg:gap-16`}>
        <div className="lg:sticky lg:top-28 lg:self-start">
          <SectionHead eyebrow="FAQ" title="Questions, answered.">
            What people ask before they connect their first channel.
          </SectionHead>
          <ul className="mt-8 grid gap-2">
            {more.map(([label, href, what]) => (
              <li key={label}>
                <a
                  href={href}
                  className="group flex items-center justify-between gap-4 rounded-xl border border-border bg-background/60 px-4 py-3 transition-colors hover:border-[color-mix(in_oklch,var(--brand)_40%,transparent)]"
                >
                  <span>
                    <span className="block text-sm font-semibold">{label}</span>
                    <span className="block text-sm text-muted-foreground">{what}</span>
                  </span>
                  <ArrowRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-brand" />
                </a>
              </li>
            ))}
          </ul>
        </div>
        <div className="grid gap-3">
          {faqs.map(([q, a], i) => (
            <details
              key={q}
              open={i === 0}
              className="group rounded-2xl border border-border bg-background transition-colors open:border-[color-mix(in_oklch,var(--brand)_35%,transparent)] open:bg-card"
            >
              <summary className="flex min-h-14 cursor-pointer list-none items-center gap-4 px-5 py-4 sm:px-6 [&::-webkit-details-marker]:hidden">
                <span className="font-mono text-xs text-muted-foreground group-open:text-brand">
                  {String(i + 1).padStart(2, '0')}
                </span>
                <span className="flex-1 text-[17px] font-medium tracking-tight">{q}</span>
                <span className="flex size-8 shrink-0 items-center justify-center rounded-full border border-border text-muted-foreground transition group-open:rotate-45 group-open:border-transparent group-open:bg-brand group-open:text-background">
                  <Plus className="size-4" />
                </span>
              </summary>
              <p className="-mt-1 px-5 pb-6 pl-[3.25rem] leading-relaxed text-pretty text-muted-foreground sm:px-6 sm:pl-[3.5rem]">
                {a}
              </p>
            </details>
          ))}
        </div>
      </div>
    </section>
  )
}

// Structured data for search engines: who publishes the site, the product and its plans (per user a month, excl. VAT,
// as on /pricing and in apps/api/internal/app/plans.go). No ratings or reviews: there are none to show.
const jsonLd = {
  '@context': 'https://schema.org',
  '@graph': [
    ...SITE_LD,
    {
      '@type': 'SoftwareApplication',
      '@id': `${SITE_URL}/#software`,
      name: 'staqly',
      description: DESCRIPTION,
      url: SITE_URL,
      applicationCategory: 'BusinessApplication',
      applicationSubCategory: 'Social media scheduler',
      operatingSystem: 'Web, macOS, Linux',
      license: 'https://www.gnu.org/licenses/agpl-3.0.html',
      isAccessibleForFree: false,
      publisher: { '@id': `${SITE_URL}/#organization` },
      sameAs: [GITHUB],
      offers: PLANS.map((p) => ({
        '@type': 'Offer',
        name: p.name,
        url: `${SITE_URL}/pricing`,
        price: String(p.month),
        priceCurrency: 'EUR',
        priceSpecification: {
          '@type': 'UnitPriceSpecification',
          price: String(p.month),
          priceCurrency: 'EUR',
          unitText: 'per user per month',
          billingDuration: 'P1M',
          valueAddedTaxIncluded: false,
        },
      })),
    },
    {
      '@type': 'FAQPage',
      mainEntity: faqs.map(([q, a]) => ({
        '@type': 'Question',
        name: q,
        acceptedAnswer: { '@type': 'Answer', text: a },
      })),
    },
  ],
}

export const metadata: Metadata = {
  ...pageMeta(HOME_TITLE, DESCRIPTION, '/'),
  title: { absolute: HOME_TITLE },
}

export default function HomePage() {
  return (
    <main id="main" className="flex flex-1 flex-col">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: ldJson(jsonLd) }} />
      <Hero />
      <ChannelStrip />
      <Audiences />
      <HowItWorks />
      <Ai />
      <Create />
      <Duo />
      <Channels />
      <Trust />
      <Explore />
      <Faq />
    </main>
  )
}
