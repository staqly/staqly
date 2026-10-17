import { HeroDemo } from '@staqly/brand/hero-demo'
import {
  ArrowRight,
  BellRing,
  BookOpen,
  Bot,
  Braces,
  Cloud,
  Cpu,
  HardDrive,
  Hourglass,
  Inbox,
  Radar,
  Rocket,
  ShieldCheck,
  Smartphone,
  Terminal,
  Workflow,
} from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { LogoMark } from '@/components/logo'
import { env } from '@/lib/env.mjs'
import { repoUrl } from '@/lib/shared'

const features: { icon: ReactNode; title: string; body: string; href: string }[] = [
  {
    icon: <Inbox />,
    title: 'One Inbox for every agent',
    body: 'Images, video, text, links and JSON from Claude Code, Codex, OpenCode, n8n and scripts — reviewed in one place, mostly from the keyboard.',
    href: '/dashboard/inbox',
  },
  {
    icon: <ShieldCheck />,
    title: 'Human approval gates',
    body: 'Agents propose, wait for your decision and act only when you approve — with edits, expiry and a reported result.',
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
    body: 'Follow any RSS/Atom feed or web page and get new entries and page changes as pending items.',
    href: '/automation/watchers',
  },
  {
    icon: <BellRing />,
    title: 'Decide from anywhere',
    body: 'Approve or reject from Telegram, Slack, Discord, ntfy or email — and every decision reaches your automations as a signed webhook.',
    href: '/automation/notifications',
  },
  {
    icon: <HardDrive />,
    title: 'Local-first',
    body: 'One Go process, one SQLite file, one uploads folder. Keyless on your machine; hosted mode when you need it.',
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
      ['Add human approval to an agent', '/how-to-add-human-approval'],
      ['Concepts', '/concepts'],
      ['Local vs cloud', '/local-vs-cloud'],
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
      ['LangChain, CrewAI & more', '/integrations'],
    ],
  },
  {
    title: 'Automate',
    icon: <Workflow />,
    links: [
      ['Watchers', '/automation/watchers'],
      ['Actions', '/automation/custom-actions'],
      ['Notifications', '/automation/notifications'],
      ['Webhooks', '/automation/webhooks'],
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
      ['staqly CLI', '/cli'],
    ],
  },
  {
    title: 'Self-host',
    icon: <Cpu />,
    links: [
      ['Run the binary', '/self-hosting/binary'],
      ['Run with Docker', '/self-hosting/docker'],
      ['Configuration', '/self-hosting/configuration'],
      ['Backup & restore', '/self-hosting/backup-and-restore'],
      ['Cloud deployment', '/self-hosting/cloud-deployment'],
    ],
  },
  {
    title: 'Cookbook',
    icon: <BookOpen />,
    links: [
      ['Gate an action on approval', '/cookbook/approval-gate'],
      ['Approval gate in CI', '/cookbook/ci-approval-gate'],
      ['Approve from chat', '/cookbook/approve-from-chat'],
      ['Instagram Avatar end to end', '/cookbook/instagram-avatar'],
      ['Find what I approved yesterday', '/cookbook/approved-yesterday'],
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
      <div className="mx-auto grid w-full max-w-6xl items-center gap-12 px-5 pt-16 pb-16 sm:px-8 sm:pt-24 lg:grid-cols-[1.05fr_1fr] lg:gap-10 lg:pb-24">
        <div className="min-w-0">
          <Link
            href="/cookbook/approval-gate"
            className="group mb-7 inline-flex max-w-full items-center gap-2 rounded-full border border-fd-border bg-fd-card/80 py-1 pr-3 pl-1 text-xs font-medium text-fd-muted-foreground shadow-sm backdrop-blur transition-colors hover:text-fd-foreground"
          >
            <span className="rounded-full bg-brand-soft px-2 py-0.5 text-brand-strong">New</span>
            <span className="truncate">Gate agent actions on human approval</span>
            <ArrowRight className="size-3.5 shrink-0 transition-transform group-hover:translate-x-0.5" />
          </Link>
          <h1 className="text-[2.6rem] leading-[1.04] font-semibold tracking-[-0.045em] text-balance sm:text-6xl lg:text-[4.1rem]">
            The workspace for everything your <span className="sq-gradient-text">AI agents</span> create.
          </h1>
          <p className="mt-6 max-w-xl text-lg leading-relaxed text-pretty text-fd-muted-foreground">
            staqly is a local-first, self-hostable workspace where agents store what they generate — and where you
            approve, reject, edit and automate it. REST, MCP and signed webhooks included.
          </p>
          <div className="mt-9 flex flex-wrap items-center gap-3">
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
          <dl className="mt-10 grid max-w-lg grid-cols-3 gap-4 border-t border-fd-border pt-6 text-sm">
            {[
              ['1', 'Go process'],
              ['1', 'SQLite file'],
              ['0', 'API keys locally'],
            ].map(([n, label]) => (
              <div key={label}>
                <dt className="text-2xl font-semibold tracking-tight tabular-nums">{n}</dt>
                <dd className="mt-0.5 text-fd-muted-foreground">{label}</dd>
              </div>
            ))}
          </dl>
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
          Agents make things. You stay in control.
        </h2>
        <p className="mt-4 text-fd-muted-foreground">
          Everything an agent produces lands in a collection with full history. Nothing ships until a human — or a rule
          you wrote — says so.
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
          <h2 className="mt-2 text-3xl font-semibold tracking-[-0.03em] sm:text-4xl">
            From zero to your first review.
          </h2>
          <p className="mt-4 text-fd-muted-foreground">
            One command installs it, one starts it: a single binary with the API, MCP and dashboard (or Docker, if you
            prefer). No account, no API key: on your own machine staqly is keyless.
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
              {'\n\n'}
              <span className="text-fd-muted-foreground"># 2. create a collection and an item</span>
              {'\n'}
              <span className="text-brand">$</span> curl -X POST localhost:3333/v1/collections -d &apos;{'{'}
              &quot;name&quot;:&quot;Drafts&quot;{'}'}&apos; \{'\n'}
              {'    '}-H &apos;Content-Type: application/json&apos;
              {'\n'}
              <span className="text-brand">$</span> curl -X POST localhost:3333/v1/collections/Drafts/items \{'\n'}
              {'    '}-H &apos;Content-Type: application/json&apos; -d &apos;{'{'}&quot;content&quot;:&quot;Hello&quot;
              {'}'}&apos;
              {'\n\n'}
              <span className="text-fd-muted-foreground"># 3. connect your agent</span>
              {'\n'}
              <span className="text-brand">$</span> claude mcp add --transport http staqly http://localhost:3333/mcp
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
          href="/local-vs-cloud#local-mode"
          className="group rounded-2xl border border-fd-border bg-fd-card p-6 transition-colors hover:bg-fd-accent/50"
        >
          <HardDrive className="size-5 text-brand" />
          <h3 className="mt-4 font-semibold">Local mode — the default</h3>
          <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
            One workspace on your machine. The API answers only this computer, without keys. Phones join with a QR code.
          </p>
        </Link>
        <Link
          href="/local-vs-cloud#cloud-mode"
          className="group rounded-2xl border border-fd-border bg-fd-card p-6 transition-colors hover:bg-fd-accent/50"
        >
          <Cloud className="size-5 text-brand" />
          <h3 className="mt-4 font-semibold">Cloud mode — when you need it</h3>
          <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
            Many workspaces, scoped API keys, sessions and recovery keys, Free and Pro plans. Same app, one environment
            variable.
          </p>
        </Link>
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-4 rounded-2xl border border-fd-border bg-fd-card px-6 py-5">
        <div className="flex items-center gap-3">
          <Smartphone className="size-5 shrink-0 text-brand" />
          <p className="text-sm text-fd-muted-foreground">
            <span className="font-medium text-fd-foreground">Review anywhere.</span> The dashboard is built for phones
            and tablets, with swipe-to-review.
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
            Apache-2.0
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

export default function HomePage() {
  return (
    <main className="flex flex-1 flex-col">
      <Hero />
      <Features />
      <QuickStart />
      <Sections />
      <Modes />
      <Footer />
    </main>
  )
}
