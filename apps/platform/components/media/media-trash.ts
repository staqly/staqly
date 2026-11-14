import type { QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { findMedia, restoreInCache, trashInCache } from '@/components/media/media-cache'
import { api } from '@/lib/api'
import { toastApiError } from '@/lib/notify'

/**
 * Files moved to Deleted: a toast with Undo (restores them) for 7 seconds. Used by the drawer and the action bar.
 * One file is named in the title; label (a count, or what happened) is the line under it.
 */
export function deletedToast(qc: QueryClient, ids: string[], label?: string, name?: string) {
  // Their use counts, for when Undo brings them back.
  const known = findMedia(qc, ids)
  const one = ids.length === 1 ? (name ?? known.get(ids[0]!)?.original_name) : undefined

  trashInCache(qc, ids)
  toast.success(one ? `“${one}” moved to Deleted` : 'Moved to Deleted', {
    description: label,
    duration: 7000,
    action: {
      label: 'Undo',
      onClick: () => {
        Promise.all(ids.map((id) => api.restoreMedia(id)))
          .then((restored) => {
            restoreInCache(qc, restored, known)
            toast.success('Restored')
          })
          .catch((e) => toastApiError(e))
      },
    },
  })
}
