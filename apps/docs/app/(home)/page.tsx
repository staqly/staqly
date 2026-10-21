import { HeroDemo } from '@staqly/brand/hero-demo'
import {
  ArrowRight,
  BellRing,
  BookOpen,
  Bot,
  Braces,
  CalendarClock,
  Cloud,
  Cpu,
  HardDrive,
  PenLine,
  Plug,
  Repeat,
  Rocket,
  Smartphone,
  Terminal,
  Workflow,
} from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { LogoMark } from '@/components/logo'
import { env } from '@/lib/env.mjs'
import { DOCS_DESCRIPTION, SITE_JSON_LD, docsMeta, ldJson } from '@/lib/seo'
import { repoUrl } from '@/lib/shared'

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
    body: 'Weekdays at 09:00, every six hours, once on Friday. The next waiting post, chosen posts or all of them. When nothing waits, ask your agent or an endpoint.',
    href: '/automation/schedules',
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
    body: 'Hand a post to n8n or a script and get the link back. Any platform, any internal tool, same calendar and history.',
    href: '/channels/your-own-automation',
  },
  {
    icon: <BellRing />,
    title: 'Alerts and webhooks',
    body: 'Telegram, Slack, Discord, ntfy or email when a post goes out or fails; every event reaches your automations as a signed webhook.',
    href: '/automation/alerts',
  },
  {
    icon: <HardDrive />,
    title: 'Hosted or self-hosted',
    body: 'Staqly Cloud at app.staqly.ai for your team, or the Community Edition on your own machine: one Go process, one SQLite file, one access token.',
    href: '/local-vs-cloud',
  },
]

const sections: { title: string; icon: ReactNode; links: [string, string][] }[] = [
  {
    title: 'Get started',
    icon: <Rocket />,
    links: [
      ['What is staqly?', '/introduction'],
      ['Quickstart', '/quickstart'],
      ['Concepts', '/concepts'],
      ['Channels', '/channels'],
      ['Community Edition vs Staqly Cloud', '/local-vs-cloud'],
    ],
  },
  {
    title: 'Connect agents',
    icon: <Bot />,
    links: [
      ['Claude Code', '/agents/claude-code'],
      ['OpenAI Codex', '/agents/codex'],
      ['OpenCode', '/agents/opencode'],
      ['Cursor', '/agents/cursor'],
      ['Windsurf', '/agents/windsurf'],
      ['n8n', '/integrations/n8n'],
      ['Scripts & SDKs', '/agents/scripts'],
    ],
  },
  {
    title: 'Automate',
    icon: <Workflow />,
    links: [
      ['Schedules', '/automation/schedules'],
      ['Your own automation', '/channels/your-own-automation'],
      ['Webhooks', '/automation/webhooks'],
      ['Alerts', '/automation/alerts'],
    ],
  },
  {
    title: 'Build',
    icon: <Braces />,
    links: [
      ['API overview', '/api'],
      ['API reference', '/api/reference'],
      ['MCP tools', '/mcp/tools'],
      ['TypeScript SDK', '/sdk/typescript'],
      ['Python SDK', '/sdk/python'],
      ['staqly CLI', '/cli'],
    ],
  },
  {
    title: 'Self-host the Community Edition',
    icon: <Cpu />,
    links: [
      ['Run the binary', '/self-hosting/binary'],
      ['Run with Docker', '/self-hosting/docker'],
      ['Configuration', '/self-hosting/configuration'],
      ['Backup & restore', '/self-hosting/backup-and-restore'],
      ['Media storage', '/self-hosting/storage'],
    ],
  },
  {
    title: 'Cookbook',
    icon: <BookOpen />,
    links: [
      ['Let an agent fill a collection', '/cookbook/agent-fills-a-collection'],
      ['Publish through n8n', '/cookbook/publish-through-n8n'],
      ['Posts from your endpoint', '/cookbook/posts-from-your-endpoint'],
      ['Manage from your phone', '/cookbook/manage-from-your-phone'],
    ],
  },
]

function Hero() {
  return (
    <section className="relative isolate overflow-hidden border-b border-fd-border">
      <div aria-hidden className="sq-grid-bg absolute inset-0 -z-10" />
      <div
        aria-hidden
        className="sq-glow absolute -top-40 left-1/2 -z-10 h-[28rem] w-[min(56rem,140vw)] -translate-x-1/2"
      />
      <div className="mx-auto grid w-full max-w-7xl items-center gap-12 px-5 pt-12 pb-12 sm:px-8 sm:pt-16 lg:grid-cols-[1fr_1fr] lg:gap-12 lg:pb-16">
        <div className="min-w-0">
          <Link
            href="/cookbook/agent-fills-a-collection"
            className="group mb-7 inline-flex max-w-full items-center gap-2 rounded-full border border-fd-border bg-fd-card/80 py-1 pr-3 pl-1 text-xs font-medium text-fd-muted-foreground shadow-sm backdrop-blur transition-colors hover:text-fd-foreground"
          >
            <span className="rounded-full bg-brand-soft px-2 py-0.5 text-brand-strong">New</span>
            <span className="truncate">Let an agent fill a collection, publish every morning</span>
            <ArrowRight className="size-3.5 shrink-0 transition-transform group-hover:translate-x-0.5" />
          </Link>
          <h1 className="text-[2.4rem] leading-[1.05] font-semibold tracking-[-0.045em] text-balance sm:text-5xl lg:text-[3.4rem]">
            Social media scheduling for you and your <span className="sq-gradient-text">AI agents</span>.
          </h1>
          <p className="mt-6 max-w-xl text-lg leading-relaxed text-pretty text-fd-muted-foreground">
            Write posts with your agents, set a schedule, and Staqly publishes them on time. MCP, REST and webhooks
            included.
          </p>
          <div className="mt-8 flex flex-wrap items-center gap-3">
            <Link
              href="/quickstart"
              className="sq-rainbow inline-flex h-11 items-center gap-2 rounded-xl px-5 text-sm font-semibold shadow-[0_8px_24px_-8px_color-mix(in_oklch,var(--brand)_70%,transparent)] transition hover:brightness-110"
            >
              Quickstart <ArrowRight className="size-4" />
            </Link>
            <Link
              href="/api/reference"
              className="inline-flex h-11 items-center gap-2 rounded-xl border border-fd-border bg-fd-card px-5 text-sm font-semibold transition-colors hover:bg-fd-accent"
            >
              <Braces className="size-4 text-fd-muted-foreground" /> API reference
            </Link>
            <Link
              href="/introduction"
              className="inline-flex h-11 items-center gap-1.5 px-2 text-sm font-medium text-fd-muted-foreground transition-colors hover:text-fd-foreground"
            >
              Read the docs <ArrowRight className="size-4" />
            </Link>
          </div>
        </div>
        <HeroDemo basePath={env.basePath} />
      </div>
    </section>
  )
}

function Features() {
  return (
    <section className="mx-auto w-full max-w-6xl px-5 py-20 sm:px-8">
      <div className="max-w-2xl">
        <p className="text-sm font-semibold text-brand-strong">Why staqly</p>
        <h2 className="mt-2 text-3xl font-semibold tracking-[-0.03em] text-balance sm:text-4xl">
          Agents write the posts. You keep the rhythm.
        </h2>
        <p className="mt-4 text-fd-muted-foreground">
          Every post lands in a collection with full history. Collections have schedules, schedules publish to channels,
          and a durable job queue does the sending with retries.
        </p>
      </div>
      <div className="mt-12 grid gap-px overflow-hidden rounded-2xl border border-fd-border bg-fd-border sm:grid-cols-2 lg:grid-cols-3">
        {features.map((f) => (
          <Link
            key={f.title}
            href={f.href}
            className="group flex flex-col gap-3 bg-fd-card p-6 transition-colors hover:bg-fd-accent/60 sm:p-7"
          >
            <span className="flex size-10 items-center justify-center rounded-xl border border-fd-border bg-fd-background text-brand [&_svg]:size-5">
              {f.icon}
            </span>
            <h3 className="font-semibold tracking-tight">{f.title}</h3>
            <p className="text-sm leading-relaxed text-fd-muted-foreground">{f.body}</p>
            <span className="mt-auto inline-flex items-center gap-1 pt-2 text-sm font-medium text-fd-muted-foreground transition-colors group-hover:text-brand-strong">
              Learn more <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
            </span>
          </Link>
        ))}
      </div>
    </section>
  )
}

function QuickStart() {
  return (
    <section className="border-y border-fd-border bg-fd-card/50">
      <div className="mx-auto grid w-full max-w-6xl gap-10 px-5 py-20 sm:px-8 lg:grid-cols-[0.9fr_1.1fr] lg:items-center">
        <div>
          <p className="text-sm font-semibold text-brand-strong">Five minutes</p>
          <h2 className="mt-2 text-3xl font-semibold tracking-[-0.03em] sm:text-4xl">From zero to your first post.</h2>
          <p className="mt-4 text-fd-muted-foreground">
            One command installs it, one starts it: a single binary with the API, MCP, the job queue and the dashboard
            (or Docker, if you prefer). No account: on your own machine one access token, printed at start, opens
            everything.
          </p>
          <div className="mt-7 flex flex-wrap gap-3 text-sm">
            <Link
              href="/quickstart"
              className="inline-flex items-center gap-1.5 font-semibold text-brand-strong hover:underline pointer-coarse:-my-2.5 pointer-coarse:py-2.5"
            >
              Follow the quickstart <ArrowRight className="size-4" />
            </Link>
          </div>
        </div>
        <div className="min-w-0 overflow-hidden rounded-2xl border border-fd-border bg-fd-background shadow-sm">
          <div className="flex items-center gap-2 border-b border-fd-border px-4 py-2.5 text-xs text-fd-muted-foreground">
            <Terminal className="size-3.5" /> Terminal
          </div>
          <pre className="overflow-x-auto p-5 font-mono text-[13px] leading-7">
            <code>
              <span className="text-fd-muted-foreground"># 1. install and run it</span>
              {'\n'}
              <span className="text-brand">$</span> curl -fsSL
              https://raw.githubusercontent.com/staqly/staqly/main/scripts/install.sh | sh
              {'\n'}
              <span className="text-brand">$</span> staqly serve
              {'\n'}
              <span className="text-brand">$</span> export STAQLY_API_KEY=$(staqly serve token)
              {'  '}
              <span className="text-fd-muted-foreground"># in a second terminal</span>
              {'\n\n'}
              <span className="text-fd-muted-foreground"># 2. connect a channel and write a post</span>
              {'\n'}
              <span className="text-brand">$</span> curl -X POST localhost:3333/v1/channels -d &apos;{'{'}
              &quot;provider&quot;:&quot;custom&quot;,&quot;name&quot;:&quot;My publisher&quot;{'}'}&apos; \{'\n'}
              {'    '}-H &apos;Content-Type: application/json&apos; -H &quot;Authorization: Bearer $STAQLY_API_KEY&quot;
              {'\n'}
              <span className="text-brand">$</span> curl -X POST localhost:3333/v1/posts -H &apos;Content-Type:
              application/json&apos; -H &quot;Authorization: Bearer $STAQLY_API_KEY&quot; \{'\n'}
              {'    '}-d &apos;{'{'}&quot;collection&quot;:&quot;Default&quot;,&quot;content&quot;:&quot;Hello&quot;,
              &quot;targets&quot;:[{'{'}&quot;channel_id&quot;:&quot;ch_...&quot;{'}'}],&quot;publish_now&quot;:true
              {'}'}
              &apos;
              {'\n\n'}
              <span className="text-fd-muted-foreground"># 3. connect your agent</span>
              {'\n'}
              <span className="text-brand">$</span> claude mcp add --transport http staqly http://localhost:3333/mcp \
              {'\n'}
              {'    '}--header &quot;Authorization: Bearer $STAQLY_API_KEY&quot;
            </code>
          </pre>
        </div>
      </div>
    </section>
  )
}

function Sections() {
  return (
    <section className="mx-auto w-full max-w-6xl px-5 py-20 sm:px-8">
      <h2 className="text-2xl font-semibold tracking-[-0.03em]">Explore the docs</h2>
      <div className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {sections.map((s) => (
          <div key={s.title} className="rounded-2xl border border-fd-border bg-fd-card p-5">
            <h3 className="flex items-center gap-2 text-sm font-semibold [&_svg]:size-4 [&_svg]:text-brand">
              {s.icon} {s.title}
            </h3>
            <ul className="mt-3 space-y-0.5">
              {s.links.map(([label, href]) => (
                <li key={href}>
                  <Link
                    href={href}
                    className="group -mx-2 flex items-center justify-between rounded-lg px-2 py-1.5 text-sm text-fd-muted-foreground pointer-coarse:py-2.5 transition-colors hover:bg-fd-accent hover:text-fd-foreground"
                  >
                    {label}
                    <ArrowRight className="size-3.5 opacity-50 transition-opacity group-hover:opacity-100" />
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  )
}

function Modes() {
  return (
    <section className="mx-auto w-full max-w-6xl px-5 pb-20 sm:px-8">
      <div className="grid gap-4 md:grid-cols-2">
        <Link
          href="/local-vs-cloud#community-edition"
          className="group rounded-2xl border border-fd-border bg-fd-card p-6 transition-colors hover:bg-fd-accent/50"
        >
          <HardDrive className="size-5 text-brand" />
          <h3 className="mt-4 font-semibold">Community Edition — the default</h3>
          <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
            One workspace on your machine. The API answers only this computer, with one access token. Phones join with a
            QR code.
          </p>
        </Link>
        <Link
          href="/local-vs-cloud#staqly-cloud"
          className="group rounded-2xl border border-fd-border bg-fd-card p-6 transition-colors hover:bg-fd-accent/50"
        >
          <Cloud className="size-5 text-brand" />
          <h3 className="mt-4 font-semibold">Staqly Cloud — hosted for your team</h3>
          <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
            Sign up at app.staqly.ai: many workspaces, members with roles, scoped API keys and plans per user. We run it
            for you.
          </p>
        </Link>
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-4 rounded-2xl border border-fd-border bg-fd-card px-6 py-5">
        <div className="flex items-center gap-3">
          <Smartphone className="size-5 shrink-0 text-brand" />
          <p className="text-sm text-fd-muted-foreground">
            <span className="font-medium text-fd-foreground">Manage anywhere.</span> The dashboard is built for phones
            and tablets: the calendar as a list, a tap to publish or reschedule.
          </p>
        </div>
        <Link
          href="/dashboard/phones-and-tablets"
          className="inline-flex items-center gap-1.5 text-sm font-semibold text-brand-strong hover:underline pointer-coarse:-my-2.5 pointer-coarse:py-2.5"
        >
          Connect a device <ArrowRight className="size-4" />
        </Link>
      </div>
    </section>
  )
}

function Footer() {
  return (
    <footer className="border-t border-fd-border">
      <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center justify-between gap-4 px-5 py-8 text-sm text-fd-muted-foreground sm:px-8">
        <span className="inline-flex items-center gap-2">
          <LogoMark className="size-5" /> staqly ·{' '}
          <a href={`${repoUrl}/blob/main/LICENSE`} className="hover:text-fd-foreground">
            AGPL-3.0
          </a>
        </span>
        <nav className="flex flex-wrap gap-x-5 gap-y-2 pointer-coarse:[&>*]:inline-flex pointer-coarse:[&>*]:min-h-10 pointer-coarse:[&>*]:items-center">
          <Link href="/introduction" className="hover:text-fd-foreground">
            Docs
          </Link>
          <Link href="/api/reference" className="hover:text-fd-foreground">
            API
          </Link>
          <Link href="/mcp" className="hover:text-fd-foreground">
            MCP
          </Link>
          <a href={env.webUrl} className="hover:text-fd-foreground">
            staqly.ai
          </a>
          <a href={repoUrl} className="hover:text-fd-foreground">
            GitHub
          </a>
        </nav>
      </div>
    </footer>
  )
}

export const metadata: Metadata = {
  ...docsMeta(
    'staqly docs: the local-first social media scheduler for people and their AI agents',
    DOCS_DESCRIPTION,
    '/',
    'website',
  ),
  title: { absolute: 'staqly docs: the local-first social media scheduler for people and their AI agents' },
}

export default function HomePage() {
  return (
    <main className="flex flex-1 flex-col">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: ldJson(SITE_JSON_LD) }} />
      <Hero />
      <Features />
      <QuickStart />
      <Sections />
      <Modes />
      <Footer />
    </main>
  )
}
