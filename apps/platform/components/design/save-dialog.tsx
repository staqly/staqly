'use client'

import { useQuery } from '@tanstack/react-query'
import { Check, Folder, FolderOpen, Images, Save } from 'lucide-react'
import { useState } from 'react'

import { mediaKeys } from '@/components/media/media-cache'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { folderTree } from '@/lib/media'

/**
 * The first save of a new design: its name and the library folder its pictures go to (the top level unless one is
 * picked). Saving again replaces the pictures where they are, so it asks only once.
 */
export function SaveDialog({
  name,
  pages = 1,
  description,
  onSave,
  onClose,
}: {
  name: string
  pages?: number
  /** What saving makes, in place of the design's pages. */
  description?: string
  onSave: (name: string, folder: string) => void
  onClose: () => void
}) {
  const [value, setValue] = useState(name)
  const [folder, setFolder] = useState('')
  const folders = useQuery({ queryKey: mediaKeys.folders, queryFn: () => api.mediaFolders() })
  const tree = folderTree(folders.data?.data ?? [])

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          className="contents"
          onSubmit={(e) => {
            e.preventDefault()
            onSave(value.trim() || name, folder)
          }}
        >
          <DialogHeader>
            <DialogTitle>Save to Media</DialogTitle>
            <DialogDescription>
              {description ??
                (pages > 1
                  ? `Each of the ${pages} pages is saved as a picture, and the design stays editable.`
                  : 'The page is saved as a picture, and the design stays editable.')}
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="design-save-name">Name</FieldLabel>
            <Input
              id="design-save-name"
              value={value}
              maxLength={150}
              onChange={(e) => setValue(e.target.value)}
              // oxlint-disable-next-line jsx-a11y/no-autofocus -- the dialog opens to name the design
              autoFocus
            />
          </Field>
          <Field>
            <FieldLabel id="design-save-folder">Folder</FieldLabel>
            {/* The library's folders as a tree; the top of the library unless one is picked. */}
            <div
              role="radiogroup"
              aria-labelledby="design-save-folder"
              className="max-h-56 overflow-y-auto rounded-lg border p-1"
            >
              {[
                { id: '', name: 'Media library (no folder)', depth: 0 },
                ...tree.map((f) => ({ ...f, depth: f.depth + 1 })),
              ].map((f) => (
                <button
                  key={f.id || 'root'}
                  type="button"
                  role="radio"
                  aria-checked={folder === f.id}
                  onClick={() => setFolder(f.id)}
                  style={{ paddingLeft: `${0.5 + f.depth * 1.1}rem` }}
                  className="flex h-8 w-full items-center gap-2 rounded-md pr-2 text-left text-sm hover:bg-accent/60 aria-checked:bg-brand-soft aria-checked:text-brand"
                >
                  {f.id ? (
                    folder === f.id ? (
                      <FolderOpen className="size-4 shrink-0" aria-hidden />
                    ) : (
                      <Folder className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                    )
                  ) : (
                    <Images className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                  )}
                  <span className="min-w-0 flex-1 truncate">{f.name}</span>
                  {folder === f.id && <Check className="size-4 shrink-0" aria-hidden />}
                </button>
              ))}
            </div>
          </Field>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" data-testid="design-save-confirm">
              <Save /> Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
