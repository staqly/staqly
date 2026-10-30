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
  'Add staqly to Claude Code, OpenAI Codex, OpenCode, Cursor or any MCP client in one line, so your agent writes posts, puts them on the calendar and refills a collection when a schedule asks.',
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

// From apps/docs/content/docs/mcp/tools.mdx (100 tools); the ones an agent reaches for most.
const tools: [string, string][] = [
  ['create_post', 'Write a post into a collection: text, media, one target per channel with its own text or settings'],
  ['preview_post', 'See the text each channel will carry and what its platform objects to before publishing'],
  ['schedule_post', 'Put a post on the calendar; publish_post sends it now, cancel_post takes it off'],
  [
    'list_requested_work',
    'See what the workspace asks of the agent: schedules out of posts, targets waiting for a result',
  ],
  [
    'create_schedule',
    'Weekday mornings, every six hours or once: which posts, which channels, what to do when nothing waits',
  ],
  ['list_channels', 'The connected accounts with their status; connect_channel adds a token platform'],
  ['report_publish_result', 'Close a target on your own automation with the published link or an error'],
]

export default function AgentsPage() {
  return (
    <main id="main" className="flex-1">
      <PageHero eyebrow="Agents" title="Give your agent a calendar to write into.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Add Staqly to Claude Code, Codex or Cursor in one line. Your agent writes the posts, Staqly puts them on time.
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
          <H2>100 tools. These are the ones that matter.</H2>
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
            All 100 tools <ArrowRight className="size-4" />
          </a>
        </div>
      </section>

      <section className={`${wrap} grid gap-10 py-14 sm:py-20 lg:grid-cols-[0.9fr_1.1fr] lg:items-center`}>
        <div>
          <Eyebrow>How it runs</Eyebrow>
          <H2>Write, preview, schedule, refill.</H2>
          <ol className="mt-6 space-y-3 text-sm leading-relaxed">
            {[
              'The agent writes: create_post with the text, the media and one target per channel.',
              'It checks: preview_post says what each platform objects to; update_post fixes it.',
              'It schedules: schedule_post for a time, or leaves a draft for the collection\u2019s schedule.',
              'It refills: list_requested_work shows a schedule out of posts; the agent writes the next batch.',
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
          code={`# you: "Announce v1.4 on every channel tomorrow at 9"
list_channels
create_post    {"collection":"Product updates","title":"v1.4","content":"v1.4 is out: ...",
                "targets":[{"channel_id":"ch_n8n..."},{"channel_id":"ch_x...","content":"v1.4: ..."}]}
preview_post   {"post_id":"post_..."}  → every target ok
schedule_post  {"post_id":"post_...","publish_at":"2026-10-21T09:00:00Z"}`}
        />
      </section>
      <section className="border-t border-border">
        <div className={`${wrap} py-14 sm:py-20`}>
          <Eyebrow>Use cases</Eyebrow>
          <H2>See it applied.</H2>
          <p className="mt-6 max-w-5xl text-lg leading-relaxed text-muted-foreground">
            A week of posts from one prompt, one post in every channel&apos;s length, publishing through n8n, feeding a
            schedule from your own endpoint:{' '}
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
