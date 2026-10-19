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
          className="group flex flex-col rounded-2xl border border-border bg-card p-6 transition-colors hover:bg-accent/50"
        >
          <h3 className="font-semibold tracking-tight text-balance">{u.title}</h3>
          <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{u.summary}</p>
          <span className="mt-auto inline-flex items-center gap-1 pt-5 text-sm font-medium text-muted-foreground transition-colors group-hover:text-brand-strong">
            Read more <ArrowRight className="size-3.5" />
          </span>
        </Link>
      ))}
    </div>
  )
}
