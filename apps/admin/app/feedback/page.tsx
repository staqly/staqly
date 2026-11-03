'use client'

import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'

import { Shell } from '@/components/shell'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { adminApi, errorText } from '@/lib/api'
import { formatDate } from '@/lib/format'

type Feedback = {
  id: string
  kind: string
  description: string
  reporter_id: string
  reporter_name: string
  reporter_email: string
  workspace_id: string
  workspace_name: string
  created_at: string
  page_path: string
  attachment: boolean
  status: string
}
type Page = { items: Feedback[]; next_cursor: string }

function FeedbackCard({ row }: { row: Feedback }) {
  const client = useQueryClient()
  const update = useMutation({
    mutationFn: (status: string) => adminApi.send('PATCH', `/v1/admin/feedback/${row.id}`, { status }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['admin-feedback'] }),
  })

  return (
    <Card className="min-w-0 gap-3 p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">{row.kind === 'problem' ? 'Problem' : 'Feature request'}</h2>
        <Select value={row.status} onValueChange={(status) => update.mutate(status)} disabled={update.isPending}>
          <SelectTrigger aria-label={`Status of ${row.id}`} className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="new">New</SelectItem>
            <SelectItem value="in_progress">In progress</SelectItem>
            <SelectItem value="closed">Closed</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <p className="whitespace-pre-wrap wrap-anywhere">{row.description}</p>
      <dl className="grid gap-2 text-sm text-muted-foreground sm:grid-cols-2 [&_dd]:wrap-anywhere">
        <div>
          <dt className="font-medium">Reporter</dt>
          <dd>
            {row.reporter_name} {row.reporter_email}
            <span className="block font-mono text-xs">{row.reporter_id}</span>
          </dd>
        </div>
        <div>
          <dt className="font-medium">Workspace</dt>
          <dd>
            {row.workspace_name}
            <span className="block font-mono text-xs">{row.workspace_id}</span>
          </dd>
        </div>
        <div>
          <dt className="font-medium">Date</dt>
          <dd>{formatDate(row.created_at)}</dd>
        </div>
        <div>
          <dt className="font-medium">Page</dt>
          <dd>{row.page_path}</dd>
        </div>
      </dl>
      {row.attachment && (
        <a className="w-fit py-2 text-sm underline" href={`/api/v1/admin/feedback/${row.id}/attachment`}>
          Download screenshot
        </a>
      )}
      {update.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorText(update.error)}
        </p>
      )}
    </Card>
  )
}

export default function FeedbackPage() {
  const list = useInfiniteQuery({
    queryKey: ['admin-feedback'],
    initialPageParam: '',
    queryFn: ({ pageParam }) => adminApi.get<Page>(`/v1/admin/feedback?${new URLSearchParams({ cursor: pageParam })}`),
    getNextPageParam: (page) => page.next_cursor || undefined,
  })
  const rows = list.data?.pages.flatMap((page) => page.items) ?? []

  return (
    <Shell title="Feedback">
      {list.isPending && <Skeleton className="h-48 w-full" />}
      {list.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorText(list.error)}
        </p>
      )}
      {!list.isPending && !list.isError && rows.length === 0 && (
        <p className="text-sm text-muted-foreground">No feedback yet.</p>
      )}
      <div className="grid gap-4">
        {rows.map((row) => (
          <FeedbackCard key={row.id} row={row} />
        ))}
      </div>
      {list.hasNextPage && (
        <Button variant="outline" disabled={list.isFetchingNextPage} onClick={() => list.fetchNextPage()}>
          Load more
        </Button>
      )}
    </Shell>
  )
}
