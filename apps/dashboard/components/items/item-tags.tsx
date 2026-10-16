'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Pencil, Tag } from 'lucide-react'
import { useId } from 'react'

import { ChipsInput } from '@/components/common/chips-input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { api, type Item } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, qk, syncItemInCache } from '@/lib/queries'
import { cn } from '@/lib/utils'

/** An item's tags as small chips (lists, headers). */
export function TagList({ tags, className }: { tags?: string[]; className?: string }) {
  if (!tags?.length) {
    return null
  }

  return (
    <span className={cn('inline-flex flex-wrap items-center gap-1', className)} data-testid="item-tags">
      {tags.map((t) => (
        <span
          key={t}
          className="inline-flex h-5 items-center gap-0.5 rounded-md bg-muted px-1.5 text-[11px] text-muted-foreground"
        >
          <Tag aria-hidden className="size-2.5" />
          {t}
        </span>
      ))}
    </span>
  )
}

/** Edit tags where the Edit tab is gone (locked content): every change saves, without a new version. */
export function TagsPopover({ item }: { item: Item }) {
  const qc = useQueryClient()
  const inputId = useId()
  const save = useMutation({
    mutationFn: (tags: string[]) => api.updateItem(item.id, { tags }),
    onSuccess: (updated) => {
      syncItemInCache(qc, updated)
      invalidateAfterItemChange(qc, updated.id)
      void qc.invalidateQueries({ queryKey: qk.tags })
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
          aria-label="Edit tags"
        >
          <Pencil className="size-3" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-80 p-2"
        // Focus the text box, not the first chip's remove button (Enter there would delete that tag).
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          document.getElementById(inputId)?.focus()
        }}
      >
        <ChipsInput
          id={inputId}
          label="Tags"
          value={save.isPending && save.variables ? save.variables : (item.tags ?? [])}
          onChange={(tags) => save.mutate(tags)}
          placeholder="Add a tag, press Enter"
          max={20}
          maxLength={40}
        />
      </PopoverContent>
    </Popover>
  )
}
