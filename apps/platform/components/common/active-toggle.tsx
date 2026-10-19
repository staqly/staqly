import { Switch } from '@/components/ui/switch'

/** On / off at the top of a form (webhooks, notification channels), with what each state means. */
export function ActiveToggle({
  id,
  checked,
  onChange,
  on,
  off,
}: {
  id: string
  checked: boolean
  onChange: (v: boolean) => void
  on: string
  off: string
}) {
  return (
    <label htmlFor={id} className="flex cursor-pointer items-center gap-3 rounded-lg border bg-muted/30 px-3 py-2.5">
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
      <span className="grid">
        <span className="text-sm font-medium">{checked ? 'Active' : 'Paused'}</span>
        <span className="text-xs text-muted-foreground">{checked ? on : off}</span>
      </span>
    </label>
  )
}
