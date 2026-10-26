'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bot, Plug, X } from 'lucide-react'

import { ErrorState } from '@/components/common/error-state'
import { LIVE_INTERVAL_MS } from '@/components/shell/live-updates'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Agent } from '@/lib/api'
import { formatDateTime, parseDate, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

const ACTIVE_MS = 2 * 60 * 1000
const ACTIVITY_TONE: Record<ReturnType<typeof agentActivity>, Tone> = {
  active: 'success',
  recent: 'warning',
  idle: 'neutral',
}

/** Active (talked in the last 2 minutes), Recent (today), or Idle. */
export function agentActivity(a: Pick<Agent, 'last_seen_at'>, now = Date.now()) {
  const ago = now - (parseDate(a.last_seen_at)?.getTime() ?? 0)

  return ago < ACTIVE_MS ? 'active' : ago < 24 * 3600 * 1000 ? 'recent' : 'idle'
}

function useAgents() {
  return useQuery({
    queryKey: ['agents'],
    queryFn: async () => (await api.agents()).data ?? [],
    refetchInterval: LIVE_INTERVAL_MS * 5,
  })
}

function AgentRow({
  agent: a,
  selected,
  onSelect,
  onForget,
  forgetting,
}: {
  agent: Agent
  selected: boolean
  onSelect: (a: Agent | null) => void
  onForget: (a: Agent) => void
  forgetting: boolean
}) {
  const activity = agentActivity(a)

  return (
    <div
      className={cn(
        'flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border p-3 transition-colors',
        selected ? 'border-brand/60 bg-brand-soft/60 ring-1 ring-brand/30' : 'bg-background',
      )}
    >
      <button
        type="button"
        aria-pressed={selected}
        onClick={() => onSelect(selected ? null : a)}
        className="flex min-h-11 min-w-0 flex-1 basis-full items-center gap-3 text-left sm:basis-0"
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/40">
          {a.kind === 'mcp' ? <Plug className="size-4" /> : <Bot className="size-4" />}
        </span>
        <span className="min-w-0">
          <span className="flex items-center gap-2">
            <span className="truncate font-medium">{a.name}</span>
            <span className="rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground uppercase">
              {a.kind === 'mcp' ? 'MCP' : 'API'}
            </span>
            {a.version && <span className="truncate text-xs text-muted-foreground">{a.version}</span>}
          </span>
          <span className="block text-xs text-muted-foreground">
            {a.principal_id === 'local' ? 'Access token (this computer)' : `Key: ${a.principal_name || a.principal_id}`}{' '}
            · first seen {timeAgo(a.first_seen_at)}
          </span>
        </span>
      </button>
      <div className="flex items-center gap-1.5 text-xs max-sm:mr-auto" title={formatDateTime(a.last_seen_at)}>
        <span aria-hidden className={cn('size-2 rounded-full', TONES[ACTIVITY_TONE[activity]].dot)} />
        {activity === 'active' ? 'Active now' : `Last seen ${timeAgo(a.last_seen_at)}`}
      </div>
      <div className="w-24 text-right text-xs text-muted-foreground tabular-nums max-sm:w-auto">
        <span className="font-medium text-foreground">{a.posts_created}</span> items added
      </div>
      <div className="w-24 text-right text-xs text-muted-foreground tabular-nums max-sm:w-auto">
        {a.requests} request{a.requests === 1 ? '' : 's'}
      </div>
      <Button
        size="icon-sm"
        className="max-sm:ml-auto"
        variant="ghost"
        aria-label={`Remove ${a.name} from the list`}
        title={
          a.principal_id === 'local'
            ? 'Removes it from this list only: it keeps working and comes back on its next call. To cut it off, stop it or remove the staqly server from its MCP settings.'
            : 'Removes it from this list only: it keeps working and comes back on its next call. To cut it off, revoke its API key in Settings.'
        }
        disabled={forgetting}
        onClick={() => onForget(a)}
      >
        <X />
      </Button>
    </div>
  )
}

/**
 * Everything that has talked to this workspace, recorded automatically, in two groups: agents (MCP clients by
 * their own name: Claude Code, Codex, Cursor...) and API connections (REST clients by their User-Agent: n8n, SDK
 * scripts, curl). People in the browser are neither. Selecting one narrows Recent activity to it.
 */
export function ConnectedAgents({
  selectedId,
  onSelect,
}: {
  selectedId: string | null
  onSelect: (a: Agent | null) => void
}) {
  const qc = useQueryClient()
  const agents = useAgents()
  const forget = useMutation({
    mutationFn: (id: string) => api.forgetAgent(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['agents'] }),
    onError: (e) => toastApiError(e),
  })
  const list = agents.data ?? []
  const onForget = (a: Agent) => {
    if (a.id === selectedId) {
      onSelect(null)
    }

    forget.mutate(a.id)
  }
  const group = (kind: Agent['kind'], title: string, description: string, empty: string) => {
    const rows = list.filter((a) => a.kind === kind)

    return (
      <section className="grid grid-cols-1 gap-2" aria-label={title}>
        <div>
          <h2 className="text-sm font-semibold">{title}</h2>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
        {agents.isLoading ? (
          <Skeleton className="h-16 rounded-xl bg-muted/40" />
        ) : agents.error && !agents.data ? (
          // Once, in the first section: the second would only repeat it.
          kind === 'mcp' && (
            <ErrorState
              title="Could not load the agents"
              error={agents.error}
              onRetry={() => void agents.refetch()}
              className="py-6"
            />
          )
        ) : rows.length === 0 ? (
          <div className="rounded-xl border border-dashed p-4 text-center text-sm text-muted-foreground">{empty}</div>
        ) : (
          rows.map((a) => (
            <AgentRow
              key={a.id}
              agent={a}
              selected={a.id === selectedId}
              onSelect={onSelect}
              onForget={onForget}
              forgetting={forget.isPending && forget.variables === a.id}
            />
          ))
        )}
      </section>
    )
  }

  return (
    <div id="agents" className="grid grid-cols-1 gap-5 rounded-xl border bg-card p-4" data-testid="agents">
      {group(
        'mcp',
        'Agents',
        'AI agents connected over MCP, by the name they report. Select one to see only its activity. Removed after 30 days without activity.',
        'No agent has connected yet. Follow a guide above; it shows up here after its first tool call.',
      )}
      {group(
        'api',
        'API connections',
        'Scripts and tools calling the REST API (n8n, SDKs, curl), by their User-Agent. Removed after 30 days without activity.',
        'Nothing has called the REST API yet.',
      )}
    </div>
  )
}
