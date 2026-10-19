'use client'

import { MultiSelect } from '@/components/common/multi-select'
import { useTags } from '@/lib/queries'

/** Tags filter: any of the picked tags (the workspace's tags, most used first). */
export function TagFilter({
  value,
  onChange,
  className = 'w-36',
}: {
  value: string[]
  onChange: (v: string[]) => void
  className?: string
}) {
  const tags = useTags()

  return (
    <MultiSelect
      label="Tag filter"
      size="sm"
      className={className}
      anyLabel="Any tag"
      value={value}
      options={(tags.data ?? []).map((t) => ({ value: t.name, label: t.name }))}
      onChange={onChange}
    />
  )
}
