import { ArrowRight, CalendarCheck, Clock, Link2Off, Ratio, Type } from 'lucide-react'
import Link from 'next/link'
import type { ReactNode } from 'react'

import { ORGANIZATION, breadcrumbLd, ldJson } from '@/lib/seo'
import { SITE_URL } from '@/lib/site'
import { TOOLS, type Tool } from '@/lib/tools'

const ICONS: Record<Tool['icon'], ReactNode> = {
  clock: <Clock />,
  type: <Type />,
  link: <Link2Off />,
  check: <CalendarCheck />,
  ratio: <Ratio />,
}

export function ToolCards({ items = TOOLS }: { items?: Tool[] }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {items.map((t) => (
        <Link
          key={t.slug}
          href={`/tools/${t.slug}`}
          className="group flex flex-col rounded-2xl border border-border bg-card p-6 transition-colors hover:bg-accent/50"
        >
          <span className="flex size-10 items-center justify-center rounded-xl border border-border bg-background text-brand [&_svg]:size-5">
            {ICONS[t.icon]}
          </span>
          <h3 className="mt-4 font-semibold tracking-tight text-balance">{t.title}</h3>
          <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{t.summary}</p>
          <span className="mt-auto inline-flex items-center gap-1 pt-5 text-sm font-medium text-muted-foreground transition-colors group-hover:text-brand-strong">
            Open the tool <ArrowRight className="size-3.5" />
          </span>
        </Link>
      ))}
    </div>
  )
}

/** Structured data for a free browser tool, and the breadcrumb its page shows (Free tools > tool). */
export function ToolJsonLd({ tool }: { tool: Tool }) {
  const data = [
    {
      '@context': 'https://schema.org',
      '@type': 'WebApplication',
      name: tool.title,
      description: tool.summary,
      url: `${SITE_URL}/tools/${tool.slug}`,
      applicationCategory: 'BusinessApplication',
      operatingSystem: 'Web',
      isAccessibleForFree: true,
      offers: { '@type': 'Offer', price: '0', priceCurrency: 'EUR' },
      publisher: { '@type': 'Organization', '@id': ORGANIZATION['@id'], name: ORGANIZATION.name, url: SITE_URL },
    },
    breadcrumbLd([
      ['Free tools', '/tools'],
      [tool.title, `/tools/${tool.slug}`],
    ]),
  ]

  return <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: ldJson(data) }} />
}
