'use client'

import { useState } from 'react'

import { MultiSelect } from '@/components/common/multi-select'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import type { MediaLibraryItem } from '@/lib/api'
import { normalizeTag } from '@/lib/tags'

const files = (n: number) => `${n} ${n === 1 ? 'file' : 'files'}`

/** Tags for several files at once: tags to add, and tags (that the files carry) to remove. Mount it to open. */
export function TagsDialog({
  open,
  onOpenChange,
  items,
  known,
  pending,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  items: MediaLibraryItem[]
  known: string[]
  pending: boolean
  onSubmit: (add: string[], remove: string[]) => void
}) {
  const [add, setAdd] = useState<string[]>([])
  const [remove, setRemove] = useState<string[]>([])
  const carried = [...new Set(items.flatMap((m) => m.tags ?? []))].toSorted()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" data-testid="media-tags-dialog">
        <DialogHeader>
          <DialogTitle>Tags for {files(items.length)}</DialogTitle>
          <DialogDescription>Tags help you find files; filter the library by a tag.</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            onSubmit(add, remove)
          }}
        >
          <div className="grid gap-2">
            <Label htmlFor="media-add-tags">Add tags</Label>
            <MultiSelect
              id="media-add-tags"
              label="Add tags"
              placeholder="summer-sale"
              className="w-full"
              value={add}
              onChange={setAdd}
              options={known.map((t) => ({ value: t, label: `#${t}` }))}
              creatable={{ normalize: normalizeTag }}
            />
          </div>
          {carried.length > 0 && (
            <div className="grid gap-2">
              <Label htmlFor="media-remove-tags">Remove tags</Label>
              <MultiSelect
                id="media-remove-tags"
                label="Remove tags"
                placeholder="Pick tags to remove"
                className="w-full"
                value={remove}
                onChange={setRemove}
                options={carried.map((t) => ({ value: t, label: `#${t}` }))}
              />
            </div>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || (add.length === 0 && remove.length === 0)}>
              {pending ? 'Saving...' : 'Apply'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
