'use client'

import { Loader2 } from 'lucide-react'
import dynamic from 'next/dynamic'
import { Dialog } from 'radix-ui'
import { useRef, useState } from 'react'

import { SheetContent, SheetTitle } from '@/components/ui/sheet'

import type { Embedded, SavedFile } from './embed'

// The editor (Konva, its fonts) loads when the drawer first opens, never with the post.
const DesignEditor = dynamic(() => import('./editor'), {
  ssr: false,
  loading: () => (
    <div className="flex size-full items-center justify-center">
      <Loader2 className="animate-spin text-muted-foreground" aria-label="Loading the editor" />
    </div>
  ),
})

/**
 * The design editor in a drawer over the post, almost the page's width (the whole screen on phones). Its own
 * Back and Escape close it, asking first when there are unsaved changes; Save and use in the post hands the
 * pictures to `onSaved` and closes it.
 */
export function DesignDrawer({
  open,
  onOpenChange,
  edit,
  onSaved,
  onCloseAutoFocus,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The picture to edit; none for a new design. */
  edit?: string
  onSaved: (files: SavedFile[]) => void
  /** After it closed: focus goes back where it was, unless this moves it (and prevents the default). */
  onCloseAutoFocus?: (e: Event) => void
}) {
  // Another saved design opened from inside (Your designs).
  const [project, setProject] = useState('')
  const escape = useRef<(() => void) | null>(null)
  const search = project
    ? `project=${encodeURIComponent(project)}&from=composer`
    : edit
      ? `ids=${encodeURIComponent(edit)}&from=composer`
      : 'from=composer'
  const embedded: Embedded = {
    onSaved: (files) => {
      onSaved(files)
      onOpenChange(false)
    },
    onClose: () => onOpenChange(false),
    onOpen: setProject,
    onEscape: (fn) => {
      escape.current = fn
    },
  }

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(o) => {
        // Escape: the editor decides (it clears a selection first, and asks about unsaved changes).
        if (o) {
          onOpenChange(true)
        } else if (escape.current) {
          escape.current()
        } else {
          onOpenChange(false)
        }
      }}
    >
      <SheetContent
        showCloseButton={false}
        aria-describedby={undefined}
        data-testid="design-drawer"
        onCloseAutoFocus={(e) => {
          setProject('')
          onCloseAutoFocus?.(e)
        }}
        className="gap-0 overflow-hidden p-0 data-[side=right]:w-full data-[side=right]:sm:w-[calc(100vw-2rem)] data-[side=right]:sm:max-w-none"
      >
        <SheetTitle className="sr-only">Design</SheetTitle>
        <DesignEditor key={search} search={search} embedded={embedded} />
      </SheetContent>
    </Dialog.Root>
  )
}
