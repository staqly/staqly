'use client'

import { useQuery } from '@tanstack/react-query'
import { Save } from 'lucide-react'
import { useState } from 'react'

import { NativeSelect } from '@/components/common/native-select'
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
  pages,
  onSave,
  onClose,
}: {
  name: string
  pages: number
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
            onSave(value.trim() || 'Untitled design', folder)
          }}
        >
          <DialogHeader>
            <DialogTitle>Save to Media</DialogTitle>
            <DialogDescription>
              {pages > 1
                ? `Each of the ${pages} pages is saved as a picture, and the design stays editable.`
                : 'The page is saved as a picture, and the design stays editable.'}
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
            <FieldLabel htmlFor="design-save-folder">Folder</FieldLabel>
            <NativeSelect id="design-save-folder" value={folder} onChange={(e) => setFolder(e.target.value)}>
              <option value="">No folder (the top of the library)</option>
              {tree.map((f) => (
                <option key={f.id} value={f.id}>
                  {`${'  '.repeat(f.depth)}${f.name}`}
                </option>
              ))}
            </NativeSelect>
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
