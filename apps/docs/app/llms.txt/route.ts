import { DOCS_DESCRIPTION, absUrl } from '@/lib/seo'
import { docsLlms } from '@/lib/source'

export const revalidate = false

// https://llmstxt.org: a title, a one-paragraph summary, then every page. Links are absolute so an agent that fetched
// only this file can follow them; each page's Markdown is at the same URL plus .md, the whole set in /llms-full.txt.
export async function GET() {
  const index = (await docsLlms.index())
    .replace(/^# .*\n+/, '')
    .replaceAll('](/', `](${absUrl('/')}/`)
    // Sidebar folders become the H2 sections llmstxt.org expects ("- **Channels**" -> "## Channels").
    .replace(/^- \*\*(.+)\*\*$/gm, '## $1')
  const head = `# staqly docs

> ${DOCS_DESCRIPTION} People and agents (over MCP or the REST API) write posts into collections, schedules publish
> them to connected channels, and webhooks report what happened. Every page is also Markdown at its URL plus \`.md\`
> (${absUrl('/quickstart.md')}); all pages in one file: ${absUrl('/llms-full.txt')}.

`

  return new Response(head + index, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
