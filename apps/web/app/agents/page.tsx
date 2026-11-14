import { ArrowRight, BookOpen } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'

import { AgentTabs, type AgentSetup } from '@/components/agent-tabs'
import { CtaButtons, PageHero, SectionHead, Step } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { pageMeta } from '@/lib/seo'
import { DOCS, GITHUB, MCP_URL, arrowLink, btnSecondary, wrap } from '@/lib/site'

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

const asks = [
  'Draft five posts about the new release into Product updates, one per weekday morning.',
  'What is scheduled this week, and which day is empty?',
  'Make the X version shorter and publish it now.',
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

const keys: [string, string, string][] = [
  [
    'A drafting agent',
    'posts:write',
    'Writes and edits posts and fills collections. Its drafts wait for a schedule, or for you, to go out.',
  ],
  [
    'A publishing agent',
    'posts:publish',
    'May also schedule, publish now and cancel. Give it to the agent you trust to press the button.',
  ],
]

export default function AgentsPage() {
  return (
    <main id="main" className="flex-1">
      <PageHero
        eyebrow="AI agents"
        title={
          <>
            Give your agent <span className="sq-gradient-text">a calendar to write into.</span>
          </>
        }
        aside={
          <TerminalCard
            title="claude"
            code={`# you: "Announce v1.4 on every channel tomorrow at 9"
list_channels
create_post    {"collection":"Product updates","title":"v1.4",
                "content":"v1.4 is out: ...","targets":[...]}
preview_post   {"post_id":"post_..."}  → every target ok
schedule_post  {"post_id":"post_...","publish_at":"...T09:00:00Z"}
→ on the calendar, Thu 09:00`}
          />
        }
      >
        <p className="mt-6 max-w-xl text-lg leading-relaxed text-pretty text-muted-foreground">
          Connect Claude Code, Codex, Cursor or any MCP client in one line. Your agent writes the posts and puts them on
          the calendar; Staqly publishes them on time and asks for more when a collection runs dry.
        </p>
        <CtaButtons
          secondary={
            <a href={`${DOCS}/mcp`} className={btnSecondary}>
              <BookOpen className="size-4 text-muted-foreground" /> MCP docs
            </a>
          }
        />
      </PageHero>

      <section className={`${wrap} py-20 sm:py-24`}>
        <SectionHead eyebrow="Just ask" title="Talk to your agent like you would to a social media manager." />
        <ul className="mt-10 grid gap-4 md:grid-cols-3">
          {asks.map((a) => (
            <li
              key={a}
              className="sq-reveal rounded-2xl border border-border bg-card p-6 text-lg leading-snug font-medium tracking-tight text-balance"
            >
              <span className="sq-gradient-text text-3xl leading-none">“</span>
              {a}
            </li>
          ))}
        </ul>
        <p className="mt-6 text-sm text-muted-foreground">
          No agent? The dashboard has an assistant of its own that does the same, and asks before every change.
        </p>
      </section>

      <section id="install" className="scroll-mt-16 border-y border-border bg-card/40">
        <div className={`${wrap} py-20 sm:py-24`}>
          <SectionHead eyebrow="Install" title="One line per agent.">
            Create a workspace, copy its API key into{' '}
            <code className="rounded bg-muted px-1 font-mono text-[0.9em]">STAQLY_API_KEY</code> and add one line to
            your agent.
          </SectionHead>
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
        </div>
      </section>

      <section
        className={`${wrap} grid gap-12 py-20 sm:py-24 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] lg:items-center`}
      >
        <div>
          <SectionHead eyebrow="How it runs" title="Write, preview, schedule, refill." />
          <ol className="mt-8 space-y-4">
            <Step n={1}>The agent writes: create_post with the text, the media and one target per channel.</Step>
            <Step n={2}>It checks: preview_post says what each platform objects to; update_post fixes it.</Step>
            <Step n={3}>It schedules: schedule_post for a time, or leaves a draft for the collection’s schedule.</Step>
            <Step n={4}>
              It refills: list_requested_work shows a schedule out of posts; the agent writes the next batch.
            </Step>
          </ol>
        </div>
        <div className="sq-reveal rounded-3xl border border-border bg-card p-7 sm:p-9">
          <p className="text-sm font-semibold text-brand-strong">Writing is not publishing</p>
          <h3 className="mt-3 text-2xl font-semibold tracking-tight text-balance">
            Decide which agent may press the button.
          </h3>
          <dl className="mt-6 space-y-5">
            {keys.map(([who, scope, what]) => (
              <div key={scope} className="border-l-2 border-brand/60 pl-5">
                <dt className="flex flex-wrap items-center gap-2 font-semibold">
                  {who}
                  <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-brand-strong">{scope}</code>
                </dt>
                <dd className="mt-1 text-sm leading-relaxed text-muted-foreground">{what}</dd>
              </div>
            ))}
          </dl>
          <p className="mt-6 text-xs leading-relaxed text-muted-foreground">
            On Staqly Cloud every API key carries its own scopes. In the self-hosted Community Edition the access token
            has full access.{' '}
            <a href={`${DOCS}/api/authentication`} className="underline underline-offset-4 hover:text-foreground">
              Keys and scopes
            </a>
          </p>
        </div>
      </section>

      <section className="border-y border-border bg-card/40">
        <div className={`${wrap} py-20 sm:py-24`}>
          <SectionHead eyebrow="What the agent gets" title="100 tools. These are the ones that matter." />
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
          <div className="mt-6 flex flex-wrap gap-x-6 gap-y-2">
            <a href={`${DOCS}/mcp/tools`} className={arrowLink}>
              All 100 tools <ArrowRight className="size-4" />
            </a>
            <a href={`${GITHUB}/blob/main/apps/api/openapi.yaml`} className={arrowLink}>
              OpenAPI spec <ArrowRight className="size-4" />
            </a>
          </div>
        </div>
      </section>

      <section className={`${wrap} py-20 sm:py-24`}>
        <div className="flex flex-col gap-6 rounded-3xl border border-[color-mix(in_oklch,var(--brand)_30%,transparent)] bg-brand-soft/30 p-7 sm:flex-row sm:items-center sm:justify-between sm:p-10">
          <div className="max-w-2xl">
            <h2 className="text-2xl font-semibold tracking-tight text-balance">See what people build with it.</h2>
            <p className="mt-2 leading-relaxed text-muted-foreground">
              A week of posts from one prompt, one post in every channel’s length, publishing through n8n, feeding a
              schedule from your own endpoint.
            </p>
          </div>
          <Link href="/use-cases" className={`${btnSecondary} shrink-0 self-start sm:self-center`}>
            Browse use cases <ArrowRight className="size-4" />
          </Link>
        </div>
      </section>
    </main>
  )
}
