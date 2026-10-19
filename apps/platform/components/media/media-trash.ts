import type { QueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { api } from '@/lib/api'
import { toastApiError } from '@/lib/notify'

/**
 * Files moved to Deleted: a toast with Undo (restores them) for 7 seconds. Used by the drawer and the action bar.
 */
export function deletedToast(qc: QueryClient, ids: string[], label: string) {
  void qc.invalidateQueries({ queryKey: ['media'] })
  toast.success('Moved to Deleted', {
    description: label,
    duration: 7000,
    action: {
      label: 'Undo',
      onClick: () => {
        Promise.all(ids.map((id) => api.restoreMedia(id)))
          .then(() => {
            void qc.invalidateQueries({ queryKey: ['media'] })
            toast.success('Restored')
          })
          .catch((e) => toastApiError(e))
      },
    },
  })
}
