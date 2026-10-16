'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArchiveRestore, Download, Trash2, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Checkbox } from '@/components/ui/checkbox'
import { DropdownMenuItem, DropdownMenuSeparator } from '@/components/ui/dropdown-menu'
import { api, mediaUrl, type Item } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, qk, removeItemFromLists, syncItemInCache } from '@/lib/queries'

/**
 * Item-level options (download, delete, with or without erasing the history) and their confirmations, the same in the inbox, the side panel
 * and the item page: `menu` goes into the item's More menu, `dialogs` anywhere on the page.
 */
export function useItemOptions(item: Item | undefined, onGone: () => void) {
  const qc = useQueryClient()
  const [confirmDelete, setConfirmDelete] = useState(false)
  // One Delete; ticking this erases the history too (purge: for personal data).
  const [eraseHistory, setEraseHistory] = useState(false)

  const del = useMutation({
    mutationFn: () => api.deleteItem(item!.id),
    onSuccess: () => {
      removeItemFromLists(qc, item!.id)
      qc.removeQueries({ queryKey: qk.item(item!.id) })
      invalidateAfterItemChange(qc, undefined, { usage: true })
      namedToast(item?.title, 'was deleted')
      onGone()
    },
    onError: (e) => toastApiError(e),
  })

  const purge = useMutation({
    mutationFn: () => api.purgeItem(item!.id),
    onSuccess: (r) => {
      setConfirmDelete(false)
      removeItemFromLists(qc, item!.id)
      qc.removeQueries({ queryKey: ['item', item!.id] })
      invalidateAfterItemChange(qc, undefined, { usage: true })
      toast.success(`“${item?.title || 'Untitled item'}” was deleted and its history erased`, {
        description: `${pluralize(r.events_deleted, 'event')} erased${r.media_deleted ? ', file deleted' : ''}.`,
      })
      onGone()
    },
    onError: (e) => toastApiError(e, 'Could not delete the item'),
  })

  const unarchive = useMutation({
    mutationFn: () => api.unarchiveItem(item!.id),
    onSuccess: (updated) => {
      syncItemInCache(qc, updated)
      invalidateAfterItemChange(qc, updated.id)
      namedToast(updated.title, 'is back from the archive')
    },
    onError: (e) => toastApiError(e),
  })

  if (!item) {
    return { menu: null, dialogs: null }
  }

  const menu = (
    <>
      {item.media && (
        <DropdownMenuItem asChild>
          <a href={mediaUrl(item.media.id, { download: true })} download={item.media.original_name}>
            <Download /> Download file
          </a>
        </DropdownMenuItem>
      )}
      {item.archived_at && (
        <DropdownMenuItem onSelect={() => unarchive.mutate()} disabled={unarchive.isPending}>
          <ArchiveRestore /> Unarchive
        </DropdownMenuItem>
      )}
      {/* Between the options above and the destructive one only; the menus that host this add their own. */}
      {(item.media || item.archived_at) && <DropdownMenuSeparator />}
      <DropdownMenuItem
        variant="destructive"
        onSelect={() => {
          setEraseHistory(false)
          setConfirmDelete(true)
        }}
      >
        <Trash2 /> Delete item
      </DropdownMenuItem>
    </>
  )

  // Mounted only while open: the inbox builds these for every item it shows (j/k), so closed ones cost nothing.
  const dialogs = (
    <>
      {confirmDelete && (
        <AlertDialog
          open={confirmDelete}
          onOpenChange={(o) => !del.isPending && !purge.isPending && setConfirmDelete(o)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete “{item.title || 'Untitled'}”?</AlertDialogTitle>
              <AlertDialogDescription>
                It is removed from its collection. Its history (who did what, and when) is kept for the activity log.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <label className="flex items-start gap-2.5 rounded-lg border p-3 text-sm">
              <Checkbox
                checked={eraseHistory}
                onCheckedChange={(v) => setEraseHistory(!!v)}
                className="mt-0.5"
                aria-label="Also erase its history"
              />
              <span className="grid gap-0.5">
                <span className="font-medium">Also erase its history</span>
                <span className="text-xs text-muted-foreground">
                  For personal data: every event about it, pending webhook deliveries and its file are erased too.
                  Systems that already received the data are not affected.
                </span>
              </span>
            </label>
            {eraseHistory && (
              <Callout tone="danger" icon={TriangleAlert} size="xs">
                This cannot be undone.
              </Callout>
            )}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={del.isPending || purge.isPending}>Cancel</AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                onClick={(e) => {
                  e.preventDefault()

                  if (eraseHistory) {
                    purge.mutate()
                  } else {
                    del.mutate(undefined, { onSettled: () => setConfirmDelete(false) })
                  }
                }}
                disabled={del.isPending || purge.isPending}
              >
                {del.isPending || purge.isPending
                  ? 'Deleting...'
                  : eraseHistory
                    ? 'Delete and erase history'
                    : 'Delete item'}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  )

  return { menu, dialogs }
}
