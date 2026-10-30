import { QueryClient, QueryClientProvider, useInfiniteQuery } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { api, type Post, type PostPage } from '@/lib/api'
import { flattenPosts, qk, useRefreshDuePosts } from '@/lib/queries'

const post = (id: string, status: string, publish_at: string) => ({ id, status, publish_at }) as unknown as Post

describe('useRefreshDuePosts', () => {
  it('shows a due post as publishing once the queue picked it up, and leaves future ones alone', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const due = post('pst_due', 'scheduled', new Date(Date.now() - 1000).toISOString())
    const later = post('pst_later', 'scheduled', new Date(Date.now() + 3_600_000).toISOString())
    const get = vi.spyOn(api, 'post').mockResolvedValue({ ...due, status: 'publishing' } as Post)
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    )
    const { result } = renderHook(
      () => {
        const list = useInfiniteQuery({
          queryKey: qk.posts({}),
          queryFn: async (): Promise<PostPage> => ({ data: [due, later] }) as PostPage,
          initialPageParam: '',
          getNextPageParam: () => undefined,
        })
        const posts = flattenPosts(list.data)

        useRefreshDuePosts(posts)

        return posts
      },
      { wrapper },
    )

    await waitFor(() => expect(result.current.map((p) => p.status)).toEqual(['publishing', 'scheduled']))
    expect(get.mock.calls.map((c) => c[0])).toEqual(['pst_due'])
    get.mockRestore()
  })
})
