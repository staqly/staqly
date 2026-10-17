'use client'

import Link from 'next/link'
import { useEffect, useRef } from 'react'

import { cn } from '@/lib/utils'

export type PageTab = { href: string; label: string; active: boolean; testId?: string }

/** A page's section tabs (links): full width, underlined, scrolling sideways on a phone. */
export function PageTabs({ tabs, label, className }: { tabs: PageTab[]; label: string; className?: string }) {
  const nav = useRef<HTMLElement>(null)
  const activeIndex = tabs.findIndex((t) => t.active)

  // On a phone the tabs scroll sideways: bring the current one into view.
  // Only the strip scrolls; scrollIntoView would also move the page.
  useEffect(() => {
    const el = nav.current
    const active = el?.children[activeIndex]

    if (!el || !active) {
      return
    }

    const box = el.getBoundingClientRect()
    const tab = active.getBoundingClientRect()

    if (tab.left < box.left || tab.right > box.right) {
      el.scrollLeft += tab.left - box.left - (box.width - tab.width) / 2
    }
  }, [activeIndex])

  return (
    <nav
      ref={nav}
      aria-label={label}
      className={cn('scrollbar-none -mx-4 flex gap-1 overflow-x-auto border-b px-4 sm:mx-0 sm:px-0', className)}
    >
      {tabs.map((t) => (
        <Link
          key={t.href}
          href={t.href}
          scroll={false}
          aria-current={t.active ? 'page' : undefined}
          data-testid={t.testId}
          className={cn(
            'relative -mb-px inline-flex h-10 shrink-0 items-center px-3 whitespace-nowrap text-sm font-medium transition-colors pointer-coarse:h-11',
            t.active
              ? 'text-foreground after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-brand'
              : 'text-muted-foreground hover:text-foreground',
          )}
        >
          {t.label}
        </Link>
      ))}
    </nav>
  )
}
