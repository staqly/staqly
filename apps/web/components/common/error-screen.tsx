'use client'

import { RotateCcw } from 'lucide-react'
import { useEffect } from 'react'

import { LogoMark } from '@/components/common/logo'
import { Button } from '@/components/ui/button'

/** Full-page "something broke" screen used by app/error.tsx and app/global-error.tsx. */
export function ErrorScreen({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  useEffect(() => {
    console.error(error)
  }, [error])

  return (
    <div role="alert" className="flex min-h-svh flex-col items-center justify-center gap-4 p-6 text-center">
      <LogoMark className="size-10" />
      <div>
        <h1 className="text-xl font-semibold">Something went wrong</h1>
        <p className="mt-1 max-w-sm text-sm text-muted-foreground">
          staqly hit an unexpected error. Your data is safe — try again, or reload the page.
        </p>
        {error.digest && <p className="mt-2 font-mono text-xs text-muted-foreground">Error ID: {error.digest}</p>}
      </div>
      <div className="flex gap-2">
        <Button onClick={() => retry()}>
          <RotateCcw /> Try again
        </Button>
        <Button variant="outline" asChild>
          {/* A full navigation (not <Link>) so a broken client state is discarded. */}
          {/* oxlint-disable-next-line nextjs/no-html-link-for-pages -- deliberate hard reload */}
          <a href="/inbox">Back to inbox</a>
        </Button>
      </div>
    </div>
  )
}
