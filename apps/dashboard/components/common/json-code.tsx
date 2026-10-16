import { Maximize2 } from 'lucide-react'
import { Fragment, useState } from 'react'

import { CopyButton } from '@/components/common/copy-button'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

// Strings (a key when followed by ":"), literals and numbers in pretty-printed JSON.
const TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g

const KEY = 'text-sky-700 dark:text-sky-300'
const STRING = 'text-emerald-700 dark:text-emerald-300'
const NUMBER = 'text-amber-700 dark:text-amber-300'
const LITERAL = 'text-violet-700 dark:text-violet-300'

/** Syntax-coloured JSON (keys, strings, numbers, true/false/null); the text stays selectable as-is. */
export function JsonCode({ json, className, testId }: { json: string; className?: string; testId?: string }) {
  const parts: React.ReactNode[] = []
  let last = 0

  for (const m of json.matchAll(TOKEN)) {
    const at = m.index ?? 0

    if (at > last) {
      parts.push(<Fragment key={`t${last}`}>{json.slice(last, at)}</Fragment>)
    }

    if (m[1] !== undefined) {
      parts.push(
        <span key={at} className={m[2] ? KEY : STRING}>
          {m[1]}
        </span>,
      )

      if (m[2]) {
        parts.push(<Fragment key={`c${at}`}>{m[2]}</Fragment>)
      }
    } else {
      parts.push(
        <span key={at} className={m[3] ? LITERAL : NUMBER}>
          {m[0]}
        </span>,
      )
    }

    last = at + m[0].length
  }

  parts.push(<Fragment key="end">{json.slice(last)}</Fragment>)

  return (
    <pre className={cn('font-mono text-xs leading-relaxed', className)} data-testid={testId}>
      <code>{parts}</code>
    </pre>
  )
}

/**
 * JSON in a scrollable block with a full-screen button in its corner: the whole document opens in a dialog,
 * with Copy.
 */
export function JsonBlock({
  json,
  title,
  className,
  testId,
}: {
  json: string
  title: string
  className?: string
  testId?: string
}) {
  const [open, setOpen] = useState(false)

  return (
    <div className="relative">
      <JsonCode json={json} testId={testId} className={className} />
      <Button
        variant="ghost"
        size="icon-xs"
        className="absolute top-2 right-2 bg-background/70 text-muted-foreground backdrop-blur-sm"
        onClick={() => setOpen(true)}
        aria-label={`Open ${title} full screen`}
      >
        <Maximize2 />
      </Button>
      {/* Mounted on first use: every JSON block on a page would otherwise carry a dialog. */}
      {open && (
        <Dialog open onOpenChange={setOpen}>
          <DialogContent className="grid-rows-[auto_minmax(0,1fr)] sm:h-[85vh] sm:max-w-[min(92vw,1200px)]">
            <DialogHeader className="flex-row items-center gap-2 pr-8">
              <DialogTitle>{title}</DialogTitle>
              <CopyButton value={json} label={`Copy ${title}`} size="icon-xs" className="text-muted-foreground" />
            </DialogHeader>
            <JsonCode
              json={json}
              className="scrollbar-thin min-h-0 overflow-auto rounded-lg border bg-muted/40 p-4 text-[12.5px]"
            />
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
