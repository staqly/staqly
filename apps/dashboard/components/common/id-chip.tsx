import { CopyButton } from '@/components/common/copy-button'
import { cn } from '@/lib/utils'

/** An ID (item, collection, workspace, event...): monospace on a tinted chip, with a copy button. */
export function IdChip({
  id,
  label,
  copy = true,
  className,
}: {
  id: string
  label: string
  copy?: boolean
  className?: string
}) {
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-0.5', className)}>
      <code
        className="truncate rounded-md bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground/80 ring-1 ring-border/60 ring-inset"
        title={id}
      >
        {id}
      </code>
      {copy && (
        <CopyButton value={id} label={`Copy ${label} ID`} size="icon-xs" className="shrink-0 text-muted-foreground" />
      )}
    </span>
  )
}
