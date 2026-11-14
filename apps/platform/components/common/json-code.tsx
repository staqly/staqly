import { Fragment } from 'react'

import { cn } from '@/lib/utils'

// Strings (a key when followed by ":"), literals and numbers in pretty-printed JSON.
const TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g

const KEY = 'text-sky-700 dark:text-sky-300'
const STRING = 'text-emerald-700 dark:text-emerald-300'
const NUMBER = 'text-amber-700 dark:text-amber-300'
const LITERAL = 'text-violet-700 dark:text-violet-300'

/** Pretty-printed JSON as coloured tokens (keys, strings, numbers, true/false/null); the text stays as-is. */
export function jsonTokens(json: string): React.ReactNode[] {
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

  return parts
}

/** Syntax-coloured JSON in a block; the text stays selectable as-is. */
export function JsonCode({ json, className, testId }: { json: string; className?: string; testId?: string }) {
  const parts = jsonTokens(json)

  return (
    <pre className={cn('font-mono text-xs leading-relaxed', className)} data-testid={testId}>
      <code>{parts}</code>
    </pre>
  )
}
