'use client'

import {
  Bot,
  CalendarClock,
  Check,
  ChevronDown,
  HardDrive,
  Infinity as InfinityIcon,
  LifeBuoy,
  Minus,
  Plug,
  Share2,
  Users,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'

import { IntervalToggle } from '@/components/pricing'
import {
  COMPARE,
  PLANS,
  ctaLabel,
  perMonth,
  eur,
  type Cell,
  type CompareRow,
  type Interval,
  VAT_NOTE,
} from '@/lib/plans'
import { SIGNUP, btnPrimary, btnSecondary, signupFor } from '@/lib/site'

const ICONS: Record<string, ReactNode> = {
  Publishing: <CalendarClock />,
  Channels: <Share2 />,
  Team: <Users />,
  'Automation and agents': <Bot />,
  'API and integrations': <Plug />,
  'Storage and data': <HardDrive />,
  Support: <LifeBuoy />,
}

/** Every row, by category: rows equal on every plan stay in (checks read fast and show what's included). */
const SECTIONS = COMPARE

const POPULAR = PLANS.findIndex((p) => p.highlight)

function Value({ v }: { v: Cell }) {
  if (v === true) {
    return (
      <span className="mx-auto flex size-6 items-center justify-center rounded-full bg-brand-soft text-brand-strong">
        <Check className="size-3.5" strokeWidth={3} aria-hidden />
        <span className="sr-only">Included</span>
      </span>
    )
  }

  if (v === false) {
    return (
      <span className="mx-auto flex justify-center text-muted-foreground/50">
        <Minus className="size-4" aria-hidden />
        <span className="sr-only">Not included</span>
      </span>
    )
  }

  if (v === 'Unlimited') {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-brand-soft px-2.5 py-0.5 text-xs font-semibold text-brand-strong">
        <InfinityIcon className="size-3.5" aria-hidden /> Unlimited
      </span>
    )
  }

  return <span className="font-semibold text-foreground tabular-nums">{v}</span>
}

function RowLabel({ row }: { row: CompareRow }) {
  return (
    <>
      <span className="font-medium text-foreground">{row.label}</span>
      {row.hint && <span className="mt-0.5 block text-xs font-normal text-muted-foreground">{row.hint}</span>}
    </>
  )
}

/**
 * Wide screens: a real table. The plan header sticks under the site nav (4 rem) and carries the Monthly / Yearly
 * toggle; every section folds.
 */
export function CompareTable() {
  const [interval, setInterval] = useState<Interval>('year')
  const [closed, setClosed] = useState<Set<string>>(new Set())
  const toggle = (category: string) =>
    setClosed((prev) => {
      const next = new Set(prev)

      if (next.has(category)) {
        next.delete(category)
      } else {
        next.add(category)
      }

      return next
    })

  return (
    <div className="hidden rounded-2xl border border-border bg-card/30 lg:block">
      <table className="w-full table-fixed border-collapse text-sm">
        <caption className="sr-only">Starter, Plus, Pro and Business compared</caption>
        <colgroup>
          <col className="w-[36%]" />
          {PLANS.map((p, i) => (
            <col key={p.id} className={i === POPULAR ? 'bg-brand-soft/15' : undefined} />
          ))}
        </colgroup>
        <thead>
          <tr>
            <th
              scope="col"
              className="sticky top-16 z-10 rounded-tl-2xl border-b border-border bg-card px-5 py-5 text-left align-top"
            >
              <span className="block text-lg font-semibold tracking-tight text-foreground">Compare plans</span>
              <span className="mt-1 block text-xs font-normal text-muted-foreground">
                Every plan has unlimited posts, MCP and the REST API.
              </span>
              <div className="mt-4">
                <IntervalToggle value={interval} onChange={setInterval} />
              </div>
            </th>
            {PLANS.map((p, i) => {
              const popular = i === POPULAR

              return (
                <th
                  key={p.id}
                  scope="col"
                  aria-label={p.name}
                  className={`sticky top-16 z-10 border-b border-border bg-card px-2 py-4 align-top font-normal ${
                    i === PLANS.length - 1 ? 'rounded-tr-2xl' : ''
                  }`}
                >
                  <div
                    className={`flex h-full flex-col items-center rounded-xl px-3 pt-3 pb-3 ${
                      popular
                        ? 'bg-[color-mix(in_oklch,var(--brand)_10%,var(--card))] ring-1 ring-[color-mix(in_oklch,var(--brand)_45%,transparent)]'
                        : ''
                    }`}
                  >
                    <div className="flex h-5 items-center">
                      {popular && (
                        <span className="rounded-full bg-(image:--brand-gradient) px-2 py-0.5 text-[11px] font-semibold text-(--brand-gradient-foreground)">
                          Most popular
                        </span>
                      )}
                    </div>
                    <div className="mt-1.5 text-base font-bold text-foreground">{p.name}</div>
                    <div className="mt-1 flex items-baseline gap-1 text-foreground">
                      <span className="text-3xl font-semibold tracking-tight tabular-nums">
                        {eur(perMonth(p, interval))}
                      </span>
                      <span className="text-xs text-muted-foreground">per user / month</span>
                    </div>
                    <div className="mt-0.5 h-4 text-xs text-muted-foreground tabular-nums">
                      {interval === 'year' ? `${eur(p.year)} per user, billed yearly` : 'Billed monthly'}, {VAT_NOTE}
                    </div>
                    <a
                      href={signupFor(p.id, interval)}
                      className={`${p.highlight ? btnPrimary : btnSecondary} mt-4 h-10 w-full justify-center px-2 text-sm whitespace-nowrap`}
                    >
                      {ctaLabel(p)}
                    </a>
                  </div>
                </th>
              )
            })}
          </tr>
        </thead>
        {SECTIONS.map((section) => {
          const open = !closed.has(section.category)
          const id = `compare-${section.category.toLowerCase().replaceAll(' ', '-')}`

          return (
            <tbody key={section.category}>
              <tr>
                <th scope="colgroup" colSpan={5} className="border-y border-border bg-muted/40 p-0 text-left">
                  <button
                    type="button"
                    aria-expanded={open}
                    aria-controls={id}
                    onClick={() => toggle(section.category)}
                    className="flex w-full items-center gap-2 px-5 py-3 text-xs font-semibold tracking-wide text-foreground uppercase transition-colors hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-brand focus-visible:outline-none [&_svg]:size-3.5"
                  >
                    <span className="text-brand">{ICONS[section.category]}</span>
                    {section.category}
                    <span className="ml-1 font-normal text-muted-foreground normal-case">
                      {section.rows.length} {section.rows.length === 1 ? 'item' : 'items'}
                    </span>
                    <ChevronDown
                      aria-hidden
                      className={`ml-auto text-muted-foreground transition-transform ${open ? '' : '-rotate-90'}`}
                    />
                  </button>
                </th>
              </tr>
              {open &&
                section.rows.map((row, r) => (
                  <tr
                    key={row.label}
                    id={r === 0 ? id : undefined}
                    className="border-b border-border/60 transition-colors last:border-b-0 hover:bg-accent/40"
                  >
                    <th scope="row" className="px-5 py-3 text-left font-normal">
                      <RowLabel row={row} />
                    </th>
                    {row.values.map((v, i) => (
                      <td key={PLANS[i]!.id} className="px-3 py-3 text-center">
                        <Value v={v} />
                      </td>
                    ))}
                  </tr>
                ))}
            </tbody>
          )
        })}
      </table>
    </div>
  )
}

/** Phones and tablets: pick a plan, see its values section by section. */
export function ComparePicker() {
  const [index, setIndex] = useState(Math.max(POPULAR, 0))
  const plan = PLANS[index]!

  return (
    <div className="lg:hidden">
      <div
        role="radiogroup"
        aria-label="Plan"
        className="grid grid-cols-4 gap-1 rounded-2xl border border-border bg-card p-1"
      >
        {PLANS.map((p, i) => (
          <button
            key={p.id}
            type="button"
            role="radio"
            aria-checked={i === index}
            onClick={() => setIndex(i)}
            className={`h-10 rounded-xl text-sm font-medium transition-colors ${
              i === index ? 'bg-foreground text-background' : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {p.name}
          </button>
        ))}
      </div>
      <div className="mt-4 rounded-2xl border border-border bg-card p-5" aria-live="polite">
        <div className="flex items-baseline justify-between gap-3">
          <h3 className="text-lg font-bold">{plan.name}</h3>
          {plan.highlight && (
            <span className="rounded-full bg-(image:--brand-gradient) px-2 py-0.5 text-[11px] font-semibold text-(--brand-gradient-foreground)">
              Most popular
            </span>
          )}
        </div>
        <p className="mt-1 text-3xl font-semibold tracking-tight tabular-nums">
          {eur(plan.month)} <span className="text-sm font-normal text-muted-foreground">per user / month</span>
        </p>
        <p className="text-xs text-muted-foreground tabular-nums">
          {eur(perMonth(plan, 'year'))} per user a month billed yearly
        </p>
        <a href={SIGNUP} className={`${plan.highlight ? btnPrimary : btnSecondary} mt-4 w-full justify-center`}>
          {ctaLabel(plan)}
        </a>
        {SECTIONS.map((section) => (
          <section key={section.category} className="mt-6">
            <h4 className="flex items-center gap-2 border-b border-border pb-2 text-sm font-semibold [&_svg]:size-4 [&_svg]:text-brand">
              {ICONS[section.category]} {section.category}
            </h4>
            <dl className="divide-y divide-border">
              {section.rows.map((row) => (
                <div key={row.label} className="flex items-center justify-between gap-4 py-3 text-sm">
                  <dt className="min-w-0">
                    <RowLabel row={row} />
                  </dt>
                  <dd className="shrink-0">
                    <Value v={row.values[index]!} />
                  </dd>
                </div>
              ))}
            </dl>
          </section>
        ))}
      </div>
    </div>
  )
}
