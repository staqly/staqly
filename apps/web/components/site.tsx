import { Logo } from '@staqly/brand/logo'
import { SiteFooter } from '@staqly/brand/site-footer'
import { ArrowRight, Terminal } from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { MobileMenu } from '@/components/mobile-menu'
import { NavLinks } from '@/components/nav-links'
import { TRIAL_DAYS } from '@/lib/plans'
import { DOCS, LOGIN, SIGNUP, btnPrimary, btnSecondary, wrap, wrapWide, SELF_HOST } from '@/lib/site'

export function Eyebrow({ children }: { children: ReactNode }) {
  return <p className="text-sm font-semibold text-brand-strong">{children}</p>
}

export function H2({ children }: { children: ReactNode }) {
  return (
    <h2 className="mt-3 text-3xl leading-[1.08] font-semibold tracking-[-0.035em] text-balance sm:text-[2.75rem]">
      {children}
    </h2>
  )
}

/** Eyebrow, heading and a lead paragraph: the top of most sections. */
export function SectionHead({
  eyebrow,
  title,
  children,
  center,
}: {
  eyebrow: ReactNode
  title: ReactNode
  children?: ReactNode
  center?: boolean
}) {
  return (
    <div className={center ? 'mx-auto max-w-3xl text-center' : 'max-w-2xl'}>
      <Eyebrow>{eyebrow}</Eyebrow>
      <H2>{title}</H2>
      {children && <p className="mt-5 text-lg leading-relaxed text-pretty text-muted-foreground">{children}</p>}
    </div>
  )
}

const navLinks: [string, string][] = [
  ['How it works', '/#how'],
  ['Channels', '/#channels'],
  ['Use cases', '/use-cases'],
  ['AI agents', '/agents'],
  ['Free tools', '/tools'],
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
        <NavLinks links={navLinks} />
        <div className="ml-auto flex items-center gap-2 lg:ml-0">
          <a
            href={LOGIN}
            className="hidden h-9 items-center rounded-xl px-3 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground sm:inline-flex"
          >
            Log in
          </a>
          <a href={SIGNUP} className={`${btnPrimary} h-9 px-4 whitespace-nowrap pointer-coarse:h-10`}>
            <span className="sm:hidden">Try free</span>
            <span className="hidden sm:inline">Start free trial</span>
          </a>
        </div>
        <MobileMenu links={navLinks} signup={SIGNUP} login={LOGIN} />
      </div>
    </header>
  )
}

/** The site footer, shared with the docs (packages/brand). */
export function Footer() {
  return <SiteFooter web="" docs={DOCS} link={Link} />
}

/** The small print under a signup button: what the trial is and the free alternative. */
export function TrialNote({ className = '' }: { className?: string }) {
  return (
    <p className={`text-sm text-muted-foreground ${className}`}>
      {TRIAL_DAYS}-day free trial on Starter · cancel anytime ·{' '}
      <a href={SELF_HOST} className="underline decoration-border underline-offset-4 hover:text-foreground">
        or self-host it free
      </a>
    </p>
  )
}

/** Signup + self-host buttons, used in page heroes. */
export function CtaButtons({ secondary, center }: { secondary?: ReactNode; center?: boolean }) {
  return (
    <div className={`mt-9 flex flex-wrap items-center gap-3 ${center ? 'justify-center' : ''}`}>
      <a href={SIGNUP} className={btnPrimary}>
        Start free trial <ArrowRight className="size-4" />
      </a>
      {secondary ?? (
        <a href={SELF_HOST} className={btnSecondary}>
          <Terminal className="size-4 text-muted-foreground" /> Self-host instead
        </a>
      )}
    </div>
  )
}

/** Closing call to action at the bottom of every page (rendered by the root layout). */
export function ClosingCta() {
  const steps = ['Create a workspace', 'Connect your channels', 'Schedule the first week']

  return (
    <section className="relative isolate overflow-hidden border-t border-border">
      <div aria-hidden className="sq-dot-grid absolute inset-0 -z-10" />
      <div
        aria-hidden
        className="sq-glow absolute -bottom-40 left-1/2 -z-10 h-[26rem] w-[min(52rem,140vw)] -translate-x-1/2"
      />
      <div className={`${wrap} flex flex-col items-center py-20 text-center sm:py-28`}>
        <h2 className="max-w-3xl text-4xl leading-[1.04] font-semibold tracking-[-0.045em] text-balance sm:text-6xl">
          Plan the week once. <span className="sq-gradient-text">Post every day.</span>
        </h2>
        <ol className="mt-8 flex flex-wrap justify-center gap-x-6 gap-y-2 text-sm text-muted-foreground">
          {steps.map((s, i) => (
            <li key={s} className="inline-flex items-center gap-2">
              <span className="flex size-6 items-center justify-center rounded-full border border-border bg-card font-mono text-xs text-brand">
                {i + 1}
              </span>
              {s}
            </li>
          ))}
        </ol>
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
        <TrialNote className="mt-5" />
      </div>
    </section>
  )
}

/** The top of every page but the home page: breadcrumb or eyebrow, title, lead, and optionally a picture beside. */
export function PageHero({
  eyebrow,
  title,
  children,
  aside,
}: {
  eyebrow: ReactNode
  title: ReactNode
  children?: ReactNode
  aside?: ReactNode
}) {
  return (
    <section className="relative isolate overflow-hidden border-b border-border">
      <div aria-hidden className="sq-grid-bg absolute inset-0 -z-10" />
      <div
        aria-hidden
        className="sq-glow absolute -top-40 left-1/2 -z-10 h-[24rem] w-[min(48rem,140vw)] -translate-x-1/2"
      />
      <div
        className={`${wrap} pt-14 pb-14 sm:pt-20 sm:pb-16 ${aside ? 'grid items-center gap-12 lg:grid-cols-[1fr_1.05fr]' : ''}`}
      >
        <div className="min-w-0">
          <div className="text-sm font-semibold text-brand-strong">{eyebrow}</div>
          <h1 className="mt-4 max-w-3xl text-[2.5rem] leading-[1.04] font-semibold tracking-[-0.045em] text-balance sm:text-[3.5rem]">
            {title}
          </h1>
          {children}
        </div>
        {aside && <div className="min-w-0">{aside}</div>}
      </div>
    </section>
  )
}

/** "staqly › Use cases" above a page title. */
export function Breadcrumb({ items }: { items: [string, string][] }) {
  return (
    <nav aria-label="Breadcrumb" className="font-normal text-muted-foreground">
      {items.map(([label, href], i) => (
        <span key={href}>
          {i > 0 && ' › '}
          <Link
            href={href}
            className="hover:text-foreground pointer-coarse:-my-2.5 pointer-coarse:inline-block pointer-coarse:py-2.5"
          >
            {label}
          </Link>
        </span>
      ))}
    </nav>
  )
}

/** A numbered step: the number in a tile, then the text. */
export function Step({ n, children }: { n: number; children: ReactNode }) {
  return (
    <li className="flex gap-4">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-lg border border-border bg-card font-mono text-xs text-brand">
        {n}
      </span>
      <span className="pt-0.5 text-[15px] leading-relaxed">{children}</span>
    </li>
  )
}
