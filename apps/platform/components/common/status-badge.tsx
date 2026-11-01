import { channelStatusMeta, postStatusMeta, targetStatusMeta, UNPUBLISHED_META, type StatusMeta } from '@/lib/status'
import { cn } from '@/lib/utils'

const PILL =
  'inline-flex h-5 shrink-0 items-center gap-1 rounded-full pr-2 pl-1.5 text-[11px] font-medium whitespace-nowrap ring-1 ring-inset'

/**
 * One status as a badge, one look for all: a tinted pill with its icon, coloured by the status's tone
 * (lib/tones.ts). The publishing icon spins while something is being published right now.
 */
function Badge({
  meta,
  status,
  title,
  className,
  describedBy,
}: {
  meta: StatusMeta
  status: string
  title?: string
  className?: string
  /** The id of text that explains the status (why a channel needs reconnecting), read with it. */
  describedBy?: string
}) {
  const spin = status === 'publishing'

  return (
    <span
      data-status={status}
      title={title || meta.description}
      aria-describedby={describedBy}
      className={cn(PILL, meta.badge, className)}
    >
      <meta.Icon aria-hidden className={cn('size-3', spin && 'animate-spin')} />
      {meta.label}
    </span>
  )
}

/** A post's status (Draft, Scheduled, Publishing, Published, Failed); "Unpublished" once it was taken down. */
export function PostStatusBadge({
  status,
  unpublishedAt,
  title,
  className,
}: {
  status: string
  unpublishedAt?: string | null
  title?: string
  className?: string
}) {
  return unpublishedAt ? (
    <Badge meta={UNPUBLISHED_META} status="unpublished" title={title} className={className} />
  ) : (
    <Badge meta={postStatusMeta(status)} status={status} title={title} className={className} />
  )
}

/** One channel's state for a post (Waiting, Publishing, Published, Failed). */
export function TargetStatusBadge({
  status,
  title,
  className,
}: {
  status: string
  title?: string
  className?: string
}) {
  return <Badge meta={targetStatusMeta(status)} status={status} title={title} className={className} />
}

/** A connected channel's state (Connected, Needs reconnect, Disabled). */
export function ChannelStatusBadge({
  status,
  title,
  className,
  describedBy,
}: {
  status: string
  title?: string
  className?: string
  describedBy?: string
}) {
  return (
    <Badge
      meta={channelStatusMeta(status)}
      status={status}
      title={title}
      className={className}
      describedBy={describedBy}
    />
  )
}
