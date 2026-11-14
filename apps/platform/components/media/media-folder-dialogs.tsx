'use client'

import { Check, Folder, Images } from 'lucide-react'
import { useState } from 'react'

import { ROW } from '@/components/media/media-folders'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { MediaFolder } from '@/lib/api'
import { folderTree } from '@/lib/media'
import { cn } from '@/lib/utils'

/** Pick a folder (or No folder) from the tree; picking moves right away. `current` is marked, not offered. */
export function FolderPickerDialog({
  open,
  onOpenChange,
  title,
  folders,
  current,
  exclude,
  onPick,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  folders: MediaFolder[]
  current?: string
  exclude?: Set<string>
  onPick: (folderId: string) => void
}) {
  const options = [
    { id: '', name: 'No folder', depth: 0 },
    ...folderTree(folders)
      .filter((f) => !exclude?.has(f.id))
      .map((f) => ({ id: f.id, name: f.name, depth: f.depth })),
  ]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm" data-testid="media-folder-picker">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>Folders only organize the library; posts keep their files.</DialogDescription>
        </DialogHeader>
        <ul className="-mx-2 grid max-h-80 gap-0.5 overflow-y-auto p-1" aria-label="Folders">
          {options.map((o) => {
            const here = o.id === current

            return (
              <li key={o.id || 'none'}>
                <button
                  type="button"
                  disabled={here}
                  onClick={() => {
                    onPick(o.id)
                    onOpenChange(false)
                  }}
                  style={{ paddingLeft: `${0.5 + o.depth}rem` }}
                  className={cn(ROW, 'h-8 hover:bg-accent/60 disabled:cursor-default disabled:hover:bg-transparent')}
                >
                  {o.id ? (
                    <Folder className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                  ) : (
                    <Images className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                  )}
                  <span className="min-w-0 flex-1 truncate">{o.name}</span>
                  {here && (
                    <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
                      <Check className="size-3.5" aria-hidden /> Here now
                    </span>
                  )}
                </button>
              </li>
            )
          })}
        </ul>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** New folder or rename: one name field. Mount it when it opens (it starts from `initial`). */
export function FolderNameDialog({
  open,
  onOpenChange,
  title,
  description,
  initial = '',
  confirmLabel,
  pending,
  error,
  onSubmit,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  initial?: string
  confirmLabel: string
  pending?: boolean
  error?: string
  onSubmit: (name: string) => void
}) {
  const [name, setName] = useState(initial)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" data-testid="media-folder-dialog">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <form
          noValidate
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()

            if (name.trim()) {
              onSubmit(name.trim())
            }
          }}
        >
          <Field data-invalid={!!error || undefined}>
            <FieldLabel htmlFor="media-folder-name" required>
              Name
            </FieldLabel>
            <Input
              id="media-folder-name"
              value={name}
              maxLength={80}
              placeholder="Clients"
              aria-required
              aria-invalid={!!error || undefined}
              onChange={(e) => setName(e.target.value)}
            />
            <FieldError>{error}</FieldError>
          </Field>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || pending}>
              {pending ? 'Saving...' : confirmLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
