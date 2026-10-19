'use client'

import { Menu, X } from 'lucide-react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useEffect, useState } from 'react'

/** Hamburger menu below `lg`; closes on navigation and on Escape.
 * Absolute, not fixed: the header's backdrop-filter makes it the containing block. */
export function MobileMenu({ links, signup }: { links: [string, string][]; signup: string }) {
  const pathname = usePathname()
  // Remember which page the menu was opened on, so navigating away closes it without an effect.
  const [openOn, setOpenOn] = useState<string | null>(null)
  const open = openOn === pathname
  const setOpen = (next: boolean) => setOpenOn(next ? pathname : null)

  useEffect(() => {
    if (!open) {
      return
    }

    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpenOn(null)

    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'

    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [open])

  return (
    <div className="lg:hidden">
      <button
        type="button"
        aria-label={open ? 'Close menu' : 'Open menu'}
        aria-expanded={open}
        aria-controls="mobile-menu"
        onClick={() => setOpen(!open)}
        className="-mr-2 flex size-10 items-center justify-center rounded-lg text-muted-foreground hover:text-foreground"
      >
        {open ? <X className="size-5" /> : <Menu className="size-5" />}
      </button>
      {open && (
        <div
          id="mobile-menu"
          className="absolute inset-x-0 top-full z-30 flex h-[calc(100dvh-4rem)] flex-col gap-1 border-t border-border bg-background px-5 pt-4 pb-[max(1.5rem,env(safe-area-inset-bottom))]"
        >
          {links.map(([label, href]) => (
            <Link
              key={label}
              href={href}
              onClick={() => setOpen(false)}
              className="rounded-xl px-3 py-3.5 text-lg font-medium hover:bg-accent"
            >
              {label}
            </Link>
          ))}
          <a
            href={signup}
            className="mt-auto inline-flex h-12 items-center justify-center rounded-xl sq-rainbow font-semibold"
          >
            Start free trial
          </a>
        </div>
      )}
    </div>
  )
}
