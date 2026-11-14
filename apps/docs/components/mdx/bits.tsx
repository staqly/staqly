import Link from 'next/link'
import type { ReactNode } from 'react'

import { operationFor } from '@/lib/reference'

/** Small inline label, e.g. <Badge>cloud</Badge>. */
export function Badge({
  children,
  tone = 'neutral',
}: {
  children: ReactNode
  tone?: 'neutral' | 'brand' | 'success' | 'warn' | 'soon'
}) {
  return <span className={`sq-badge sq-badge-${tone}`}>{children}</span>
}

/** Keyboard key, e.g. <Kbd>j</Kbd>. */
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="sq-kbd">{children}</kbd>
}

/** HTTP method + path, e.g. <Method m="POST" path="/v1/posts" />, linked to its API reference page when it has one. */
export function Method({ m, path }: { m: 'GET' | 'POST' | 'PATCH' | 'DELETE' | 'PUT'; path: string }) {
  const op = operationFor(m, path)
  const label = (
    <>
      <span className={`sq-method sq-method-${m.toLowerCase()}`}>{m}</span>
      <code>{path}</code>
    </>
  )

  return op ? (
    <Link href={op.url} prefetch={false} className="sq-endpoint" title={op.title}>
      {label}
    </Link>
  ) : (
    <span className="sq-endpoint">{label}</span>
  )
}

/** Banner for features that are planned but not available yet. */
export function ComingSoon({ children }: { children?: ReactNode }) {
  return (
    <div className="sq-coming-soon">
      <span className="sq-badge sq-badge-brand">Coming soon</span>
      <div>{children}</div>
    </div>
  )
}

/** "Yes" in the Required column of parameter tables. */
export function Required() {
  return <span className="font-medium text-red-400">Yes</span>
}
