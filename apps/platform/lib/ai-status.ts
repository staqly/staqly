import { useQuery } from '@tanstack/react-query'

import { api, type AIJob } from './api'
import { qk } from './queries'

// Which AI tools the server offers and one job's state while it runs; small, so pages that only show or hide an AI
// button load nothing else (the words for the tools are in lib/ai.ts).

export function isLive(job: AIJob | undefined) {
  return !!job && (job.status === 'queued' || job.status === 'running')
}

export function useAiStatus() {
  return useQuery({ queryKey: qk.ai, queryFn: api.ai, staleTime: 5 * 60_000 })
}

/** One job, polled while it runs (one small request every 1.5 s; nothing once it ended). */
export function useAiJob(id: string | null) {
  return useQuery({
    queryKey: qk.aiJob(id ?? ''),
    queryFn: () => api.aiJob(id!),
    enabled: !!id,
    refetchInterval: (q) => (isLive(q.state.data) || !q.state.data ? 1500 : false),
  })
}
