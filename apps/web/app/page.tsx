import { HeroDemo } from '@staqly/brand/hero-demo'
import { ProviderTile } from '@staqly/brand/providers'
import {
  ArrowRight,
  BellRing,
  Bot,
  CalendarClock,
  History,
  ListChecks,
  PenLine,
  Plug,
  Plus,
  Repeat,
  Send,
  ShieldCheck,
  SquareTerminal,
  Webhook,
} from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { Eyebrow, H2 } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { ToolPreview } from '@/components/tool-preview'
import { ToolCards } from '@/components/tools/tool-cards'
import { UseCaseCards } from '@/components/use-case-cards'
import { PLANS } from '@/lib/plans'
import { HOME_TITLE, SITE_LD, ldJson, pageMeta } from '@/lib/seo'
import {
  APP,
  DESCRIPTION,
  DOCS,
  GITHUB,
  INSTALL,
  MCP_URL,
  SIGNUP,
  SITE_URL,
  arrowLink,
  btnPrimary,
  btnSecondary,
  wrap,
} from '@/lib/site'
import { allCases } from '@/lib/use-cases'

function Hero() {
  return (
    <section className="relative isolate overflow-hidden border-b border-border">
      <div aria-hidden className="sq-grid-bg absolute inset-0 -z-10" />
      <div
        aria-hidden
        className="sq-glow absolute -top-40 left-1/2 -z-10 h-[28rem] w-[min(56rem,140vw)] -translate-x-1/2"
      />
      <div
        className={`${wrap} grid items-center gap-12 pt-16 pb-16 sm:pt-24 lg:grid-cols-[1.05fr_1fr] lg:gap-10 lg:pb-24`}
      >
        <div className="min-w-0">
          <p className="mb-6 inline-flex items-center gap-2 rounded-full border border-border bg-card/80 px-3 py-1 text-xs font-medium text-muted-foreground backdrop-blur">
            <span className="size-1.5 rounded-full bg-brand" /> The post scheduler for people and their agents
          </p>
          <h1 className="text-[2.6rem] leading-[1.04] font-semibold tracking-[-0.045em] text-balance sm:text-6xl lg:text-[4.1rem]">
            Your agents write the posts. <span className="sq-gradient-text">Staqly keeps the rhythm.</span>
          </h1>
          <p className="mt-6 max-w-xl text-lg leading-relaxed text-pretty text-muted-foreground">
            Plan a month of posts in one sitting with Claude Code, Codex or n8n. Staqly publishes each one when your
            audience is online: on X today, with Instagram, TikTok and YouTube on the way.
          </p>
          <div className="mt-9 flex flex-wrap items-center gap-3">
            <a href={SIGNUP} className={btnPrimary}>
              Start free trial <ArrowRight className="size-4" />
            </a>
            <Link href="/#how" className={btnSecondary}>
              See how it works
            </Link>
          </div>
          <div className="mt-8 flex flex-wrap gap-2 text-xs font-medium text-muted-foreground">
            {['MCP + REST API', 'Hosted, ready in a minute', 'Open source, AGPL-3.0'].map((b) => (
              <span key={b} className="rounded-md border border-border bg-card px-2.5 py-1">
                {b}
              </span>
            ))}
          </div>
        </div>
        <HeroDemo />
      </div>
    </section>
  )
}

function Integrations() {
  return (
    <section className={`${wrap} py-12 text-center sm:py-16`}>
      <p className="text-sm text-muted-foreground">Works with the agents and tools you already use</p>
      <ToolPreview />
    </section>
  )
}

// From the provider catalog (apps/api/internal/providers): [id, name]. Available: the ones registered with
// StatusAvailable (x.go, custom.go); every other catalog entry is "soon" and is never shown as available.
const channelsLive: [string, string][] = [
  ['x', 'X'],
  ['custom', 'Your own automation'],
]
// Not built yet (catalog.go), the creator platforms first.
const channelsSoon: [string, string][] = [
  ['instagram', 'Instagram'],
  ['tiktok', 'TikTok'],
  ['youtube', 'YouTube'],
  ['threads', 'Threads'],
  ['facebook', 'Facebook'],
  ['pinterest', 'Pinterest'],
  ['linkedin', 'LinkedIn'],
  ['bluesky', 'Bluesky'],
  ['mastodon', 'Mastodon'],
  ['telegram', 'Telegram'],
  ['discord', 'Discord'],
  ['reddit', 'Reddit'],
  ['twitch', 'Twitch'],
  ['kick', 'Kick'],
  ['tumblr', 'Tumblr'],
  ['google_business', 'Google Business'],
  ['wordpress', 'WordPress'],
  ['medium', 'Medium'],
  ['devto', 'Dev.to'],
]

function Channels() {
  return (
    <section id="channels" className={`${wrap} scroll-mt-16 py-14 sm:py-20`}>
      <div className="max-w-2xl">
        <Eyebrow>Channels</Eyebrow>
        <H2>X today. Any platform through your automation.</H2>
        <p className="mt-4 text-muted-foreground">
          Connect X once and every post can go there, with its own text and the platform&apos;s limits checked before it
          leaves. Anything else goes through your own automation: Staqly hands the post to an n8n workflow or a script
          and records the link it sends back. The platforms marked Soon are not available yet; the composer already
          knows their limits and settings, so posts for them can be prepared.
        </p>
      </div>
      <ul className="mt-8 grid grid-cols-2 gap-3 sm:mt-12 sm:grid-cols-3 lg:grid-cols-4">
        {channelsLive.map(([id, name]) => (
          <li
            key={id}
            className="flex h-14 min-w-0 items-center gap-3 rounded-2xl border border-[color-mix(in_oklch,var(--brand)_40%,transparent)] bg-card px-4 font-semibold tracking-tight"
          >
            <ProviderTile id={id} name={name} className="size-8 shrink-0 rounded-lg" />
            <span className="truncate">{name}</span>
          </li>
        ))}
        {channelsSoon.map(([id, name]) => (
          <li
            key={id}
            className="flex h-14 min-w-0 items-center gap-3 rounded-2xl border border-dashed border-border px-4 font-semibold tracking-tight text-muted-foreground"
          >
            <ProviderTile id={id} name={name} className="size-8 shrink-0 rounded-lg" />
            <span className="truncate">{name}</span>
            <span className="ml-auto shrink-0 rounded-full border border-border px-2 py-0.5 text-[11px] font-medium">
              Soon
            </span>
          </li>
        ))}
      </ul>
      <a href={`${DOCS}/channels`} className={`mt-6 ${arrowLink} pointer-coarse:mt-3.5`}>
        All channels <ArrowRight className="size-4" />
      </a>
    </section>
  )
}

function HowItWorks() {
  const steps: { icon: ReactNode; title: string; body: ReactNode }[] = [
    {
      icon: <PenLine />,
      title: 'Write',
      body: 'You, Claude Code, Codex, n8n or a script write posts into a collection, over MCP or REST: text, media and a version per channel.',
    },
    {
      icon: <CalendarClock />,
      title: 'Schedule',
      body: 'Give a post a time, or let the collection do it: weekdays at 09:00, every six hours, the next waiting post or all of them.',
    },
    {
      icon: <Send />,
      title: 'Staqly publishes',
      body: (
        <>
          A durable job queue publishes every channel, retries failures and records the link. When a collection runs
          dry, the schedule asks your agent with{' '}
          <code className="rounded bg-muted px-1 font-mono text-[0.85em]">list_requested_work</code>.
        </>
      ),
    },
  ]

  return (
    <section id="how" className="scroll-mt-16 border-y border-border bg-card/50">
      <div className={`${wrap} py-14 sm:py-20`}>
        <div className="max-w-2xl">
          <Eyebrow>How it works</Eyebrow>
          <H2>Write. Schedule. Publish.</H2>
          <p className="mt-4 text-muted-foreground">
            Posts live in collections, collections have schedules, schedules publish to channels. Everything else, from
            per-channel text to retries and alerts, follows from that.
          </p>
        </div>
        <ol className="mt-8 sm:mt-12 grid gap-4 md:grid-cols-3">
          {steps.map((s, i) => (
            <li key={s.title} className="rounded-2xl border border-border bg-background p-6">
              <div className="flex items-center justify-between">
                <span className="flex size-10 items-center justify-center rounded-xl border border-border bg-card text-brand [&_svg]:size-5">
                  {s.icon}
                </span>
                <span className="font-mono text-sm text-muted-foreground">0{i + 1}</span>
              </div>
              <h3 className="mt-5 font-semibold tracking-tight">{s.title}</h3>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{s.body}</p>
            </li>
          ))}
        </ol>
        <div className="mt-8 sm:mt-12 overflow-hidden rounded-2xl border border-border shadow-[0_40px_100px_-40px_rgb(0_0_0/0.45)]">
          {/* AVIF, then WebP, at the width the screen needs (a phone gets ~10 KB instead of the 64 KB JPEG). */}
          <picture>
            <source type="image/avif" srcSet={screenshotSet('avif')} sizes={screenshotSizes} />
            <source type="image/webp" srcSet={screenshotSet('webp')} sizes={screenshotSizes} />
            <img
              src="/screenshots/schedule-dark.jpg"
              alt="The staqly calendar: scheduled posts by day and hour with their channels, next to the upcoming schedule runs"
              // Intrinsic size reserves the space (no layout shift); it is below the fold, so it loads late.
              width={1456}
              height={816}
              loading="lazy"
              decoding="async"
              className="h-auto w-full"
            />
          </picture>
        </div>
      </div>
    </section>
  )
}

// The calendar screenshot's widths (public/screenshots/schedule-dark-<width>.<format>, made from the JPEG with ffmpeg
// libsvtav1 at crf 24 and cwebp -q 90 -sharp_yuv) and the width it shows at: the wrap minus its padding.
const screenshotSet = (format: 'avif' | 'webp') =>
  [720, 1080, 1456].map((w) => `/screenshots/schedule-dark-${w}.${format} ${w}w`).join(', ')
const screenshotSizes = '(min-width: 72rem) 1088px, (min-width: 40rem) calc(100vw - 4rem), calc(100vw - 2.5rem)'

const features: { icon: ReactNode; title: string; body: string; href: string }[] = [
  {
    icon: <CalendarClock />,
    title: 'A calendar, not a queue',
    body: 'Day, week, month and list views. Drag a post to another slot, add one into a free hour, see the upcoming schedule runs next to the posts.',
    href: '/dashboard/schedule',
  },
  {
    icon: <Repeat />,
    title: 'Schedules per collection',
    body: 'Weekdays at 09:00, every six hours, once on Friday. The next waiting post, chosen posts or all of them. Time zones that survive daylight saving.',
    href: '/automation/schedules',
  },
  {
    icon: <Bot />,
    title: 'Agents that refill the queue',
    body: 'When a collection runs dry, the schedule asks your agent for new posts, or calls an endpoint of yours that returns them.',
    href: '/cookbook/agent-fills-a-collection',
  },
  {
    icon: <PenLine />,
    title: 'One post, every channel',
    body: 'A text per channel where it matters, a character counter per platform and a preview that says what each one objects to before anything goes out.',
    href: '/dashboard/posts',
  },
  {
    icon: <Plug />,
    title: 'Your own automation as a channel',
    body: 'Hand a post to n8n or a script, get the link back. Any platform, any internal tool, same calendar and same history.',
    href: '/channels/your-own-automation',
  },
  {
    icon: <ListChecks />,
    title: 'A queue you can see',
    body: 'Publishing and schedule runs are durable jobs with retries. Settings shows what is queued, running or failed, with a Retry.',
    href: '/concepts#jobs',
  },
  {
    icon: <BellRing />,
    title: 'Alerts where you are',
    body: 'Telegram, Slack, Discord, ntfy or email when a post goes out or fails, when a channel needs reconnecting or a schedule asks for more.',
    href: '/automation/alerts',
  },
  {
    icon: <SquareTerminal />,
    title: 'CLI, SDKs, REST, MCP',
    body: 'staqly posts create from cron, @staqly/sdk and the Python client in your code, 100 MCP tools for your agent, one OpenAPI contract behind all of it.',
    href: '/cli',
  },
  {
    icon: <History />,
    title: 'Full history',
    body: 'Every post, channel and schedule change is an event. Signed webhooks deliver them; back up, export everything or purge a post for good.',
    href: '/dashboard/activity',
  },
]

function Features() {
  return (
    <section id="features" className={`${wrap} scroll-mt-16 py-14 sm:py-20`}>
      <div className="max-w-2xl">
        <Eyebrow>Features</Eyebrow>
        <H2>Built for posts that keep coming.</H2>
      </div>
      <div className="mt-8 sm:mt-12 grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-3">
        {features.map((f) => (
          <a
            key={f.title}
            href={DOCS + f.href}
            className="group flex flex-col gap-3 bg-card p-6 transition-colors hover:bg-accent/60 sm:p-7"
          >
            <span className="flex size-10 items-center justify-center rounded-xl border border-border bg-background text-brand [&_svg]:size-5">
              {f.icon}
            </span>
            <h3 className="font-semibold tracking-tight">{f.title}</h3>
            <p className="text-sm leading-relaxed text-muted-foreground">{f.body}</p>
            <span className="mt-auto inline-flex items-center gap-1 pt-2 text-sm font-medium text-muted-foreground transition-colors group-hover:text-brand-strong">
              Learn more <ArrowRight className="size-3.5" />
            </span>
          </a>
        ))}
      </div>
    </section>
  )
}

function ConnectAgent() {
  return (
    <section className="border-y border-border bg-card/50">
      <div className={`${wrap} grid gap-10 py-14 sm:py-20 lg:grid-cols-[0.9fr_1.1fr] lg:items-center`}>
        <div>
          <Eyebrow>Connect your agent</Eyebrow>
          <H2>One line. Then just ask.</H2>
          <p className="mt-4 text-muted-foreground">
            Create a workspace, copy your API key and add staqly to your agent over MCP. It can then write posts, put
            them on the calendar and refill a collection when a schedule asks.
          </p>
          <ul className="mt-7 space-y-2.5">
            {[
              'Draft five posts about the new release into Product updates, one per weekday morning.',
              'What is scheduled this week?',
              'Make the X version shorter and publish it now.',
            ].map((p) => (
              <li
                key={p}
                className="rounded-r-xl border-l-2 border-brand bg-muted px-4 py-2.5 text-sm italic text-muted-foreground"
              >
                “{p}”
              </li>
            ))}
          </ul>
          <Link href="/agents" className={`mt-7 ${arrowLink} pointer-coarse:mt-4.5`}>
            Setup for every agent <ArrowRight className="size-4" />
          </Link>
        </div>
        <TerminalCard
          code={`# Claude Code
$ claude mcp add --transport http staqly ${MCP_URL} \\
    --header "Authorization: Bearer $STAQLY_API_KEY"

# Codex
$ codex mcp add staqly --url ${MCP_URL} \\
    --bearer-token-env-var STAQLY_API_KEY

# or plain REST, from anything
$ curl -X POST ${APP}/v1/posts \\
    -H "Authorization: Bearer $STAQLY_API_KEY" -H 'Content-Type: application/json' \\
    -d '{"collection":"Default","content":"v1.4 is out.","targets":[{"channel_id":"ch_..."}],"publish_now":true}'
→ 201 {"id":"post_8f2...","status":"publishing"}`}
        />
      </div>
    </section>
  )
}

const safety: { icon: ReactNode; title: string; body: string }[] = [
  {
    icon: <ShieldCheck />,
    title: 'Credentials stay sealed',
    body: 'Platform tokens are encrypted with a key that stays on the server Staqly runs on, never returned by any API call, and used only there.',
  },
  {
    icon: <PenLine />,
    title: 'Writing is not publishing',
    body: 'A drafting agent gets posts:write and fills collections. Only your schedules, you or a publisher key with posts:publish put a word on your channels.',
  },
  {
    icon: <History />,
    title: 'Every post on record',
    body: 'Who wrote it, when it was scheduled, which channel published it with which link, and why one failed: kept with every post.',
  },
  {
    icon: <Webhook />,
    title: 'Signed webhooks, scoped keys',
    body: 'Automations can verify every event. Each API key sees one workspace and only the scopes you gave it.',
  },
]

function Safety() {
  return (
    <section className={`${wrap} py-14 sm:py-20`}>
      <div className="max-w-2xl">
        <Eyebrow>Why it&apos;s safe</Eyebrow>
        <H2>Your accounts, your keys, your say.</H2>
        <p className="mt-4 text-muted-foreground">
          Staqly holds the calendar and the credentials on the server it runs on, ours or yours when you self-host, and
          publishes exactly what is on it.
        </p>
      </div>
      <div className="mt-8 sm:mt-12 grid gap-4 sm:grid-cols-2">
        {safety.map((f) => (
          <div key={f.title} className="flex gap-4 rounded-2xl border border-border bg-card p-6">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-border bg-background text-brand [&_svg]:size-5">
              {f.icon}
            </span>
            <div>
              <h3 className="font-semibold tracking-tight">{f.title}</h3>
              <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{f.body}</p>
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

function UseCasesTeaser() {
  return (
    <section className={`${wrap} py-14 sm:py-20`}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="max-w-2xl">
          <Eyebrow>Use cases</Eyebrow>
          <H2>See it applied.</H2>
        </div>
        <Link href="/use-cases" className={arrowLink}>
          All use cases <ArrowRight className="size-4" />
        </Link>
      </div>
      <div className="mt-10">
        <UseCaseCards items={allCases.slice(0, 3)} />
      </div>
    </section>
  )
}

function ToolsTeaser() {
  return (
    <section className={`${wrap} pb-14 sm:pb-20`}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="max-w-2xl">
          <Eyebrow>Free tools</Eyebrow>
          <H2>Try it before you sign up.</H2>
        </div>
        <Link href="/tools" className={arrowLink}>
          All tools <ArrowRight className="size-4" />
        </Link>
      </div>
      <div className="mt-10">
        <ToolCards />
      </div>
    </section>
  )
}

function OpenSource() {
  return (
    <section id="open-source" className={`${wrap} scroll-mt-16 py-14 sm:py-20`}>
      <div className="grid gap-10 rounded-2xl border border-dashed border-border p-8 sm:p-10 lg:grid-cols-[0.9fr_1.1fr] lg:items-center">
        <div>
          <Eyebrow>Open source</Eyebrow>
          <h2 className="mt-2 text-2xl font-semibold tracking-[-0.03em]">AGPL-3.0, self-hostable, no lock-in.</h2>
          <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
            The whole app is open source. Prefer to run it on your own machine or server? Staqly Community Edition is
            one binary with no plan limits that runs in the background on macOS or Linux (or one Docker container): one
            workspace, opened with the access token it prints at start. You take care of backups and updates yourself.
          </p>
          <a href={`${DOCS}/quickstart`} className={`mt-6 ${arrowLink} pointer-coarse:mt-3.5`}>
            Community Edition quickstart <ArrowRight className="size-4" />
          </a>
        </div>
        <TerminalCard
          code={`$ ${INSTALL}
$ staqly service install   # runs in the background, starts with your computer`}
        />
      </div>
    </section>
  )
}

const faqs: [string, string][] = [
  [
    'What makes Staqly different from other post schedulers?',
    'It is built for agents as much as for people. Claude Code, Codex or n8n write posts over MCP or REST, collections carry their own schedules, and when a collection runs dry the schedule asks your agent for more. It is also open source and runs on your own machine if you want.',
  ],
  [
    'Which platforms can it publish to?',
    'Today X, and any other platform through your own automation: an n8n workflow or a script that publishes the post and sends the link back. Instagram, TikTok, YouTube, LinkedIn and the other platforms on the channel list are not available yet; the composer already knows their limits and settings, so posts for them can be prepared.',
  ],
  [
    'Can I use it without an AI agent?',
    'Yes. The dashboard has a composer, a calendar you can drag posts around in, and per-channel previews. Agents are optional.',
  ],
  [
    'Does an agent publish without me?',
    'Only if you let it. Writing and publishing are separate permissions: a drafting agent fills collections, and only your schedules, you or a key with the publish permission put a post on your channels.',
  ],
  [
    'What happens when a platform is down?',
    'Publishing is a queued job. A failed channel retries on its own, the others are not held back, and you get an alert with the platform’s error and a Retry for that channel.',
  ],
  [
    'Can I host it myself?',
    'Yes. Staqly is AGPL-3.0: one binary or one Docker container, no plan limits, your data on your disk. It runs one workspace and prints an access token at start for your browser and agents; OAuth platforms such as X need your own developer app. The hosted version saves you backups and updates.',
  ],
]

function Faq() {
  return (
    <section id="faq" className={`${wrap} scroll-mt-16 py-14 sm:py-20`}>
      <div className="mx-auto max-w-3xl">
        <Eyebrow>FAQ</Eyebrow>
        <H2>Questions, answered.</H2>
        <div className="mt-8 divide-y divide-border rounded-2xl border border-border bg-card sm:mt-12">
          {faqs.map(([q, a]) => (
            <details key={q} className="group px-6 py-1">
              <summary className="flex min-h-12 cursor-pointer list-none items-center justify-between gap-4 py-3 font-medium [&::-webkit-details-marker]:hidden">
                {q}
                <Plus className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-45" />
              </summary>
              <p className="pb-5 text-sm leading-relaxed text-muted-foreground">{a}</p>
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
      <Integrations />
      <HowItWorks />
      <Channels />
      <ConnectAgent />
      <Features />
      <Safety />
      <UseCasesTeaser />
      <ToolsTeaser />
      <OpenSource />
      <Faq />
    </main>
  )
}
