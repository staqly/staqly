'use client'

import { useQuery } from '@tanstack/react-query'
import { LoaderCircle, Send } from 'lucide-react'
import Link from 'next/link'

import { api, type Item } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { qk } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

/** Sending (first try, spinner), Retrying (after an error, waiting for the next try), Delivered, Failed. */
function deliveryState(d: { status: string; attempts: number; last_status_code: number | null }) {
  const code = d.last_status_code ? ` · ${d.last_status_code}` : ''

  if (d.status === 'succeeded') {
    return { label: `Delivered${code}`, tone: TONES.success.badge }
  }

  if (d.status === 'failed') {
    return { label: `Failed${code}`, tone: TONES.danger.badge }
  }

  if (d.attempts > 0) {
    return { label: `Retrying${code}`, tone: TONES.warning.badge }
  }

  return { label: 'Sending', tone: TONES.info.badge, busy: true }
}

/**
 * What happened after a decision: the webhook deliveries of this item's events (approved, rejected,
 * publish requested...), or a plain note that no webhook was told. Agents waiting over MCP or the API see the
 * decision by themselves.
 */
export function ItemDeliveries({ item }: { item: Item }) {
  const deliveries = useQuery({
    queryKey: ['deliveries', 'item', item.id, item.version],
    queryFn: async () => (await api.deliveries({ item_id: item.id, limit: 20 })).data ?? [],
    refetchInterval: (q) => ((q.state.data ?? []).some((d) => d.status === 'pending') ? 3000 : false),
  })
  const webhooks = useQuery({ queryKey: qk.webhooks, queryFn: async () => (await api.webhooks()).data ?? [] })
  const url = (id: string) => webhooks.data?.find((w) => w.id === id)?.url ?? id
  const list = deliveries.data ?? []

  if (deliveries.isLoading || deliveries.error) {
    return null
  }

  return (
    // min-w-0: a long error must wrap inside the panel, not widen the sheet.
    <section className="min-w-0 rounded-xl border bg-card p-4" data-testid="item-deliveries">
      <div className="mb-2 flex items-center gap-2">
        <Send className="size-3.5 text-muted-foreground" aria-hidden />
        <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">What happened next</h3>
        <Link href="/webhooks" className="ml-auto text-xs text-muted-foreground hover:text-foreground hover:underline">
          Webhooks
        </Link>
      </div>
      {list.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No webhook was notified about this item. Agents waiting on it over MCP or the API see the decision themselves;
          to push decisions to n8n or your own service,{' '}
          <Link href="/webhooks" className="text-foreground underline underline-offset-2 hover:no-underline">
            add a webhook
          </Link>
          .
        </p>
      ) : (
        <ul className="divide-y text-sm">
          {list.map((d) => (
            <li key={d.id} className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 py-2">
              <span className="font-mono text-xs">{d.event_type}</span>
              <span className="min-w-0 flex-1 truncate text-muted-foreground" title={url(d.webhook_id)}>
                → {url(d.webhook_id)}
              </span>
              <DeliveryBadge d={d} />
              <time
                className="text-xs text-muted-foreground"
                dateTime={d.created_at}
                title={formatDateTime(d.created_at)}
              >
                {timeAgo(d.created_at)}
              </time>
              {d.status === 'pending' && d.attempts > 0 && (
                <p className="line-clamp-2 w-full text-xs break-words text-muted-foreground" title={d.last_error}>
                  Attempt {d.attempts} failed{d.last_error ? `: ${d.last_error}` : ''}
                  {d.next_attempt_at ? `. Next try ${timeAgo(d.next_attempt_at)}` : ''}
                </p>
              )}
              {d.status === 'failed' && d.last_error && (
                <p
                  className="line-clamp-2 w-full text-xs break-words text-red-600 dark:text-red-400"
                  title={d.last_error}
                >
                  {d.last_error}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function DeliveryBadge({ d }: { d: Parameters<typeof deliveryState>[0] }) {
  const st = deliveryState(d)

  return (
    <span
      className={cn(
        'inline-flex h-5 items-center gap-1 rounded-full px-2 text-[11px] font-medium ring-1 ring-inset',
        st.tone,
      )}
    >
      {st.busy && <LoaderCircle aria-hidden className="size-3 animate-spin" />}
      {st.label}
    </span>
  )
}
