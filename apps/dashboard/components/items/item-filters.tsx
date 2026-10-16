'use client'

import { MultiSelect } from '@/components/common/multi-select'
import { useTags } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'

export const DECISION_OPTIONS = (['pending', 'approved', 'rejected', 'expired'] as const).map((v) => ({
  value: v,
  label: STATUS_META[v].label,
  dot: STATUS_META[v].dot,
}))

export const RUN_OPTIONS = [
  { value: 'none', label: 'Not run' },
  ...(['processing', 'completed', 'failed'] as const).map((v) => ({
    value: v,
    label: STATUS_META[v].label,
    dot: STATUS_META[v].dot,
  })),
]

/** Tags filter: any of the picked tags (the workspace's tags, most used first). */
export function TagFilter({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const tags = useTags()

  return (
    <MultiSelect
      label="Tag filter"
      size="sm"
      className="w-36"
      anyLabel="Any tag"
      value={value}
      options={(tags.data ?? []).map((t) => ({ value: t.tag, label: t.tag }))}
      onChange={onChange}
    />
  )
}
