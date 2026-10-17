'use client'

import { Cloud } from 'lucide-react'

import { CodeBlock, InlineCode } from '@/components/common/code-block'
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

/** Where agents reach the API and its MCP endpoint (the API's PUBLIC_API_URL). */
export function useApiUrls() {
  const apiUrl = usePublicApiUrl()

  return { apiUrl, mcpUrl: `${apiUrl}/mcp` }
}

/** All MCP tools (apps/api/internal/mcpserver). */
export const MCP_TOOL_COUNT = 71

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
            <InlineCode>COOKIE_SECURE=true</InlineCode>. There you sign in with an admin API key and create scoped API
            keys; clients switch by using the cloud URL and <InlineCode>Authorization: Bearer stk_...</InlineCode>.
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
