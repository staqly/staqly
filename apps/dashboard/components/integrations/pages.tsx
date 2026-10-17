'use client'

import { useMutation, useQuery } from '@tanstack/react-query'
import { Blocks, BookOpen, CheckCircle2, FileCode2, Plug, PlugZap, XCircle } from 'lucide-react'
import Link from 'next/link'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useState } from 'react'

import { CodeBlock, InlineCode } from '@/components/common/code-block'
import { CopyButton } from '@/components/common/copy-button'
import { DOCS_URL } from '@/components/common/docs-link'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { PageTabs } from '@/components/common/page-tabs'
import { EventTimeline } from '@/components/items/event-timeline'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api, errorMessage, type Agent, type EventQuery, type Health, type SessionInfo } from '@/lib/api'
import { usePublicApiUrl } from '@/lib/config'
import { useCollections, useMode } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn, hrefWithParams } from '@/lib/utils'

import { ConnectedAgents } from './agents'
import { KEY_PLACEHOLDER, MCP_TOOL_COUNT, useApiKeyValue } from './shared'

interface ProbeResult {
  ok: boolean
  ms: number
  health?: Health
  session?: SessionInfo
  error?: string
}

async function probe(): Promise<ProbeResult> {
  const start = performance.now()

  try {
    const [health, session] = await Promise.all([api.health(), api.session()])

    return { ok: true, ms: Math.round(performance.now() - start), health, session }
  } catch (e) {
    return { ok: false, ms: Math.round(performance.now() - start), error: errorMessage(e) }
  }
}

/** Tabs of the integrations page; `?tab=` selects one (the old per-client pages redirect here). */
const INTEGRATION_TABS = [
  { id: 'claude-code', label: 'Claude Code', docs: '/agents/claude-code' },
  { id: 'codex', label: 'Codex', docs: '/agents/codex' },
  { id: 'opencode', label: 'OpenCode', docs: '/agents/opencode' },
  { id: 'n8n', label: 'n8n', docs: '/integrations/n8n' },
  { id: 'api', label: 'REST & MCP', docs: '/api' },
] as const

function openCodeConfig(isLocal: boolean, mcpUrl: string) {
  const staqly = isLocal
    ? { type: 'remote', url: mcpUrl, enabled: true }
    : { type: 'remote', url: mcpUrl, headers: { Authorization: 'Bearer {env:STAQLY_API_KEY}' }, enabled: true }

  return JSON.stringify({ $schema: 'https://opencode.ai/config.json', mcp: { staqly } }, null, 2)
}

function GuideLink({ path }: { path: string }) {
  return (
    <Button asChild variant="outline" size="sm" className="w-fit">
      <a href={`${DOCS_URL}${path}`} target="_blank" rel="noopener noreferrer">
        <BookOpen /> Full guide
      </a>
    </Button>
  )
}

/**
 * One row: where agents connect (this computer on a local install, this instance in cloud mode; a local install
 * never shows cloud keys or addresses), and a test that checks /health and the session through the proxy.
 */
function ConnectionBar() {
  const apiUrl = usePublicApiUrl()
  const test = useMutation({ mutationFn: probe })
  const r = test.data

  return (
    <div className="grid gap-2 rounded-xl border bg-card px-4 py-3" data-testid="connection-bar">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <Plug className="size-4 text-muted-foreground" aria-hidden />
        <span className="text-sm text-muted-foreground">Agents reach this workspace at</span>
        <span className="inline-flex items-center gap-0.5">
          <InlineCode>{apiUrl}</InlineCode>
          <CopyButton value={apiUrl} label="Copy API URL" size="icon-xs" className="text-muted-foreground" />
        </span>
        {/* Result and button stay together on the right, so a result never pushes the button to a new line. */}
        <div className="ml-auto flex min-w-0 items-center gap-3">
          {r && (
            <span
              role="status"
              className={cn(
                'inline-flex min-w-0 items-center gap-1.5 text-sm',
                r.ok ? TONES.success.text : TONES.danger.text,
              )}
              title={
                r.ok ? `${r.health?.version ?? r.health?.status} · ${r.session?.workspace.name} · ${r.ms} ms` : r.error
              }
            >
              {r.ok ? <CheckCircle2 className="size-4 shrink-0" /> : <XCircle className="size-4 shrink-0" />}
              <span className="truncate">{r.ok ? `Connected · ${r.ms} ms` : `Failed: ${r.error}`}</span>
            </span>
          )}
          <Button size="sm" variant="outline" onClick={() => test.mutate()} disabled={test.isPending}>
            <PlugZap /> {test.isPending ? 'Testing...' : 'Test connection'}
          </Button>
        </div>
      </div>
    </div>
  )
}

/** Latest events of every agent, or only of the selected one. */
function RecentActivity({ agent, onShowAll }: { agent: Agent | null; onShowAll: () => void }) {
  const { isLocal } = useMode()
  const query: EventQuery = agent
    ? { agent: [agent.name], actor_id: agent.principal_id, limit: 5 }
    : // Local mode has no API keys, so show all recent activity instead.
      isLocal
      ? { limit: 5 }
      : { actor_type: 'api_key', limit: 5 }
  const activity = useQuery({
    queryKey: ['events', 'integration-recent', query],
    queryFn: () => api.events(query),
  })

  return (
    <div className="rounded-xl border bg-card p-4" data-testid="recent-activity">
      <div className="mb-4 flex items-center justify-between gap-2">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold">
            Recent activity
            {agent && <span className="font-normal text-muted-foreground"> · {agent.name}</span>}
          </h2>
          <p className="text-sm text-muted-foreground">
            {agent
              ? 'What this agent did lately.'
              : isLocal
                ? 'Latest events from agents and the dashboard.'
                : 'Events triggered with API keys.'}
          </p>
        </div>
        <div className="flex shrink-0 gap-1">
          {agent && (
            <Button size="sm" variant="ghost" onClick={onShowAll}>
              Show all
            </Button>
          )}
          <Button asChild size="sm" variant="ghost">
            <Link href={isLocal ? '/activity' : '/activity?actor_type=api_key'}>View all</Link>
          </Button>
        </div>
      </div>
      {activity.isLoading ? (
        <Skeleton className="h-40" />
      ) : activity.error ? (
        <ErrorState error={activity.error} onRetry={() => activity.refetch()} />
      ) : !activity.data?.data?.length ? (
        <EmptyState
          icon={<Plug />}
          title={agent ? `No activity from ${agent.name} yet` : 'No agent activity yet'}
          description={
            isLocal
              ? 'Once an agent connects, its actions show up here.'
              : 'Once an agent uses an API key, its actions show up here.'
          }
        />
      ) : (
        <EventTimeline events={activity.data.data} showItem />
      )}
    </div>
  )
}

type Transport = 'http' | 'stdio'

interface Conn {
  /** Needs an API key (cloud). */
  auth: boolean
  key: string
  apiUrl: string
  mcpUrl: string
  transport: Transport
}

const STDIO = ['docker', 'exec', '-i', 'staqly', 'mcp-stdio']

function ClaudeCodeTab({ c }: { c: Conn }) {
  return (
    <CodeBlock
      language="bash"
      code={
        c.transport === 'stdio'
          ? `claude mcp add staqly -- ${STDIO.join(' ')}`
          : `claude mcp add --transport http staqly ${c.mcpUrl}${c.auth ? ` --header "Authorization: Bearer ${c.key}"` : ''}`
      }
    />
  )
}

function CodexTab({ c }: { c: Conn }) {
  return (
    <CodeBlock
      language="bash"
      code={
        c.transport === 'stdio'
          ? `codex mcp add staqly -- ${STDIO.join(' ')}`
          : c.auth
            ? `export STAQLY_API_KEY=${c.key}\ncodex mcp add staqly --url ${c.mcpUrl} --bearer-token-env-var STAQLY_API_KEY`
            : `codex mcp add staqly --url ${c.mcpUrl}`
      }
    />
  )
}

function OpenCodeTab({ c }: { c: Conn }) {
  const stdio = JSON.stringify(
    { $schema: 'https://opencode.ai/config.json', mcp: { staqly: { type: 'local', command: STDIO, enabled: true } } },
    null,
    2,
  )

  return (
    <>
      {c.auth && c.transport === 'http' && <CodeBlock language="bash" code={`export STAQLY_API_KEY=${c.key}`} />}
      <CodeBlock
        title="opencode.json"
        language="json"
        code={c.transport === 'stdio' ? stdio : openCodeConfig(!c.auth, c.mcpUrl)}
      />
    </>
  )
}

function N8nTab({ c }: { c: Conn }) {
  return (
    <>
      <p className="text-sm text-muted-foreground">
        Point HTTP Request nodes at <InlineCode>{c.apiUrl}</InlineCode>
        {c.auth ? (
          ' with a Header Auth credential:'
        ) : (
          <>
            {' '}
            (<InlineCode>http://api:3333</InlineCode> from the staqly Docker network), no authentication needed.
          </>
        )}{' '}
        Receive decisions by adding the n8n Webhook URL on the{' '}
        <Link href="/webhooks" className="text-brand hover:underline">
          Webhooks
        </Link>{' '}
        page.
      </p>
      {c.auth && <CodeBlock title="Header Auth credential" code={`Name:  Authorization\nValue: Bearer ${c.key}`} />}
    </>
  )
}

function ApiTab({ c }: { c: Conn }) {
  // By ID: a name can change (or need URL-escaping), the ID never does.
  const collections = useCollections()
  const col = collections.data?.find((x) => x.is_default) ?? collections.data?.[0]
  const colId = col?.id ?? 'col_...'
  const auth = c.auth ? ` \\\n  -H "Authorization: Bearer ${c.key}"` : ''

  return (
    <>
      <CodeBlock
        title="Create an item"
        language="bash"
        code={`curl -X POST ${c.apiUrl}/v1/collections/${colId}/items${auth} \\\n  -H "Content-Type: application/json" \\\n  -d '{"type":"text","title":"Hello","content":"Review me"}'`}
      />
      <CodeBlock title={`MCP endpoint (${MCP_TOOL_COUNT} tools, Streamable HTTP)`} code={c.mcpUrl} />
    </>
  )
}

const TAB_BODY: Record<(typeof INTEGRATION_TABS)[number]['id'], (p: { c: Conn }) => React.ReactNode> = {
  'claude-code': ClaudeCodeTab,
  codex: CodexTab,
  opencode: OpenCodeTab,
  n8n: N8nTab,
  api: ApiTab,
}
const MCP_CLIENTS = new Set(['claude-code', 'codex', 'opencode'])

/** A small switch in the same style as the client tabs, so every choice on the page looks alike. */
function Segmented<T extends string>({
  label,
  value,
  onChange,
  options,
}: {
  label: string
  value: T
  onChange: (v: T) => void
  options: { value: T; label: string }[]
}) {
  return (
    <Tabs value={value} onValueChange={(v) => onChange(v as T)}>
      <TabsList aria-label={label}>
        {options.map((o) => (
          <TabsTrigger key={o.value} value={o.value}>
            {o.label}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}

/** One line under the snippets: whether the commands need a key, and where to get one. */
function KeyHint({ cloud }: { cloud: boolean }) {
  const { remembered } = useApiKeyValue()

  // A local install needs no key; in cloud mode one comes from API keys.
  if (!cloud) {
    return <span>No API key needed: requests from this machine are trusted.</span>
  }

  if (remembered) {
    return (
      <span>
        Uses the key <strong className="text-foreground">{remembered.name}</strong> you just created (kept in this tab
        only).
      </span>
    )
  }

  return (
    <span>
      Replace <InlineCode>{KEY_PLACEHOLDER}</InlineCode> with an API key from{' '}
      <Link href="/integrations/api-keys?new=1" className="text-brand hover:underline">
        API keys
      </Link>
      .
    </span>
  )
}

export function IntegrationsPage() {
  const { isLocal } = useMode()
  const apiUrl = usePublicApiUrl()
  const mcpUrl = `${apiUrl}/mcp`
  const { key } = useApiKeyValue()
  const router = useRouter()
  const pathname = usePathname()
  const params = useSearchParams()
  const requested = params.get('tab')
  const tab = INTEGRATION_TABS.find((t) => t.id === requested)?.id ?? 'claude-code'
  // Two sections: how to connect (Setup) and who is connected and what they did (Agents & activity).
  const view = params.get('view') === 'agents' ? 'agents' : 'setup'
  const [transport, setTransport] = useState<Transport>('http')
  const [agent, setAgent] = useState<Agent | null>(null)
  // Cloud mode: this instance, over HTTPS with a key. A local install connects only to itself, keyless.
  const cloud = !isLocal
  // STDIO runs mcp-stdio in the container, so Docker only; HTTP is the same however staqly is started.
  const stdioAvailable = !cloud && MCP_CLIENTS.has(tab)
  const c: Conn = {
    auth: cloud,
    key,
    apiUrl,
    mcpUrl,
    transport: stdioAvailable ? transport : 'http',
  }

  return (
    <PageContainer>
      <PageHeader
        title="Integrations"
        icon={<Blocks className="size-4" />}
        description="Connect an agent, see which agents talk to this workspace and what they did. Step-by-step guides live at docs.staqly.ai."
      />
      <PageTabs
        label="Integrations sections"
        className="mt-4"
        tabs={[
          { href: hrefWithParams(pathname, params, { view: undefined }), label: 'Setup', active: view === 'setup' },
          {
            href: hrefWithParams(pathname, params, { view: 'agents' }),
            label: 'Agents & activity',
            active: view === 'agents',
            testId: 'agents-tab',
          },
        ]}
      />
      {view === 'agents' ? (
        <div className="mt-6 grid grid-cols-1 gap-4">
          <ConnectedAgents selectedId={agent?.id ?? null} onSelect={setAgent} />
          <RecentActivity agent={agent} onShowAll={() => setAgent(null)} />
        </div>
      ) : (
        <div className="mt-6 grid grid-cols-1 gap-4">
          <ConnectionBar />

          <Tabs
            value={tab}
            onValueChange={(v) => router.replace(`${pathname}?tab=${v}`, { scroll: false })}
            className="gap-0 rounded-xl border bg-card"
          >
            <div className="flex flex-wrap items-center gap-2 border-b p-3" data-testid="connection-options">
              <TabsList
                className="max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto pointer-coarse:group-data-horizontal/tabs:h-auto"
                aria-label="Client"
              >
                {INTEGRATION_TABS.map((t) => (
                  <TabsTrigger key={t.id} value={t.id} className="pointer-coarse:min-h-10">
                    {t.label}
                  </TabsTrigger>
                ))}
              </TabsList>
              <div className="ml-auto flex flex-wrap items-center gap-2">
                {stdioAvailable && (
                  <Segmented
                    label="MCP transport"
                    value={transport}
                    onChange={setTransport}
                    options={[
                      { value: 'http', label: 'HTTP' },
                      { value: 'stdio', label: 'STDIO (Docker)' },
                    ]}
                  />
                )}
              </div>
            </div>
            {INTEGRATION_TABS.map(({ id, docs }) => {
              const Body = TAB_BODY[id]

              return (
                <TabsContent key={id} value={id} className="grid grid-cols-1 gap-3 p-3">
                  <Body c={c} />
                  <div className="flex flex-wrap items-center gap-2 pt-1 text-xs text-muted-foreground">
                    <KeyHint cloud={cloud} />
                    <div className="ml-auto flex gap-2">
                      {id === 'api' && (
                        <Button asChild variant="outline" size="sm">
                          <a href="/api/openapi.yaml" target="_blank" rel="noopener noreferrer">
                            <FileCode2 /> openapi.yaml
                          </a>
                        </Button>
                      )}
                      <GuideLink path={docs} />
                    </div>
                  </div>
                </TabsContent>
              )
            })}
          </Tabs>
        </div>
      )}
    </PageContainer>
  )
}

export function IntegrationsRedirect({ tab, view }: { tab?: string; view?: 'agents' }) {
  const router = useRouter()

  useEffect(() => {
    router.replace(view ? `/integrations?view=${view}` : `/integrations?tab=${tab}`)
  }, [router, tab, view])

  return null
}
