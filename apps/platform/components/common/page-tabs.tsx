'use client'

import Link from 'next/link'
import { useEffect, useRef } from 'react'

import { useScrollFade } from '@/hooks/use-scroll-fade'
import { cn } from '@/lib/utils'

export type PageTab = { href: string; label: string; active: boolean; testId?: string }

/**
 * A page's section tabs (links): pills like the in-page tabs, scrolling sideways on a phone. An end that hides more
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
    <div className={cn('-mx-4 sm:mx-0', className)}>
      <nav
        ref={nav}
        aria-label={label}
        data-fade={fade.fade ?? undefined}
        className={cn(
          'scrollbar-none mx-4 flex w-fit max-w-[calc(100%-2rem)] gap-0.5 overflow-x-auto rounded-lg bg-muted p-[3px] sm:mx-0 sm:max-w-full dark:bg-white/[0.06]',
          fade.className,
        )}
      >
        {tabs.map((t) => (
          <Link
            key={t.href}
            href={t.href}
            scroll={false}
            aria-current={t.active ? 'page' : undefined}
            data-testid={t.testId}
            className={cn(
              // The same pills as the in-page tabs (components/ui/tabs.tsx), so every tab strip looks alike.
              'inline-flex h-[26px] shrink-0 items-center justify-center rounded-md pointer-coarse:min-w-10 border border-transparent px-2.5 whitespace-nowrap text-sm font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:h-10',
              t.active
                ? 'bg-background text-foreground shadow-sm dark:border-foreground/25 dark:bg-foreground/20 dark:shadow-none'
                : 'text-foreground/70 hover:text-foreground dark:text-foreground/75',
            )}
          >
            {t.label}
          </Link>
        ))}
      </nav>
    </div>
  )
}
