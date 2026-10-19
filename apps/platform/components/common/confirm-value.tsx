import { CopyButton } from './copy-button'

/** The exact text to type into a "type it to confirm" field, in a code box with a copy button. */
export function ConfirmValue({ value }: { value: string }) {
  return (
    <span className="flex items-center gap-1 rounded-md border bg-muted/50 py-1 pr-1 pl-2.5 font-mono text-sm">
      <span className="min-w-0 flex-1 truncate select-all" data-testid="confirm-value">
        {value}
      </span>
      <CopyButton value={value} label={`Copy ${value}`} size="icon-xs" />
    </span>
  )
}
