import { DESCRIPTION, DOCS, GITHUB, SIGNUP, SITE_URL } from '@/lib/site'
import { TOOLS } from '@/lib/tools'
import { allCases } from '@/lib/use-cases'

export const dynamic = 'force-static'

// https://llmstxt.org — a plain-markdown map of the site for LLMs and agents.
export function GET() {
  const body = `# staqly

> ${DESCRIPTION}

Agents connect over MCP (Claude Code, Codex, OpenCode, Cursor and any MCP client) or REST, write posts with \`create_post\`,
put them on the calendar with \`schedule_post\` and refill a collection when \`list_requested_work\` shows a schedule out of
posts. Posts go to connected channels; a channel on your own automation hands them to n8n or a script. Hosted workspaces start
with a free 7-day Starter trial, card required (Plus, Pro and Business are paid from the first day); plans are per user, excl. VAT: Starter €15, Plus €29, Pro €59 and Business €99 a month (billed yearly: €12, €23, €47 and €79 a month),
with unlimited posts on every plan. The app is open
source (AGPL-3.0) and can be self-hosted as Staqly Community Edition, with no plan limits.

## Site

- [Home](${SITE_URL}/): what staqly does and how the write, schedule, publish loop works
- [Agents](${SITE_URL}/agents): one-line MCP setup for every agent and the main MCP tools
- [Pricing](${SITE_URL}/pricing): Starter, Plus, Pro and Business, monthly or yearly (save 20 % or more), Starter with a 7-day free trial
- [Compare plans](${SITE_URL}/pricing/compare): every limit and feature by plan
- [Use cases](${SITE_URL}/use-cases): agents that refill a collection, one post per channel, publishing through n8n
- [Free tools](${SITE_URL}/tools): browser tools for social posts, no sign-up

## Use cases

${allCases.map((u) => `- [${u.title}](${SITE_URL}/use-cases/${u.slug}): ${u.summary}`).join('\n')}

## Free tools

${TOOLS.map((t) => `- [${t.title}](${SITE_URL}/tools/${t.slug}): ${t.summary}`).join('\n')}

## Docs

- [Documentation](${DOCS}/): full docs, with its own llms.txt at ${DOCS}/llms.txt
- [Quickstart](${DOCS}/quickstart): install, connect a channel, write and publish a first post, connect an agent
- [MCP tools reference](${DOCS}/mcp/tools): every tool an agent can call
- [API reference](${DOCS}/api/reference): the REST API

## Optional

- [Create your account, Starter free for 7 days](${SIGNUP})
- [Source code on GitHub](${GITHUB})
`

  return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
