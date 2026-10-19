import { Logo } from '@staqly/brand/logo'
import Link from 'next/link'

import { cn } from '@/lib/utils'

/** Full-page frame for the public screens (create workspace, sign in, connect device). */
export function PublicShell({
  children,
  nav,
  className,
}: {
  children: React.ReactNode
  nav?: React.ReactNode
  className?: string
}) {
  return (
    <div className="relative isolate flex min-h-svh flex-col overflow-x-clip bg-background">
      <div
        aria-hidden
        className="bg-grid pointer-events-none absolute inset-x-0 top-0 -z-10 h-[520px] [mask-image:radial-gradient(ellipse_at_top,black_20%,transparent_70%)]"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute top-[-200px] left-1/2 -z-10 h-[440px] w-[min(900px,140vw)] -translate-x-1/2 rounded-full bg-[radial-gradient(closest-side,rgb(60_235_122/0.16),transparent)] blur-2xl"
      />
      <header className="mx-auto flex w-full max-w-6xl items-center justify-between gap-3 px-4 pt-[max(0.75rem,env(safe-area-inset-top))] pb-2 sm:px-6 sm:pt-5 lg:px-8">
        <Link
          href="/"
          className="flex min-h-11 items-center rounded-lg outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          aria-label="Staqly home"
        >
          <Logo className="text-lg" markClassName="size-8" />
        </Link>
        {nav}
      </header>
      <main
        className={cn(
          'mx-auto w-full max-w-6xl flex-1 px-4 pb-[max(2.5rem,env(safe-area-inset-bottom))] sm:px-6 lg:px-8',
          className,
        )}
      >
        {children}
      </main>
    </div>
  )
}
