'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { api, type Post } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { postTitle } from '@/lib/posts'
import { invalidateAfterPostChange, qk, syncPostInCache } from '@/lib/queries'
import { formatSchedule } from '@/lib/schedule'

/**
 * Set, move or clear a post's time. The change shows at once (the calendar chip moves), the server's answer
 * replaces it; on an error the post goes back where it was.
 */
export function useSetSchedule(opts?: { onDone?: (post: Post) => void }) {
  const qc = useQueryClient()

  return useMutation({
    mutationFn: ({ post, publishAt }: { post: Post; publishAt: string | null }) =>
      publishAt ? api.schedulePost(post.id, publishAt) : api.cancelPost(post.id),
    onMutate: ({ post, publishAt }) => {
      syncPostInCache(qc, { ...post, publish_at: publishAt, status: publishAt ? 'scheduled' : 'draft' })
    },
    onSuccess: (updated, { publishAt }) => {
      syncPostInCache(qc, updated)
      invalidateAfterPostChange(qc, updated.id)
      toast.success(
        publishAt
          ? `“${postTitle(updated, 40)}” goes out ${formatSchedule(publishAt)}`
          : `“${postTitle(updated, 40)}” is a draft again`,
      )
      opts?.onDone?.(updated)
    },
    onError: (err, { post }) => {
      syncPostInCache(qc, post)
      void qc.invalidateQueries({ queryKey: qk.post(post.id) })
      toastApiError(err)
    },
  })
}
