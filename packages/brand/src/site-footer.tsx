import { Heart } from 'lucide-react'
import type { ComponentType, ReactNode } from 'react'

import { Logo, LogoMark } from './logo'
import { TOOLS } from './tools'

// The site footer of staqly.ai and the docs (docs.staqly.ai): the logo and a line about staqly, link columns, and a
// bottom bar. Dark only, like both sites. Each site passes its own link component (next/link) and the bases of the
// two sites, so its own pages stay relative and the other site's are absolute.

export const GITHUB = 'https://github.com/staqly/staqly'

type LinkProps = { href: string; className?: string; children: ReactNode; 'aria-label'?: string }

export type SiteFooterProps = {
  /** Base of staqly.ai links: '' on staqly.ai, 'https://staqly.ai' elsewhere. */
  web: string
  /** Base of docs links: '' in the docs, 'https://docs.staqly.ai' elsewhere. */
  docs: string
  /** Path prefix of the app's public/ files, for the logo (the docs' base path). */
  basePath?: string
  /** The site's link component (next/link); a plain <a> by default. */
  link?: ComponentType<LinkProps>
}

type Column = { title: string; links: [string, string][] }

function columns(web: string, docs: string): Column[] {
  return [
    {
      title: 'Product',
      links: [
        ['How it works', `${web}/#how`],
        ['Channels', `${web}/#channels`],
        ['Pricing', `${web}/pricing`],
        ['Compare plans', `${web}/pricing/compare`],
        ['Use cases', `${web}/use-cases`],
        ['Agents', `${web}/agents`],
      ],
    },
    {
      title: 'Free tools',
      links: [
        ['All free tools', `${web}/tools`],
        ...TOOLS.map((t): [string, string] => [t.short, `${web}/tools/${t.slug}`]),
      ],
    },
    {
      title: 'Developers',
      links: [
        ['Docs', docs || '/'],
        ['API reference', `${docs}/api/reference`],
        ['MCP', `${docs}/mcp`],
        ['CLI', `${docs}/cli`],
        ['TypeScript SDK', `${docs}/sdk/typescript`],
        ['Python SDK', `${docs}/sdk/python`],
        ['Self-hosting', `${docs}/quickstart`],
      ],
    },
    {
      title: 'Legal',
      links: [
        ['Security', `${GITHUB}/blob/main/SECURITY.md`],
        ['Terms', `${docs}/legal/terms`],
        ['Privacy', `${docs}/legal/privacy`],
        ['AGPL-3.0 license', `${GITHUB}/blob/main/LICENSE`],
      ],
    },
  ]
}

function A({ href, className, children, 'aria-label': label }: LinkProps) {
  return (
    <a href={href} className={className} aria-label={label}>
      {children}
    </a>
  )
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden className={className} fill="currentColor">
      <path d="M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2c-3.3.7-4-1.6-4-1.6-.6-1.4-1.4-1.8-1.4-1.8-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.8 1.3 3.5 1 0-.8.4-1.3.7-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.2.5-2.3 1.3-3.1-.2-.4-.6-1.6 0-3.2 0 0 1-.3 3.4 1.2a11.5 11.5 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.6.2 2.8.1 3.2.8.8 1.3 1.9 1.3 3.2 0 4.6-2.8 5.6-5.5 5.9.5.4.9 1.1.9 2.2v3.3c0 .3.1.7.8.6A12 12 0 0 0 12 .3" />
    </svg>
  )
}

const wrap = 'mx-auto w-full max-w-6xl px-5 sm:px-8'

export function SiteFooter({ web, docs, basePath, link: Link = A }: SiteFooterProps) {
  return (
    <footer className="border-t border-border">
      <div className={`${wrap} grid gap-10 py-12 sm:grid-cols-2 sm:py-16 lg:grid-cols-[1.4fr_repeat(4,1fr)]`}>
        <div className="max-w-xs sm:col-span-2 lg:col-span-1">
          <Link href={web || '/'} aria-label="Staqly home" className="inline-flex min-h-10 items-center">
            <Logo basePath={basePath} colorway="dark" wordmarkClassName="text-[18px]" />
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
        {columns(web, docs).map((c) => (
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
            <LogoMark className="size-5" basePath={basePath} colorway="dark" /> © 2026 Staqly. All rights reserved.
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
