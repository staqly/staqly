'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'

/** True when `href` is a page link (not a home anchor or the docs) and the page or one under it is open. */
export function isActive(pathname: string, href: string) {
  if (!href.startsWith('/') || href.includes('#')) {
    return false
  }

  const path = pathname.replace(/\/$/, '')

  return path === href || path.startsWith(`${href}/`)
}

/** Header links at `lg` and up; the open page is marked with aria-current. */
export function NavLinks({ links }: { links: [string, string][] }) {
  const pathname = usePathname()

  return (
    <nav className="ml-auto hidden gap-6 text-sm whitespace-nowrap text-muted-foreground lg:flex">
      {links.map(([label, href]) => (
        <Link
          key={label}
          href={href}
          aria-current={isActive(pathname, href) ? 'page' : undefined}
          className="py-0.5 hover:text-foreground aria-[current=page]:font-medium aria-[current=page]:text-foreground pointer-coarse:py-2.5"
        >
          {label}
        </Link>
      ))}
    </nav>
  )
}
