import { HomeLayout } from 'fumadocs-ui/layouts/home'
import Link from 'next/link'

import { LegacyRedirect } from '@/components/legacy-redirect'
import { LogoMark } from '@/components/logo'
import { env } from '@/lib/env.mjs'
import { baseOptions } from '@/lib/layout.shared'

export default function NotFound() {
  return (
    <HomeLayout {...baseOptions()}>
      <LegacyRedirect />
      <main className="flex flex-1 flex-col items-center justify-center px-6 py-24 text-center">
        <LogoMark className="size-12" />
        <p className="mt-6 font-mono text-sm text-brand-strong">404</p>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight">This page isn’t in the stack</h1>
        <p className="mt-3 max-w-md text-fd-muted-foreground">
          It may have moved. Try the search (<kbd className="sq-kbd">⌘</kbd> <kbd className="sq-kbd">K</kbd>) or start
          from the docs home.
        </p>
        <div className="mt-8 flex flex-wrap justify-center gap-3">
          <Link
            href="/"
            className="inline-flex h-10 items-center rounded-xl bg-fd-primary px-4 text-sm font-semibold text-fd-primary-foreground"
          >
            Docs home
          </Link>
          <a
            href={env.webUrl}
            className="inline-flex h-10 items-center rounded-xl border border-fd-border px-4 text-sm font-semibold hover:bg-fd-accent"
          >
            staqly.ai
          </a>
        </div>
      </main>
    </HomeLayout>
  )
}
