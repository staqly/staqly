'use client'

import Link from 'next/link'
import { useEffect, useRef } from 'react'

import { useScrollFade } from '@/hooks/use-scroll-fade'
import { cn } from '@/lib/utils'

export type PageTab = { href: string; label: string; active: boolean; testId?: string }

/**
 * A page's section tabs (links): full width, underlined, scrolling sideways on a phone. An end that hides more
 * tabs fades out, so the strip reads as scrollable.
 */
export function PageTabs({ tabs, label, className }: { tabs: PageTab[]; label: string; className?: string }) {
  const nav = useRef<HTMLElement>(null)
  const activeIndex = tabs.findIndex((t) => t.active)
  const fade = useScrollFade(nav)

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
    // The border is on the wrapper, so it runs the full width under the faded ends.
    <div className={cn('-mx-4 border-b sm:mx-0', className)}>
      <nav
        ref={nav}
        aria-label={label}
        data-fade={fade.fade ?? undefined}
        className={cn('scrollbar-none flex gap-1 overflow-x-auto px-4 sm:px-0', fade.className)}
      >
        {tabs.map((t) => (
          <Link
            key={t.href}
            href={t.href}
            scroll={false}
            aria-current={t.active ? 'page' : undefined}
            data-testid={t.testId}
            className={cn(
              'relative inline-flex h-10 shrink-0 items-center px-3 whitespace-nowrap text-sm font-medium transition-colors pointer-coarse:h-11',
              t.active
                ? 'text-foreground after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-brand'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {t.label}
          </Link>
        ))}
      </nav>
    </div>
  )
}
