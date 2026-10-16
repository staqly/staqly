import { HeroDemo } from '@staqly/brand/hero-demo'
import {
  ArrowRight,
  BellRing,
  History,
  Hourglass,
  Inbox,
  MousePointerClick,
  Radar,
  Send,
  ShieldCheck,
  Sparkles,
  SquareTerminal,
  Webhook,
} from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { PricingCards } from '@/components/pricing'
import { Eyebrow, H2 } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { UseCaseCards } from '@/components/use-case-cards'
import {
  APP,
  DESCRIPTION,
  DOCS,
  GITHUB,
  INSTALL,
  MCP_URL,
  SIGNUP,
  SITE_URL,
  btnPrimary,
  btnSecondary,
  wrap,
  SELF_HOST,
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
            <span className="size-1.5 rounded-full bg-brand" /> Human sign-off for AI agents
          </p>
          <h1 className="text-[2.6rem] leading-[1.04] font-semibold tracking-[-0.045em] text-balance sm:text-6xl lg:text-[4.1rem]">
            Let your agents create. <span className="sq-gradient-text">Ship only what you approve.</span>
          </h1>
          <p className="mt-6 max-w-xl text-lg leading-relaxed text-pretty text-muted-foreground">
            staqly is the review inbox between your AI agents and the real world. Every post, image, report or risky
            action lands in one place — you approve, edit or reject it from your desktop, phone or chat, and your agents
            act only on what you said yes to.
          </p>
          <div className="mt-9 flex flex-wrap items-center gap-3">
            <a href={SIGNUP} className={btnPrimary}>
              Create free workspace <ArrowRight className="size-4" />
            </a>
            <Link href="/#how" className={btnSecondary}>
              See how it works
            </Link>
          </div>
          <p className="mt-4 text-sm text-muted-foreground">
            Free: 50 approvals a month · no card · 7-day Pro trial ·{' '}
            <a href={SELF_HOST} className="underline-offset-4 hover:text-foreground hover:underline">
              or self-host
            </a>
          </p>
          <div className="mt-8 flex flex-wrap gap-2 text-xs font-medium text-muted-foreground">
            {['MCP + REST API', 'Hosted, ready in a minute', 'Open source, Apache-2.0'].map((b) => (
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
  const tools = ['Claude Code', 'Codex', 'OpenCode', 'Cursor', 'n8n', 'REST API', 'TypeScript SDK', 'Python SDK', 'CLI']
  const channels = ['Telegram', 'Slack', 'Discord', 'ntfy', 'Email']

  return (
    <section className={`${wrap} py-12 text-center`}>
      <p className="text-sm text-muted-foreground">Works with the agents and tools you already use</p>
      <div className="mt-5 flex flex-wrap justify-center gap-2">
        {tools.map((t) => (
          <span key={t} className="rounded-full border border-border bg-card px-3.5 py-1.5 text-sm font-medium">
            {t}
          </span>
        ))}
      </div>
      <p className="mt-6 text-sm text-muted-foreground">
        Approve straight from{' '}
        {channels.map((c, i) => (
          <span key={c}>
            <span className="font-medium text-foreground">{c}</span>
            {i < channels.length - 2 ? ', ' : i === channels.length - 2 ? ' or ' : ''}
          </span>
        ))}
      </p>
    </section>
  )
}

function HowItWorks() {
  const steps: { icon: ReactNode; title: string; body: ReactNode }[] = [
    {
      icon: <Send />,
      title: 'Agents send',
      body: 'Claude Code, Codex, n8n or a script sends what it made — a post, an image, a summary — over MCP or REST, into a collection.',
    },
    {
      icon: <Inbox />,
      title: 'You review',
      body: 'Approve, reject with feedback, or edit before approving. Keyboard-first on desktop, one tap on your phone or in chat.',
    },
    {
      icon: <Webhook />,
      title: 'staqly tells',
      body: (
        <>
          Agents waiting on <code className="rounded bg-muted px-1 font-mono text-[0.85em]">await_decision</code> get
          your answer. Signed webhooks fire, and every decision lands in the history.
        </>
      ),
    },
  ]

  return (
    <section id="how" className="scroll-mt-16 border-y border-border bg-card/50">
      <div className={`${wrap} py-14 sm:py-20`}>
        <div className="max-w-2xl">
          <Eyebrow>How it works</Eyebrow>
          <H2>Send. Review. Act.</H2>
          <p className="mt-4 text-muted-foreground">
            staqly never publishes anything itself. It sits between what your agents make and what your automations do
            with it — so nothing ships until a human says so.
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
          <img
            src="/screenshots/inbox-dark.png"
            alt="staqly inbox: items waiting for review, each with its summary, tags, collection, status and Reject / Approve buttons"
            // Intrinsic size reserves the space (no layout shift); it is below the fold, so it loads late.
            width={1800}
            height={1125}
            loading="lazy"
            decoding="async"
            className="h-auto w-full"
          />
        </div>
      </div>
    </section>
  )
}

const features: { icon: ReactNode; title: string; body: string; href: string }[] = [
  {
    icon: <Inbox />,
    title: 'One inbox for every agent',
    body: 'Images, video, text, links and JSON from every agent and script — reviewed in one place, mostly from the keyboard.',
    href: '/dashboard/inbox',
  },
  {
    icon: <ShieldCheck />,
    title: 'Human approval gates',
    body: 'Agents propose, wait for your decision and act only on what you approved — with edits, expiry and a reported result.',
    href: '/cookbook/approval-gate',
  },
  {
    icon: <Hourglass />,
    title: 'Expiry',
    body: 'Give a request a deadline. If nobody decides in time it turns expired, so a stale ask never runs late.',
    href: '/concepts#status-lifecycle',
  },
  {
    icon: <Radar />,
    title: 'Watchers',
    body: 'Pull in fresh content from any RSS/Atom feed or web page as pending items.',
    href: '/automation/watchers',
  },
  {
    icon: <BellRing />,
    title: 'Decide from anywhere',
    body: 'Approve or reject from Telegram, Slack, Discord, ntfy or email — no dashboard needed.',
    href: '/automation/notifications',
  },
  {
    icon: <Sparkles />,
    title: 'Summaries and tags',
    body: 'Your own Claude Code or Codex writes a one-line summary and up to three tags for each new item, so the inbox reads at a glance. Hosted: done for you.',
    href: '/self-hosting/configuration#auto-tags-and-summaries',
  },
  {
    icon: <MousePointerClick />,
    title: 'Your own buttons',
    body: 'Add Publish, Regenerate or Translate to a collection. A click sends a signed webhook to your automation and the item shows whether the run completed or failed.',
    href: '/automation/custom-actions',
  },
  {
    icon: <SquareTerminal />,
    title: 'Gates for scripts and CI',
    body: 'staqly push --wait blocks a script or a pipeline until someone decides: exit 0 when approved, 2 when rejected, 3 when time runs out.',
    href: '/cookbook/ci-approval-gate',
  },
  {
    icon: <History />,
    title: 'Full history',
    body: 'Every version and every decision is recorded. Back up, export everything or purge an item for good.',
    href: '/dashboard/activity',
  },
]

function Features() {
  return (
    <section id="features" className={`${wrap} scroll-mt-16 py-14 sm:py-20`}>
      <div className="max-w-2xl">
        <Eyebrow>Features</Eyebrow>
        <H2>Built for what agents actually make.</H2>
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
            Create a workspace, copy your API key and add staqly to your agent over MCP. It can then create collections,
            save what it makes and wait for your call.
          </p>
          <ul className="mt-7 space-y-2.5">
            {[
              'Create a collection called Instagram Avatar and add this image to it.',
              "Show me everything that's waiting for my review.",
              'Wait until I approve this post, then publish it.',
            ].map((p) => (
              <li
                key={p}
                className="rounded-r-xl border-l-2 border-brand bg-muted px-4 py-2.5 text-sm italic text-muted-foreground"
              >
                “{p}”
              </li>
            ))}
          </ul>
          <Link
            href="/agents"
            className="mt-7 inline-flex items-center gap-1.5 text-sm font-semibold text-brand-strong hover:underline"
          >
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
$ curl -X POST ${APP}/v1/collections/Drafts/items \\
    -H "Authorization: Bearer $STAQLY_API_KEY" \\
    -F file=@avatar.png -F 'item={"title":"Avatar #1"}'
→ 201 {"id":"itm_8f2...","status":"pending"}`}
        />
      </div>
    </section>
  )
}

const safety: { icon: ReactNode; title: string; body: string }[] = [
  {
    icon: <ShieldCheck />,
    title: 'Fail-closed by default',
    body: 'An agent waiting on a decision acts only on approved. Rejected, expired or timed out means it does nothing.',
  },
  {
    icon: <Inbox />,
    title: 'Edit before you approve',
    body: 'Fix the caption, the SQL or the email first. The agent gets exactly the version you approved.',
  },
  {
    icon: <History />,
    title: 'Every decision on record',
    body: 'Who approved what, when, with which edits and what happened next — kept with every item.',
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
        <H2>Built as a safety layer, not another bot.</H2>
        <p className="mt-4 text-muted-foreground">
          staqly never publishes or executes anything itself. It holds the queue and the decision.
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
        <Link
          href="/use-cases"
          className="inline-flex items-center gap-1.5 text-sm font-semibold text-brand-strong hover:underline"
        >
          All use cases <ArrowRight className="size-4" />
        </Link>
      </div>
      <div className="mt-10">
        <UseCaseCards items={allCases.slice(0, 3)} />
      </div>
    </section>
  )
}

function Pricing() {
  return (
    <section id="pricing" className="scroll-mt-16 border-y border-border bg-card/50">
      <div className={`${wrap} py-14 sm:py-20`}>
        <div className="mx-auto mb-12 max-w-2xl text-center">
          <Eyebrow>Pricing</Eyebrow>
          <H2>50 free approvals a month. Scale when you need to.</H2>
        </div>
        <PricingCards />
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
          <h2 className="mt-2 text-2xl font-semibold tracking-[-0.03em]">Apache-2.0, self-hostable, no lock-in.</h2>
          <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
            The whole app is open source. Prefer to run it on your own machine or server? It&apos;s one binary with no
            limits that runs in the background on macOS or Linux (or one Docker container) — you just take care of
            backups and updates yourself.
          </p>
          <a
            href={`${DOCS}/quickstart`}
            className="mt-6 inline-flex items-center gap-1.5 text-sm font-semibold text-brand-strong hover:underline"
          >
            Self-hosting quickstart <ArrowRight className="size-4" />
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

// Structured data for search engines: the product, its plans and where it lives.
const jsonLd = {
  '@context': 'https://schema.org',
  '@type': 'SoftwareApplication',
  name: 'staqly',
  description: DESCRIPTION,
  url: SITE_URL,
  applicationCategory: 'DeveloperApplication',
  operatingSystem: 'Web, macOS, Linux',
  license: 'https://www.apache.org/licenses/LICENSE-2.0',
  codeRepository: GITHUB,
  offers: [
    { '@type': 'Offer', name: 'Free', price: '0', priceCurrency: 'USD' },
    { '@type': 'Offer', name: 'Pro', price: '30', priceCurrency: 'USD' },
  ],
}

export default function HomePage() {
  return (
    <main className="flex flex-1 flex-col">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }} />
      <Hero />
      <Integrations />
      <HowItWorks />
      <ConnectAgent />
      <Features />
      <Safety />
      <UseCasesTeaser />
      <Pricing />
      <OpenSource />
    </main>
  )
}
