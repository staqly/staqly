import { DESCRIPTION, DOCS, GITHUB, SIGNUP, SITE_URL, TAGLINE } from '@/lib/site'
import { allCases } from '@/lib/use-cases'

export const dynamic = 'force-static'

// https://llmstxt.org — a plain-markdown map of the site for LLMs and agents.
export function GET() {
  const body = `# staqly

> ${TAGLINE}. ${DESCRIPTION}

Agents connect over MCP (Claude Code, Codex, OpenCode, Cursor and any MCP client) or REST, create items, wait for a human
decision with \`await_decision\` and report what they did with \`report_result\`. Hosted workspaces start free (50 approvals a
month, unlimited items and collections, 7-day Pro trial); Pro is $30/month with unlimited approvals. The app is open source (Apache-2.0) and can be self-hosted.

## Site

- [Home](${SITE_URL}/): what staqly does and how the send → review → act loop works
- [Agents](${SITE_URL}/agents): one-line MCP setup for every agent and the main MCP tools
- [Pricing](${SITE_URL}/pricing): Free and Pro plans
- [Use cases](${SITE_URL}/use-cases): where a human decision belongs between an agent and the real world

## Use cases

${allCases.map((u) => `- [${u.title}](${SITE_URL}/use-cases/${u.slug}): ${u.summary}`).join('\n')}

## Docs

- [Documentation](${DOCS}/): full docs, with its own llms.txt at ${DOCS}/llms.txt
- [Quickstart](${DOCS}/quickstart): install, create a first item, connect an agent
- [MCP tools reference](${DOCS}/mcp/tools): every tool an agent can call
- [API reference](${DOCS}/api/reference): the REST API

## Optional

- [Create a free workspace](${SIGNUP})
- [Source code on GitHub](${GITHUB})
`

  return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
