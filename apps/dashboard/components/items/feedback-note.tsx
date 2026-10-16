import { MessageSquareQuote } from 'lucide-react'

import { DisclosureCard } from '@/components/common/disclosure-card'
import type { Item } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'

/**
 * Reviewer feedback in the same expandable card as event data. The header shows the start of the text, so a
 * collapsed card still says what it is about; Copy appears once it is open.
 */
export function FeedbackCard({
  text,
  when,
  defaultOpen = false,
  className,
}: {
  text: string
  when?: string
  defaultOpen?: boolean
  className?: string
}) {
  return (
    <DisclosureCard
      className={className}
      label="Feedback"
      copy={text}
      defaultOpen={defaultOpen}
      bodyTestId="feedback-body"
      testId="feedback-note"
      header={
        <>
          <MessageSquareQuote aria-hidden className="size-3.5 shrink-0" />
          <span className="shrink-0 font-medium text-foreground/80">Feedback</span>
        </>
      }
      preview={<span className="min-w-0 truncate">{text.split('\n')[0]}</span>}
      meta={
        when && (
          <time dateTime={when} title={formatDateTime(when)}>
            {timeAgo(when)}
          </time>
        )
      }
    >
      <blockquote className="px-3 py-2.5 text-sm leading-relaxed break-words whitespace-pre-wrap text-foreground">
        {text}
      </blockquote>
    </DisclosureCard>
  )
}

/** The item's latest feedback, open by default where it matters most: on the item itself. */
export function FeedbackNote({ item, className }: { item: Item; className?: string }) {
  return item.feedback ? (
    <FeedbackCard text={item.feedback} when={item.decided_at ?? item.updated_at} defaultOpen className={className} />
  ) : null
}
