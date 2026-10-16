import { Logo, LogoMark } from '@staqly/brand/logo'
import { ArrowRight, Terminal } from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { MobileMenu } from '@/components/mobile-menu'
import { DOCS, GITHUB, SIGNUP, btnPrimary, btnSecondary, wrap, SELF_HOST } from '@/lib/site'

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
  ['Use cases', '/use-cases'],
  ['Agents', '/agents'],
  ['Pricing', '/#pricing'],
  ['Docs', DOCS],
]

export function Nav() {
  return (
    <header className="sticky top-0 z-20 border-b border-border bg-background/80 backdrop-blur-md">
      <div className={`${wrap} flex h-16 items-center gap-3 md:gap-8`}>
        <Link href="/" aria-label="staqly home">
          <Logo colorway="dark" wordmarkClassName="text-[18px]" />
        </Link>
        <nav className="ml-auto hidden gap-6 text-sm text-muted-foreground md:flex">
          {navLinks.map(([label, href]) => (
            <Link key={label} href={href} className="hover:text-foreground">
              {label}
            </Link>
          ))}
        </nav>
        <a href={SIGNUP} className={`${btnPrimary} ml-auto h-9 px-4 md:ml-0`}>
          Get started
        </a>
        <MobileMenu links={navLinks} signup={SIGNUP} />
      </div>
    </header>
  )
}

const footerLinks: [string, string][] = [
  ['Use cases', '/use-cases'],
  ['Agents', '/agents'],
  ['Docs', DOCS],
  ['API', `${DOCS}/api/reference`],
  ['MCP', `${DOCS}/mcp`],
  ['Self-hosting', `${DOCS}/self-hosting/binary`],
  ['Security', `${GITHUB}/blob/main/SECURITY.md`],
  ['Apache-2.0', `${GITHUB}/blob/main/LICENSE`],
  ['Terms', `${DOCS}/legal/terms`],
  ['Privacy', `${DOCS}/legal/privacy`],
]

export function Footer() {
  return (
    <footer className="border-t border-border">
      <div
        className={`${wrap} flex flex-col gap-5 py-8 text-sm text-muted-foreground lg:flex-row lg:items-center lg:justify-between`}
      >
        <span className="inline-flex items-center gap-2">
          <LogoMark className="size-5" colorway="dark" /> © 2026 staqly · human sign-off for AI agents
        </span>
        <nav className="flex flex-wrap items-center gap-y-2 text-xs [&>*+*]:before:mx-2 [&>*+*]:before:text-muted-foreground/40 [&>*+*]:before:content-['•'] [&>*]:inline-flex [&>*]:items-center">
          {footerLinks.map(([label, href]) => (
            <Link key={label} href={href} className="hover:text-foreground">
              {label}
            </Link>
          ))}
          <a href={GITHUB} aria-label="staqly on GitHub" className="hover:text-foreground">
            <GitHubIcon className="size-3.5" />
          </a>
        </nav>
      </div>
    </footer>
  )
}

/** Signup + self-host buttons, used in page heroes. */
export function CtaButtons() {
  return (
    <div className="mt-9 flex flex-wrap items-center gap-3">
      <a href={SIGNUP} className={btnPrimary}>
        Get started free <ArrowRight className="size-4" />
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
          Let your agents create — <span className="sq-gradient-text">you keep the final say.</span>
        </h2>
        <p className="mt-5 max-w-xl text-muted-foreground">
          Create a workspace in a minute, connect your agent over MCP or REST, and keep a human in the loop.
        </p>
        <div className="mt-9 flex flex-wrap justify-center gap-3">
          <a
            href={SIGNUP}
            className="inline-flex h-12 items-center gap-2 rounded-xl sq-rainbow px-6 font-semibold shadow-[0_12px_40px_-12px_#63f08e] transition hover:brightness-105"
          >
            Get started free <ArrowRight className="size-4" />
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
