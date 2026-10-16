'use client'

import Link from 'next/link'

import type { Item } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { CANCELED_ERROR } from '@/lib/status'
import { cn } from '@/lib/utils'

/** The processing timeout writes this into metadata.error (apps/api/internal/app/processing.go). */
const TIMEOUT_PREFIX = 'No result reported within'

/**
 * Why a failed item failed: metadata.error, set by the automation's /result or the processing timeout. A
 * timeout reads "Timed out": the decision was made, the automation never reported back. Retry is the item's main
 * action in the toolbar above, not repeated here.
 */
export function FailedNote({ item }: { item: Item }) {
  const raw = typeof item.metadata?.error === 'string' && item.metadata.error.trim() ? item.metadata.error : null
  const reason = raw ? tidyDurations(raw) : null
  const timedOut = !!reason?.startsWith(TIMEOUT_PREFIX)
  const canceled = reason === CANCELED_ERROR

  return (
    <div
      role={canceled ? 'status' : 'alert'}
      // Text only (the status badge above has the icon); a run stopped by hand is neutral, not an error.
      className={cn(
        'rounded-xl border px-3 py-2.5',
        canceled ? 'border-border bg-muted/40' : 'border-red-500/40 bg-red-500/5',
      )}
      data-testid="failed-note"
    >
      <div className="min-w-0 text-sm">
        <div className="flex flex-wrap items-baseline gap-x-2">
          <span className="font-medium">{timedOut ? 'Timed out' : canceled ? 'Canceled' : 'Failed'}</span>
          <time
            className="text-xs text-muted-foreground"
            dateTime={item.updated_at}
            title={formatDateTime(item.updated_at)}
          >
            {timeAgo(item.updated_at)}
          </time>
        </div>
        <p className="break-words whitespace-pre-wrap text-muted-foreground">
          {timedOut ? (
            <>
              {reason}: your automation never reported a result. Retry it, or{' '}
              <Link href="/settings" className="underline-offset-2 hover:text-foreground hover:underline">
                give it more time
              </Link>
              .
            </>
          ) : canceled ? (
            'Stopped by hand before your automation reported back. Retry to run it again.'
          ) : (
            (reason ?? 'No reason was reported. Retry it, or check your automation.')
          )}
        </p>
      </div>
    </div>
  )
}

/** Older messages carry Go durations ("1h0m0s"): read them as 1h, 30m. */
function tidyDurations(text: string) {
  return text
    .replace(/\b(\d+)h0m0s\b/g, '$1h')
    .replace(/\b(\d+)m0s\b/g, '$1m')
    .replace(/\b(\d+)h(\d+)m0s\b/g, '$1h$2m')
}
