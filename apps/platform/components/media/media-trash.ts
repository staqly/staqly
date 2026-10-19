import type { QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { findMedia, restoreInCache, trashInCache } from '@/components/media/media-cache'
import { api } from '@/lib/api'
import { toastApiError } from '@/lib/notify'

/**
 * Files moved to Deleted: a toast with Undo (restores them) for 7 seconds. Used by the drawer and the action bar.
 */
export function deletedToast(qc: QueryClient, ids: string[], label: string) {
  // Their use counts, for when Undo brings them back.
  const known = findMedia(qc, ids)

  trashInCache(qc, ids)
  toast.success('Moved to Deleted', {
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
