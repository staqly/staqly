'use client'

import { setShowPast, useShowPast } from '@/hooks/use-show-past'
import { cn } from '@/lib/utils'

/**
 * "Show past posts" for the post lists and the calendar. Light markup (one button with switch semantics that looks
 * like `ui/switch`, its label inside), so the calendar does not load Radix's switch for one control.
 */
export function PastPostsSwitch({ className }: { className?: string }) {
  const on = useShowPast()

  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      onClick={() => setShowPast(!on)}
      data-testid="show-past-posts"
      className={cn(
        'group/past inline-flex items-center gap-2 rounded-md text-xs whitespace-nowrap text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background pointer-coarse:min-h-10',
        className,
      )}
    >
      <span
        aria-hidden
        className={cn(
          'relative inline-flex h-[18.4px] w-[32px] shrink-0 items-center rounded-full border border-transparent transition-all',
          on ? 'bg-brand' : 'bg-input dark:bg-input/80',
        )}
      >
        <span
          className={cn(
            'pointer-events-none block size-4 rounded-full transition-transform',
            on ? 'translate-x-[calc(100%-2px)] bg-white' : 'translate-x-0 bg-background dark:bg-foreground',
          )}
        />
      </span>
      Show past posts
    </button>
  )
}
