import Link from "next/link";
import type { ReactNode } from "react";
import {
  ArrowRight,
  BellRing,
  BookOpen,
  Bot,
  Braces,
  CheckCheck,
  Cloud,
  Cpu,
  Filter,
  HardDrive,
  Inbox,
  Radar,
  Rocket,
  ShieldCheck,
  Smartphone,
  Terminal,
  Workflow,
} from "lucide-react";
import { LogoMark } from "@/components/logo";

const features: { icon: ReactNode; title: string; body: string; href: string }[] = [
  {
    icon: <Inbox />,
    title: "One Inbox for every agent",
    body: "Images, video, text, links and JSON from Claude Code, Codex, OpenCode, n8n and scripts — reviewed in one place, mostly from the keyboard.",
    href: "/docs/dashboard/inbox",
  },
  {
    icon: <ShieldCheck />,
    title: "Human approval gates",
    body: "Agents propose, wait for your decision and act only when you approve — with edits, expiry and a reported result.",
    href: "/docs/cookbook/approval-gate",
  },
  {
    icon: <Filter />,
    title: "Rules engine",
    body: "Auto-approve confident output, reject noisy sources, expire what nobody reviews and escalate what is urgent.",
    href: "/docs/automation/rules",
  },
  {
    icon: <Radar />,
    title: "Watchers",
    body: "Follow RSS feeds and web pages — Hacker News, Reddit, GitHub, arXiv, YouTube and more — as pending items.",
    href: "/docs/automation/watchers",
  },
  {
    icon: <BellRing />,
    title: "Decide from anywhere",
    body: "Approve or reject from Telegram, Slack, Discord, ntfy or email — and every decision reaches your automations as a signed webhook.",
    href: "/docs/automation/notifications",
  },
  {
    icon: <HardDrive />,
    title: "Local-first",
    body: "One Go process, one SQLite file, one uploads folder. Keyless on your machine; hosted mode when you need it.",
    href: "/docs/local-vs-cloud",
  },
];

const sections: { title: string; icon: ReactNode; links: [string, string][] }[] = [
  {
    title: "Get started",
    icon: <Rocket />,
    links: [
      ["What is staq.ly?", "/docs"],
      ["Quickstart", "/docs/quickstart"],
      ["Concepts", "/docs/concepts"],
      ["Local vs cloud", "/docs/local-vs-cloud"],
    ],
  },
  {
    title: "Connect agents",
    icon: <Bot />,
    links: [
      ["Claude Code", "/docs/agents/claude-code"],
      ["OpenAI Codex", "/docs/agents/codex"],
      ["OpenCode", "/docs/agents/opencode"],
      ["n8n", "/docs/agents/n8n"],
    ],
  },
  {
    title: "Automate",
    icon: <Workflow />,
    links: [
      ["Rules engine", "/docs/automation/rules"],
      ["Watchers & presets", "/docs/automation/watchers"],
      ["Custom actions", "/docs/automation/custom-actions"],
      ["Notifications", "/docs/automation/notifications"],
      ["Webhooks", "/docs/automation/webhooks"],
    ],
  },
  {
    title: "Build",
    icon: <Braces />,
    links: [
      ["API overview", "/docs/api"],
      ["API reference", "/docs/api/reference"],
      ["MCP tools", "/docs/mcp/tools"],
      ["TypeScript SDK", "/docs/sdk/typescript"],
      ["staqly CLI", "/docs/cli"],
    ],
  },
  {
    title: "Self-host",
    icon: <Cpu />,
    links: [
      ["Docker Compose", "/docs/self-hosting/docker-compose"],
      ["Configuration", "/docs/self-hosting/configuration"],
      ["Backup & restore", "/docs/self-hosting/backup-and-restore"],
      ["Cloud deployment", "/docs/self-hosting/cloud-deployment"],
      ["Monitoring", "/docs/self-hosting/monitoring"],
    ],
  },
  {
    title: "Cookbook",
    icon: <BookOpen />,
    links: [
      ["Gate an action on approval", "/docs/cookbook/approval-gate"],
      ["Approval gate in CI", "/docs/cookbook/ci-approval-gate"],
      ["Approve from chat", "/docs/cookbook/approve-from-chat"],
      ["Instagram Avatar end to end", "/docs/cookbook/instagram-avatar"],
      ["Find what I approved yesterday", "/docs/cookbook/approved-yesterday"],
      ["Auto-triage with rules + watchers", "/docs/cookbook/auto-triage"],
    ],
  },
];

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
            href="/docs/cookbook/approval-gate"
            className="group mb-7 inline-flex max-w-full items-center gap-2 rounded-full border border-fd-border bg-fd-card/80 py-1 pr-3 pl-1 text-xs font-medium text-fd-muted-foreground shadow-sm backdrop-blur transition-colors hover:text-fd-foreground"
          >
            <span className="rounded-full bg-brand-soft px-2 py-0.5 text-brand-strong">New</span>
            <span className="truncate">Gate agent actions on human approval</span>
            <ArrowRight className="size-3.5 shrink-0 transition-transform group-hover:translate-x-0.5" />
          </Link>
          <h1 className="text-[2.6rem] leading-[1.04] font-semibold tracking-[-0.045em] text-balance sm:text-6xl lg:text-[4.1rem]">
            The workspace for everything your{" "}
            <span className="bg-gradient-to-br from-[#FF9A4D] via-[#F2622B] to-[#D9402A] bg-clip-text text-transparent">
              AI agents
            </span>{" "}
            create.
          </h1>
          <p className="mt-6 max-w-xl text-lg leading-relaxed text-pretty text-fd-muted-foreground">
            staq.ly is a local-first, self-hostable workspace where agents store what they generate — and where you
            approve, reject, edit and automate it. REST, MCP and signed webhooks included.
          </p>
          <div className="mt-9 flex flex-wrap items-center gap-3">
            <Link
              href="/docs/quickstart"
              className="inline-flex h-11 items-center gap-2 rounded-xl bg-fd-primary px-5 text-sm font-semibold text-fd-primary-foreground shadow-[0_8px_24px_-8px_color-mix(in_oklch,var(--brand)_70%,transparent)] transition hover:brightness-110"
            >
              Quickstart <ArrowRight className="size-4" />
            </Link>
            <Link
              href="/docs/api/reference"
              className="inline-flex h-11 items-center gap-2 rounded-xl border border-fd-border bg-fd-card px-5 text-sm font-semibold transition-colors hover:bg-fd-accent"
            >
              <Braces className="size-4 text-fd-muted-foreground" /> API reference
            </Link>
            <Link
              href="/docs"
              className="inline-flex h-11 items-center gap-1.5 px-2 text-sm font-medium text-fd-muted-foreground transition-colors hover:text-fd-foreground"
            >
              Read the docs <ArrowRight className="size-4" />
            </Link>
          </div>
          <dl className="mt-10 grid max-w-lg grid-cols-3 gap-4 border-t border-fd-border pt-6 text-sm">
            {[
              ["1", "Go process"],
              ["1", "SQLite file"],
              ["0", "API keys locally"],
            ].map(([n, label]) => (
              <div key={label}>
                <dt className="text-2xl font-semibold tracking-tight tabular-nums">{n}</dt>
                <dd className="mt-0.5 text-fd-muted-foreground">{label}</dd>
              </div>
            ))}
          </dl>
        </div>
        <HeroVisual />
      </div>
    </section>
  );
}

function HeroVisual() {
  return (
    <div className="relative min-w-0">
      <div className="sq-float relative rounded-2xl border border-fd-border bg-fd-card shadow-[0_30px_80px_-30px_rgb(0_0_0/0.35)]">
        <div className="flex items-center gap-2 border-b border-fd-border px-4 py-3">
          <span className="size-2.5 rounded-full bg-[#ff5f57]" />
          <span className="size-2.5 rounded-full bg-[#febc2e]" />
          <span className="size-2.5 rounded-full bg-[#28c840]" />
          <span className="ml-2 flex items-center gap-1.5 truncate font-mono text-xs text-fd-muted-foreground">
            <Terminal className="size-3.5" /> claude
          </span>
        </div>
        <div className="space-y-3 overflow-x-auto p-4 font-mono text-[12.5px] leading-relaxed sm:p-5">
          <p className="whitespace-nowrap text-fd-muted-foreground">
            <span className="text-brand">❯</span> claude mcp add --transport http staqly http://localhost:3333/mcp
          </p>
          <p className="whitespace-nowrap">
            <span className="text-brand">❯</span> “Add these 4 avatars to Instagram Avatar”
          </p>
          <p className="whitespace-nowrap text-fd-muted-foreground">
            ⏺ staqly · <span className="text-fd-foreground">create_item</span> ×4 → status{" "}
            <span className="text-brand-strong">pending</span>
          </p>
          <p className="whitespace-nowrap text-fd-muted-foreground">
            ⏺ staqly · <span className="text-fd-foreground">await_decision</span> …
          </p>
        </div>
      </div>

      <div className="relative -mt-6 ml-6 rounded-2xl border border-fd-border bg-fd-background/95 p-4 shadow-[0_24px_60px_-28px_rgb(0_0_0/0.4)] backdrop-blur sm:ml-14 sm:p-5">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <Inbox className="size-4 text-brand" /> Inbox
            <span className="rounded-full bg-brand-soft px-2 py-0.5 text-[11px] font-semibold text-brand-strong">
              4 pending
            </span>
          </div>
          <span className="hidden font-mono text-[11px] text-fd-muted-foreground sm:inline">j / k · a · r · e</span>
        </div>
        <ul className="space-y-2">
          {[
            ["Avatar post #1", "Golden hour on the rooftop ☀️", "approved"],
            ["Avatar post #2", "Neon nights in the city 🌃", "pending"],
            ["Avatar post #3", "Slow morning, good coffee ☕", "pending"],
          ].map(([title, caption, status], i) => (
            <li
              key={title}
              className={`flex items-center gap-3 rounded-xl border px-3 py-2.5 ${
                i === 1
                  ? "border-[color-mix(in_oklch,var(--brand)_45%,transparent)] bg-brand-soft"
                  : "border-fd-border bg-fd-card"
              }`}
            >
              <span
                aria-hidden
                className="size-9 shrink-0 rounded-lg bg-gradient-to-br from-[#ffd2b0] via-[#f59e6b] to-[#c2410c] opacity-90"
                style={{ filter: `hue-rotate(${i * 38}deg)` }}
              />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{title}</span>
                <span className="block truncate text-xs text-fd-muted-foreground">{caption}</span>
              </span>
              {status === "approved" ? (
                <span className="inline-flex items-center gap-1 rounded-md bg-emerald-500/12 px-2 py-0.5 text-[11px] font-semibold text-emerald-700 dark:text-emerald-300">
                  <CheckCheck className="size-3" /> approved
                </span>
              ) : (
                <span className="rounded-md bg-fd-muted px-2 py-0.5 text-[11px] font-semibold text-fd-muted-foreground">
                  pending
                </span>
              )}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function Features() {
  return (
    <section className="mx-auto w-full max-w-6xl px-5 py-20 sm:px-8">
      <div className="max-w-2xl">
        <p className="text-sm font-semibold text-brand-strong">Why staq.ly</p>
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
  );
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
            Docker is all you need. No account, no API key: on your own machine staq.ly is keyless, and the dashboard
            opens already signed in.
          </p>
          <div className="mt-7 flex flex-wrap gap-3 text-sm">
            <Link
              href="/docs/quickstart"
              className="inline-flex items-center gap-1.5 font-semibold text-brand-strong hover:underline"
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
              <span className="text-fd-muted-foreground"># 1. run it</span>
              {"\n"}
              <span className="text-brand">$</span> make up
              {"\n\n"}
              <span className="text-fd-muted-foreground"># 2. create a collection and an item</span>
              {"\n"}
              <span className="text-brand">$</span> curl -X POST localhost:3333/v1/collections -d &apos;{"{"}
              &quot;name&quot;:&quot;Drafts&quot;{"}"}&apos; \{"\n"}
              {"    "}-H &apos;Content-Type: application/json&apos;
              {"\n"}
              <span className="text-brand">$</span> curl -X POST localhost:3333/v1/collections/Drafts/items \{"\n"}
              {"    "}-H &apos;Content-Type: application/json&apos; -d &apos;{"{"}&quot;content&quot;:&quot;Hello&quot;
              {"}"}&apos;
              {"\n\n"}
              <span className="text-fd-muted-foreground"># 3. connect your agent</span>
              {"\n"}
              <span className="text-brand">$</span> claude mcp add --transport http staqly http://localhost:3333/mcp
            </code>
          </pre>
        </div>
      </div>
    </section>
  );
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
                    className="group -mx-2 flex items-center justify-between rounded-lg px-2 py-1.5 text-sm text-fd-muted-foreground transition-colors hover:bg-fd-accent hover:text-fd-foreground"
                  >
                    {label}
                    <ArrowRight className="size-3.5 opacity-0 transition-opacity group-hover:opacity-100" />
                  </Link>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}

function Modes() {
  return (
    <section className="mx-auto w-full max-w-6xl px-5 pb-20 sm:px-8">
      <div className="grid gap-4 md:grid-cols-2">
        <Link
          href="/docs/local-vs-cloud#local-mode"
          className="group rounded-2xl border border-fd-border bg-fd-card p-6 transition-colors hover:bg-fd-accent/50"
        >
          <HardDrive className="size-5 text-brand" />
          <h3 className="mt-4 font-semibold">Local mode — the default</h3>
          <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
            One workspace on your machine. The API answers only this computer, without keys. Phones join with a QR code.
          </p>
        </Link>
        <Link
          href="/docs/local-vs-cloud#cloud-mode"
          className="group rounded-2xl border border-fd-border bg-fd-card p-6 transition-colors hover:bg-fd-accent/50"
        >
          <Cloud className="size-5 text-brand" />
          <h3 className="mt-4 font-semibold">Cloud mode — when you need it</h3>
          <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
            Many workspaces, scoped API keys, sessions and recovery keys, Free / Indie / Team plans. Same app, one
            environment variable.
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
          href="/docs/dashboard/phones-and-tablets"
          className="inline-flex items-center gap-1.5 text-sm font-semibold text-brand-strong hover:underline"
        >
          Connect a device <ArrowRight className="size-4" />
        </Link>
      </div>
    </section>
  );
}

function Footer() {
  return (
    <footer className="border-t border-fd-border">
      <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center justify-between gap-4 px-5 py-8 text-sm text-fd-muted-foreground sm:px-8">
        <span className="inline-flex items-center gap-2">
          <LogoMark className="size-5" /> staq.ly · MIT licensed
        </span>
        <nav className="flex flex-wrap gap-x-5 gap-y-2">
          <Link href="/docs" className="hover:text-fd-foreground">
            Docs
          </Link>
          <Link href="/docs/api/reference" className="hover:text-fd-foreground">
            API
          </Link>
          <Link href="/docs/mcp" className="hover:text-fd-foreground">
            MCP
          </Link>
          <a href="https://github.com/staqly/staqly" className="hover:text-fd-foreground">
            GitHub
          </a>
        </nav>
      </div>
    </footer>
  );
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
  );
}
