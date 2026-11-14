'use client'

import { ArrowRight, Check, ChevronDown, Copy } from 'lucide-react'
import { useState, type ComponentProps, type ReactNode } from 'react'

import { SIGNUP, btnPrimary } from '@/lib/site'

/**
 * An icon that copies `text`, placed inside the box it copies (top right). The label is its accessible name and
 * hover title; "Copied" is announced for two seconds.
 */
export function CopyButton({
  text,
  label = 'Copy',
  className = '',
}: {
  text: string
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)

  return (
    <button
      type="button"
      aria-label={label}
      title={copied ? 'Copied' : label}
      className={`inline-flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-card/90 text-muted-foreground backdrop-blur transition-colors hover:bg-accent hover:text-foreground disabled:opacity-50 pointer-coarse:size-10 ${className}`}
      disabled={!text}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          setTimeout(() => setCopied(false), 2000)
        } catch {
          // ponytail: no clipboard (insecure context, denied): the text stays selectable on the page.
        }
      }}
    >
      {copied ? <Check className="size-4 text-brand" /> : <Copy className="size-4" />}
      <span className="sr-only" aria-live="polite">
        {copied ? 'Copied' : ''}
      </span>
    </button>
  )
}

/** The soft ask under every tool: what Staqly does with the same idea. */
export function ToolCta({ title, children }: { title: string; children: ReactNode }) {
  return (
    <aside className="mt-10 flex flex-col gap-4 rounded-2xl border border-[color-mix(in_oklch,var(--brand)_30%,transparent)] bg-brand-soft/30 p-6 sm:flex-row sm:items-center sm:justify-between sm:p-7">
      <div className="max-w-xl">
        <h2 className="font-semibold tracking-tight">{title}</h2>
        <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{children}</p>
      </div>
      <a href={SIGNUP} className={`${btnPrimary} shrink-0 self-start sm:self-center`}>
        Try Staqly free <ArrowRight className="size-4" />
      </a>
    </aside>
  )
}

export const fieldClass =
  'w-full rounded-xl border border-border bg-background px-3 py-2.5 text-sm text-foreground outline-none placeholder:text-muted-foreground/70 focus-visible:border-brand focus-visible:ring-2 focus-visible:ring-[color-mix(in_oklch,var(--brand)_35%,transparent)]'

export const selectClass = `${fieldClass} h-10 cursor-pointer appearance-none bg-card py-0 pr-9 transition-colors [color-scheme:dark] hover:border-foreground/20 pointer-coarse:h-11`

/** A native select (phones keep their own picker) drawn like the site's fields, with our chevron instead of the browser's. */
export function Select({ className, ...props }: ComponentProps<'select'>) {
  return (
    <span className="relative block">
      <select className={`${selectClass} ${className ?? ''}`} {...props} />
      <ChevronDown
        aria-hidden
        className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-muted-foreground"
      />
    </span>
  )
}

let allZones: string[] | undefined

function zones(): string[] {
  if (!allZones) {
    try {
      allZones = Intl.supportedValuesOf('timeZone')
    } catch {
      allZones = ['UTC']
    }
  }

  return allZones
}

/**
 * A time zone select. The full list (over 400 zones) is filled in the moment someone reaches for the field (pointer,
 * focus or key), so the page doesn't render and hydrate hundreds of options nobody opens; until then it holds the
 * chosen zone, the visitor's own and UTC.
 */
export function ZoneSelect({
  value,
  mine,
  onChange,
}: {
  value: string
  mine: string
  onChange: (zone: string) => void
}) {
  const [full, setFull] = useState(false)
  const list = full ? [...new Set([mine, 'UTC', ...zones(), value])] : [...new Set([value, mine, 'UTC'])]
  const open = () => setFull(true)

  return (
    <Select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onPointerDown={open}
      onFocus={open}
      onKeyDown={open}
    >
      {list.map((z) => (
        <option key={z} value={z}>
          {z === mine ? `${z} (yours)` : z}
        </option>
      ))}
    </Select>
  )
}
