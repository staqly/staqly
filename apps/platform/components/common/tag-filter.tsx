'use client'

import { MultiSelect } from '@/components/common/multi-select'
import { useTags } from '@/lib/queries'

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
