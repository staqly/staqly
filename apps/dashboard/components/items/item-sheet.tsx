'use client'

import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'

import { ItemDetail } from './item-detail'

/** The full item (preview, details, edit, history, versions) in a side panel; the inbox and collections share it. */
export function ItemSheet({ id, onClose }: { id: string | null; onClose: () => void }) {
  return (
    <Sheet open={!!id} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="overflow-y-auto overscroll-contain p-4 pt-[max(1rem,env(safe-area-inset-top))] pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:p-5 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl data-[side=right]:lg:max-w-3xl">
        <SheetHeader className="sr-only">
          <SheetTitle>Item details</SheetTitle>
          <SheetDescription>Preview, edit and review this item</SheetDescription>
        </SheetHeader>
        {id && <ItemDetail id={id} variant="sheet" onDeleted={onClose} />}
      </SheetContent>
    </Sheet>
  )
}
