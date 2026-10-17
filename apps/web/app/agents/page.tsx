import { ArrowRight } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'

import { AgentTabs, type AgentSetup } from '@/components/agent-tabs'
import { CtaButtons, Eyebrow, H2, PageHero } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { pageMeta } from '@/lib/seo'
import { DOCS, GITHUB, MCP_URL, arrowLink, wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Connect Claude Code, Codex, Cursor and any MCP agent',
  'Add staqly to Claude Code, OpenAI Codex, OpenCode, Cursor or any MCP client in one line, so your agent saves what it makes and waits for human approval before it acts.',
  '/agents',
)

const agents: AgentSetup[] = [
  {
    name: 'Claude Code',
    title: 'Terminal',
    code: `$ claude mcp add --transport http staqly ${MCP_URL} \\
    --header "Authorization: Bearer $STAQLY_API_KEY"`,
    guide: '/agents/claude-code',
  },
  {
    name: 'OpenAI Codex',
    title: 'Terminal',
    code: `$ codex mcp add staqly --url ${MCP_URL} \\
    --bearer-token-env-var STAQLY_API_KEY`,
    guide: '/agents/codex',
  },
  {
    name: 'OpenCode',
    title: 'opencode.json',
    code: `{
  "mcp": {
    "staqly": {
      "type": "remote",
      "url": "${MCP_URL}",
      "headers": { "Authorization": "Bearer {env:STAQLY_API_KEY}" }
    }
  }
}`,
    guide: '/agents/opencode',
  },
  {
    name: 'Cursor & other MCP clients',
    title: 'Streamable HTTP',
    code: `URL     ${MCP_URL}
Header  Authorization: Bearer stk_...`,
    guide: '/agents/other-mcp-clients',
  },
]

// From apps/docs/content/docs/mcp/tools.mdx (71 tools); the ones an agent reaches for most.
const tools: [string, string][] = [
  ['create_item', 'Store what the agent made, or the action it wants to take, as a pending item'],
  ['await_decision', 'Wait for a human to approve (with or without edits) or reject; 45 s per call by default'],
  ['report_result', 'Record what happened after acting: completed or failed, with details'],
  ['list_pending_approvals', 'See what is still waiting for a human'],
  ['search_items', 'Full-text search with status and decision-date filters'],
  ['create_collection', 'Group output, e.g. “Instagram Avatar” or “Deploys”'],
  ['create_watcher', 'Follow any RSS/Atom feed or web page for new entries and changes'],
]

export default function AgentsPage() {
  return (
    <main className="flex-1">
      <PageHero eyebrow="Agents" title="Give your agent a way to ask a human first.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          staqly speaks MCP. Add it to Claude Code, Codex, OpenCode, Cursor or any MCP client and your agent can save
          what it makes, wait for your decision and act only on what you approved.
        </p>
        <CtaButtons />
      </PageHero>

      <section className={`${wrap} py-14 sm:py-20`}>
        <Eyebrow>Install</Eyebrow>
        <H2>One line per agent.</H2>
        <p className="mt-4 max-w-2xl text-muted-foreground">
          Create a workspace, copy its API key into{' '}
          <code className="rounded bg-muted px-1 font-mono text-[0.9em]">STAQLY_API_KEY</code> and add one line to your
          agent.
        </p>
        <AgentTabs agents={agents.map((a) => ({ ...a, guide: DOCS + a.guide }))} />
        <p className="mt-6 text-sm text-muted-foreground">
          Not an MCP agent? Use the{' '}
          <a href={`${DOCS}/api`} className="font-medium text-foreground hover:underline">
            REST API
          </a>
          ,{' '}
          <a href={`${DOCS}/integrations/n8n`} className="font-medium text-foreground hover:underline">
            n8n
          </a>
          , the{' '}
          <a href={`${DOCS}/cli`} className="font-medium text-foreground hover:underline">
            CLI
          </a>{' '}
          or the{' '}
          <a href={`${DOCS}/sdk/typescript`} className="font-medium text-foreground hover:underline">
            TypeScript
          </a>{' '}
          and{' '}
          <a href={`${DOCS}/sdk/python`} className="font-medium text-foreground hover:underline">
            Python
          </a>{' '}
          SDKs.
        </p>
      </section>

      <section className="border-y border-border bg-card/50">
        <div className={`${wrap} py-14 sm:py-20`}>
          <Eyebrow>What the agent gets</Eyebrow>
          <H2>71 tools. These are the ones that matter.</H2>
          <div className="mt-10 overflow-hidden rounded-2xl border border-border bg-background">
            <table className="block w-full text-left text-sm sm:table">
              <tbody className="block sm:table-row-group">
                {tools.map(([name, purpose]) => (
                  <tr key={name} className="block border-b border-border py-3 last:border-0 sm:table-row sm:py-0">
                    <td className="block px-5 font-mono text-[13px] text-brand-strong sm:table-cell sm:py-3.5 sm:align-top sm:whitespace-nowrap">
                      {name}
                    </td>
                    <td className="block px-5 pt-1 text-muted-foreground sm:table-cell sm:py-3.5">{purpose}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <a href={`${DOCS}/mcp/tools`} className={`mt-5 ${arrowLink} pointer-coarse:mt-2.5`}>
            All 71 tools <ArrowRight className="size-4" />
          </a>
        </div>
      </section>

      <section className={`${wrap} grid gap-10 py-14 sm:py-20 lg:grid-cols-[0.9fr_1.1fr] lg:items-center`}>
        <div>
          <Eyebrow>How it runs</Eyebrow>
          <H2>Propose → await → report.</H2>
          <ol className="mt-6 space-y-3 text-sm leading-relaxed">
            {[
              'The agent proposes: create_item with what it made or wants to do.',
              'It waits: await_decision returns once you approve (maybe with edits) or reject; on a timeout it calls again.',
              'It acts on exactly what you approved, including your edits.',
              'It reports: report_result closes the loop and lands in the history.',
            ].map((s, i) => (
              <li key={s} className="flex gap-4">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-lg border border-border bg-card font-mono text-xs text-brand">
                  {i + 1}
                </span>
                <span className="pt-0.5">{s}</span>
              </li>
            ))}
          </ol>
        </div>
        <TerminalCard
          title="claude"
          code={`# you: "Draft the release email, but ask me before sending it"
create_item    {"collection":"Outbox","title":"v1.4 release email","expires_in":"2h"}
await_decision {"item_id":"itm_..."}
→ approved (subject edited)
# agent sends the approved version
report_result  {"item_id":"itm_...","status":"completed"}`}
        />
      </section>
      <section className="border-t border-border">
        <div className={`${wrap} py-14 sm:py-20`}>
          <Eyebrow>Use cases</Eyebrow>
          <H2>See it applied.</H2>
          <p className="mt-6 max-w-3xl text-lg leading-relaxed text-muted-foreground">
            Social posts and generated images, refunds, outbound email, production deploys, risky coding-agent actions —{' '}
            <Link
              href="/use-cases"
              className="font-medium text-foreground underline decoration-brand/50 underline-offset-4 hover:decoration-brand"
            >
              see what people use it for
            </Link>
            . Full reference:{' '}
            <a
              href={`${DOCS}/mcp`}
              className="font-medium text-foreground underline decoration-brand/50 underline-offset-4 hover:decoration-brand"
            >
              MCP server docs
            </a>{' '}
            and the{' '}
            <a
              href={`${GITHUB}/blob/main/apps/api/openapi.yaml`}
              className="font-medium text-foreground underline decoration-brand/50 underline-offset-4 hover:decoration-brand"
            >
              OpenAPI spec
            </a>
            .
          </p>
        </div>
      </section>
    </main>
  )
}
