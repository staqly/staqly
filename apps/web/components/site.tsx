import { Logo, LogoMark } from '@staqly/brand/logo'
import { ArrowRight, Terminal, Heart } from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { MobileMenu } from '@/components/mobile-menu'
import { DOCS, GITHUB, SIGNUP, btnPrimary, btnSecondary, wrap, wrapWide, SELF_HOST } from '@/lib/site'
import { TOOLS } from '@/lib/tools'

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden className={className} fill="currentColor">
      <path d="M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2c-3.3.7-4-1.6-4-1.6-.6-1.4-1.4-1.8-1.4-1.8-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.8 1.3 3.5 1 0-.8.4-1.3.7-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.2.5-2.3 1.3-3.1-.2-.4-.6-1.6 0-3.2 0 0 1-.3 3.4 1.2a11.5 11.5 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.6.2 2.8.1 3.2.8.8 1.3 1.9 1.3 3.2 0 4.6-2.8 5.6-5.5 5.9.5.4.9 1.1.9 2.2v3.3c0 .3.1.7.8.6A12 12 0 0 0 12 .3" />
    </svg>
  )
}

export function Eyebrow({ children }: { children: ReactNode }) {
  return <p className="text-sm font-semibold text-brand-strong">{children}</p>
}

export function H2({ children }: { children: ReactNode }) {
  return <h2 className="mt-2 text-3xl font-semibold tracking-[-0.03em] text-balance sm:text-4xl">{children}</h2>
}

const navLinks: [string, string][] = [
  ['How it works', '/#how'],
  ['Channels', '/#channels'],
  ['Use cases', '/use-cases'],
  ['Agents', '/agents'],
  ['Tools', '/tools'],
  ['Pricing', '/pricing'],
  ['Docs', DOCS],
]

export function Nav() {
  return (
    <header className="sticky top-0 z-20 border-b border-border bg-background/80 backdrop-blur-md">
      <div className={`${wrapWide} flex h-16 items-center gap-3 lg:gap-8`}>
        <Link href="/" aria-label="Staqly home" className="flex min-h-10 items-center">
          <Logo colorway="dark" wordmarkClassName="text-[18px]" />
        </Link>
        <nav className="ml-auto hidden gap-6 text-sm whitespace-nowrap text-muted-foreground lg:flex">
          {navLinks.map(([label, href]) => (
            <Link key={label} href={href} className="py-0.5 hover:text-foreground pointer-coarse:py-2.5">
              {label}
            </Link>
          ))}
        </nav>
        <a href={SIGNUP} className={`${btnPrimary} ml-auto h-9 px-4 lg:ml-0 pointer-coarse:h-10`}>
          Get started
        </a>
        <MobileMenu links={navLinks} signup={SIGNUP} />
      </div>
    </header>
  )
}

const footerColumns: { title: string; links: [string, string][] }[] = [
  {
    title: 'Product',
    links: [
      ['How it works', '/#how'],
      ['Channels', '/#channels'],
      ['Pricing', '/pricing'],
      ['Compare plans', '/pricing/compare'],
      ['Use cases', '/use-cases'],
      ['Agents', '/agents'],
    ],
  },
  {
    title: 'Free tools',
    links: [['All free tools', '/tools'], ...TOOLS.map((t): [string, string] => [t.short, `/tools/${t.slug}`])],
  },
  {
    title: 'Developers',
    links: [
      ['Docs', DOCS],
      ['API reference', `${DOCS}/api/reference`],
      ['MCP', `${DOCS}/mcp`],
      ['CLI', `${DOCS}/cli`],
      ['TypeScript SDK', `${DOCS}/sdk/typescript`],
      ['Python SDK', `${DOCS}/sdk/python`],
      ['Self-hosting', SELF_HOST],
    ],
  },
  {
    title: 'Legal',
    links: [
      ['Security', `${GITHUB}/blob/main/SECURITY.md`],
      ['Terms', `${DOCS}/legal/terms`],
      ['Privacy', `${DOCS}/legal/privacy`],
      ['AGPL-3.0 license', `${GITHUB}/blob/main/LICENSE`],
    ],
  },
]

export function Footer() {
  return (
    <footer className="border-t border-border">
      <div className={`${wrap} grid gap-10 py-12 sm:grid-cols-2 sm:py-16 lg:grid-cols-[1.4fr_repeat(4,1fr)]`}>
        <div className="max-w-xs sm:col-span-2 lg:col-span-1">
          <Link href="/" aria-label="Staqly home" className="inline-flex min-h-10 items-center">
            <Logo colorway="dark" wordmarkClassName="text-[18px]" />
          </Link>
          <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
            The post scheduler for people and their AI agents: write once, schedule it, and publish to X or anywhere
            your own automation reaches.
          </p>
          <a
            href={GITHUB}
            className="mt-5 inline-flex min-h-10 items-center gap-2 rounded-lg border border-border bg-card px-3 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <GitHubIcon className="size-4" /> Star on GitHub
          </a>
        </div>
        {footerColumns.map((c) => (
          <nav key={c.title} aria-label={c.title}>
            <h2 className="text-xs font-semibold tracking-wider text-foreground/80 uppercase">{c.title}</h2>
            <ul className="mt-4 grid gap-2.5 text-sm">
              {c.links.map(([label, href]) => (
                <li key={label}>
                  <Link
                    href={href}
                    className="inline-flex min-h-6 items-center text-muted-foreground transition-colors hover:text-brand-strong pointer-coarse:min-h-10"
                  >
                    {label}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
        ))}
      </div>
      <div className="border-t border-border">
        <div
          className={`${wrap} flex flex-col items-center gap-2 py-6 text-sm text-muted-foreground sm:flex-row sm:justify-between`}
        >
          <p className="inline-flex items-center gap-2 text-foreground">
            <LogoMark className="size-5" colorway="dark" /> © 2026 Staqly. All rights reserved.
          </p>
          <p className="inline-flex items-center gap-1.5">
            Hosted cloud + open source
            <Heart aria-label="with love" className="size-3.5 fill-red-500 text-red-500" />
          </p>
        </div>
      </div>
    </footer>
  )
}

/** Signup + self-host buttons, used in page heroes. */
export function CtaButtons() {
  return (
    <div className="mt-9 flex flex-wrap items-center gap-3">
      <a href={SIGNUP} className={btnPrimary}>
        Start free trial <ArrowRight className="size-4" />
      </a>
      <a href={SELF_HOST} className={btnSecondary}>
        <Terminal className="size-4 text-muted-foreground" /> Self-host instead
      </a>
    </div>
  )
}

/** Closing call to action at the bottom of every page (rendered by the root layout). */
export function ClosingCta() {
  return (
    <section className="relative isolate overflow-hidden">
      <div aria-hidden className="sq-dot-grid absolute inset-0 -z-10" />
      <div
        aria-hidden
        className="sq-glow absolute -bottom-40 left-1/2 -z-10 h-[26rem] w-[min(52rem,140vw)] -translate-x-1/2"
      />
      <div className={`${wrap} flex flex-col items-center py-16 text-center sm:py-28`}>
        <h2 className="max-w-3xl text-4xl leading-[1.08] font-semibold tracking-[-0.04em] text-balance sm:text-6xl">
          Plan the week once. <span className="sq-gradient-text">Post every day.</span>
        </h2>
        <p className="mt-5 max-w-xl text-muted-foreground">
          Create a workspace in a minute, connect your channels and your agent, and let the schedule keep the rhythm.
        </p>
        <div className="mt-9 flex flex-wrap justify-center gap-3">
          <a
            href={SIGNUP}
            className="inline-flex h-12 items-center gap-2 rounded-xl sq-rainbow px-6 font-semibold shadow-[0_12px_40px_-12px_#63f08e] transition hover:brightness-105"
          >
            Start free trial <ArrowRight className="size-4" />
          </a>
          <a href={SELF_HOST} className={`${btnSecondary} h-12 px-6 text-base`}>
            Self-host instead
          </a>
        </div>
      </div>
    </section>
  )
}

export function PageHero({ eyebrow, title, children }: { eyebrow: ReactNode; title: ReactNode; children?: ReactNode }) {
  return (
    <section className="relative isolate overflow-hidden border-b border-border">
      <div aria-hidden className="sq-grid-bg absolute inset-0 -z-10" />
      <div
        aria-hidden
        className="sq-glow absolute -top-40 left-1/2 -z-10 h-[24rem] w-[min(48rem,140vw)] -translate-x-1/2"
      />
      <div className={`${wrap} pt-16 pb-14 sm:pt-20`}>
        <div className="text-sm text-muted-foreground">{eyebrow}</div>
        <h1 className="mt-4 max-w-3xl text-4xl leading-[1.08] font-semibold tracking-[-0.04em] text-balance sm:text-5xl">
          {title}
        </h1>
        {children}
      </div>
    </section>
  )
}
