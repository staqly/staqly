'use client'

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { formatInterval } from '@/lib/format'

/** The choices of the processing limit, in seconds (15 min to 1 day); '' is the server default. */
const TIMEOUTS = [900, 1800, 3600, 7200, 14400, 28800, 43200, 86400]

/**
 * How long a run (Publish, Retry) may wait for the automation's result before the item fails. The value is the
 * seconds as a string ('' = the server default) so it fits a form field; a value set through the API that is not
 * a preset is listed too.
 */
export function TimeoutSelect({
  id,
  value,
  onChange,
  fallback,
}: {
  id?: string
  value: string
  onChange: (value: string) => void
  /** The server default, named in the first option. */
  fallback: number
}) {
  const presets = TIMEOUTS.map(String)
  const options = presets.includes(value) || value === '' ? presets : [...presets, value]

  return (
    <Select value={value || 'default'} onValueChange={(v) => onChange(v === 'default' ? '' : v)}>
      <SelectTrigger id={id} className="w-full sm:w-56" aria-label="Fail publishing if nothing is reported within">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="default">Default ({fallback ? formatInterval(fallback) : 'no limit'})</SelectItem>
        {options.map((o) => (
          <SelectItem key={o} value={o}>
            {formatInterval(Number(o))}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** The form value as the API field: seconds, or null for the server default. */
export function timeoutOf(value: string): number | null {
  return value === '' ? null : Number(value)
}
