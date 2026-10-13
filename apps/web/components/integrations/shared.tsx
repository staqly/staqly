'use client'

import { Cloud, KeyRound, Laptop } from 'lucide-react'
import Link from 'next/link'

import { CodeBlock, InlineCode } from '@/components/common/code-block'
import { Button } from '@/components/ui/button'
import { usePublicApiUrl } from '@/lib/config'
import { useMode } from '@/lib/queries'
import { useRememberedApiKey } from '@/lib/session-key'

export const KEY_PLACEHOLDER = '<YOUR_API_KEY>'

/** The key created in this tab (if any), else a placeholder. Never the recovery key. */
export function useApiKeyValue() {
  const remembered = useRememberedApiKey()
  const { isLocal } = useMode()

  return { key: remembered?.key ?? KEY_PLACEHOLDER, remembered, isLocal }
}

export function KeySourceNote() {
  const { remembered, isLocal } = useApiKeyValue()
  const { apiUrl } = useApiUrls()

  if (isLocal) {
    return (
      <div className="flex items-start gap-2 rounded-lg border border-sky-500/30 bg-sky-500/8 px-3 py-2 text-sm">
        <Laptop className="mt-0.5 size-4 shrink-0 text-sky-600" />
        <span>
          <strong>Local mode:</strong> no API key needed. Requests from this machine to{' '}
          <code className="font-mono text-xs">{apiUrl}</code> are trusted, so the commands below have no credentials.
        </span>
      </div>
    )
  }

  if (remembered) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-emerald-500/30 bg-emerald-500/8 px-3 py-2 text-sm">
        <KeyRound className="size-4 text-emerald-600" />
        <span>
          Snippets include the key <strong>{remembered.name}</strong> you just created. It is kept only in this
          tab&apos;s memory.
        </span>
      </div>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-sm">
      <KeyRound className="size-4 text-muted-foreground" />
      <span className="flex-1">
        Replace <code className="font-mono text-xs">{KEY_PLACEHOLDER}</code> with an API key. Keys are only shown when
        created.
      </span>
      <Button asChild size="sm" variant="outline">
        <Link href="/integrations/api-keys?new=1">Create API key</Link>
      </Button>
    </div>
  )
}

export function DocSection({
  title,
  description,
  children,
  step,
}: {
  title: string
  description?: React.ReactNode
  children?: React.ReactNode
  step?: number
}) {
  return (
    <section className="grid gap-3">
      <div className="flex items-start gap-3">
        {step !== undefined && (
          <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-brand-soft text-xs font-semibold text-brand">
            {step}
          </span>
        )}
        <div>
          <h2 className="text-base font-semibold tracking-tight">{title}</h2>
          {description && <div className="mt-0.5 text-sm text-muted-foreground">{description}</div>}
        </div>
      </div>
      {children && <div className={step !== undefined ? 'grid gap-3 sm:pl-9' : 'grid gap-3'}>{children}</div>}
    </section>
  )
}

/** Where agents reach the API and its MCP endpoint (the API's PUBLIC_API_URL). */
export function useApiUrls() {
  const apiUrl = usePublicApiUrl()

  return { apiUrl, mcpUrl: `${apiUrl}/mcp` }
}

export const MCP_TOOLS: { name: string; description: string }[] = [
  { name: 'list_collections', description: 'List collections with item counts per status' },
  { name: 'create_collection', description: 'Create a collection (gets default actions)' },
  { name: 'get_collection', description: 'Get a collection by ID or name, with its actions' },
  { name: 'list_items', description: 'List items with status/type filters' },
  { name: 'get_item', description: 'Get one item with metadata and media URL' },
  { name: 'create_item', description: 'Add text, links, JSON, base64 media or media URLs' },
  { name: 'update_item', description: 'Update fields or status (processing, completed, failed…)' },
  { name: 'search_items', description: 'Search title, content and metadata with filters' },
  { name: 'list_pending_approvals', description: 'Pending items across all collections' },
  { name: 'approve_item', description: 'Approve, optionally with edits (items:review)' },
  { name: 'reject_item', description: 'Reject with feedback (items:review)' },
  { name: 'list_available_actions', description: 'Actions allowed for an item now' },
  { name: 'execute_custom_action', description: 'Run a collection action, e.g. regenerate' },
  { name: 'get_item_history', description: 'Events, feedback and prior versions of an item' },
  { name: 'list_rules', description: 'Approval rules in evaluation order (rules:manage)' },
  { name: 'create_rule', description: 'Auto-approve, reject, set status, expire or escalate new items (rules:manage)' },
  { name: 'list_watcher_presets', description: 'Ready-made sources: HN, Reddit, Pinterest, YouTube, GitHub, RSS…' },
  {
    name: 'create_watcher_from_preset',
    description: 'Watch a source; new entries become pending items (watchers:manage)',
  },
  { name: 'list_watchers', description: 'Watchers with status, last run and errors (watchers:manage)' },
]

/** Explains how to run the same app remotely with scoped API keys. Shown in local mode. */
export function CloudModeCard({ className }: { className?: string }) {
  return (
    <section className={'grid gap-3 rounded-xl border bg-card p-4 ' + (className ?? '')} data-testid="cloud-mode-card">
      <div className="flex items-start gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-brand-soft text-brand">
          <Cloud className="size-4" />
        </span>
        <div>
          <h2 className="text-sm font-semibold">Cloud mode</h2>
          <p className="text-sm text-muted-foreground">
            Want agents on other machines to reach staqly? Deploy the same app remotely with{' '}
            <InlineCode>STAQLY_MODE=cloud</InlineCode> plus a <InlineCode>SETUP_TOKEN</InlineCode>, HTTPS and{' '}
            <InlineCode>COOKIE_SECURE=true</InlineCode>. There you sign in with a recovery key and create scoped API
            keys; clients switch by using the cloud URL and <InlineCode>Authorization: Bearer stk_…</InlineCode>.
          </p>
        </div>
      </div>
      <CodeBlock
        language="bash"
        title="Docker / environment"
        code={`STAQLY_MODE=cloud\nSETUP_TOKEN=<long random string>\nCOOKIE_SECURE=true\nPUBLIC_API_URL=https://staqly.example.com`}
      />
      <CodeBlock
        language="bash"
        title="Claude Code against a cloud instance"
        code={`claude mcp add --transport http staqly https://staqly.example.com/mcp --header "Authorization: Bearer <YOUR_API_KEY>"`}
      />
      <CodeBlock
        language="bash"
        title="Codex against a cloud instance"
        code={`export STAQLY_API_KEY=<YOUR_API_KEY>\ncodex mcp add staqly --url https://staqly.example.com/mcp --bearer-token-env-var STAQLY_API_KEY`}
      />
      <CodeBlock
        language="json"
        title="OpenCode against a cloud instance (opencode.json)"
        code={`"staqly": {\n  "type": "remote",\n  "url": "https://staqly.example.com/mcp",\n  "headers": { "Authorization": "Bearer {env:STAQLY_API_KEY}" },\n  "enabled": true\n}`}
      />
    </section>
  )
}
