import { Logo } from '@staqly/brand/logo'
import { SiteFooter } from '@staqly/brand/site-footer'
import { ArrowRight, Terminal } from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { MobileMenu } from '@/components/mobile-menu'
import { DOCS, SIGNUP, btnPrimary, btnSecondary, wrap, wrapWide, SELF_HOST } from '@/lib/site'

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

/** The site footer, shared with the docs (packages/brand). */
export function Footer() {
  return <SiteFooter web="" docs={DOCS} link={Link} />
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
