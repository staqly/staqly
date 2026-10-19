import { DESCRIPTION, DOCS, GITHUB, SIGNUP, SITE_URL, TAGLINE } from '@/lib/site'
import { allCases } from '@/lib/use-cases'

export const dynamic = 'force-static'

// https://llmstxt.org — a plain-markdown map of the site for LLMs and agents.
export function GET() {
  const body = `# staqly

> ${TAGLINE}. ${DESCRIPTION}

Agents connect over MCP (Claude Code, Codex, OpenCode, Cursor and any MCP client) or REST, write posts with \`create_post\`,
put them on the calendar with \`schedule_post\` and refill a collection when \`list_requested_work\` shows a schedule out of
posts. Posts go to connected channels; a channel on your own automation hands them to n8n or a script. Hosted workspaces start
free (50 posts a month, 3 channels, 7-day Pro trial); Pro is $30/month with unlimited posts and channels. The app is open
source (AGPL-3.0) and can be self-hosted.

## Site

- [Home](${SITE_URL}/): what staqly does and how the write, schedule, publish loop works
- [Agents](${SITE_URL}/agents): one-line MCP setup for every agent and the main MCP tools
- [Pricing](${SITE_URL}/pricing): Free and Pro plans
- [Use cases](${SITE_URL}/use-cases): agents that refill a collection, one post per channel, publishing through n8n

## Use cases

${allCases.map((u) => `- [${u.title}](${SITE_URL}/use-cases/${u.slug}): ${u.summary}`).join('\n')}

## Docs

- [Documentation](${DOCS}/): full docs, with its own llms.txt at ${DOCS}/llms.txt
- [Quickstart](${DOCS}/quickstart): install, connect a channel, write and publish a first post, connect an agent
- [MCP tools reference](${DOCS}/mcp/tools): every tool an agent can call
- [API reference](${DOCS}/api/reference): the REST API

## Optional

- [Create a free workspace](${SIGNUP})
- [Source code on GitHub](${GITHUB})
`

  return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
