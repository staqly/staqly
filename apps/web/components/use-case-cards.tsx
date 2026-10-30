import { ArrowRight } from 'lucide-react'
import Link from 'next/link'

import type { UseCase } from '@/lib/use-cases'

export function UseCaseCards({ items }: { items: UseCase[] }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {items.map((u) => (
        <Link
          key={u.slug}
          href={`/use-cases/${u.slug}`}
          className="group relative flex flex-col overflow-hidden rounded-2xl border border-border bg-card p-6 transition-colors hover:border-[color-mix(in_oklch,var(--brand)_45%,transparent)] hover:bg-accent/50 sm:p-7"
        >
          <span
            aria-hidden
            className="absolute inset-x-0 top-0 h-px bg-(image:--brand-gradient) opacity-0 transition-opacity group-hover:opacity-100"
          />
          <p className="font-mono text-xs text-brand-strong">{u.steps.length} steps</p>
          <h3 className="mt-3 text-lg leading-snug font-semibold tracking-tight text-balance">{u.title}</h3>
          <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{u.summary}</p>
          <span className="mt-auto inline-flex items-center gap-1 pt-5 text-sm font-medium text-muted-foreground transition-colors group-hover:text-brand-strong">
            See how it runs <ArrowRight className="size-3.5" />
          </span>
        </Link>
      ))}
    </div>
  )
}
