import { faqs } from '@/lib/faqs'
import { LLMS } from '@/lib/llms'
import { DOCS, SITE_URL } from '@/lib/site'
import { allCases } from '@/lib/use-cases'

export const dynamic = 'force-static'

// llms.txt with the pages' text in full: the home page's questions and every use case, step by step.
export function GET() {
  const faq = faqs.map(([q, a]) => `### ${q}\n\n${a}`).join('\n\n')
  const cases = allCases
    .map(
      (u) => `### ${u.title}

${SITE_URL}/use-cases/${u.slug}

${u.intro}

**The problem.** ${u.problem}

${u.steps.map((s, i) => `${i + 1}. ${s}`).join('\n')}

**The result.** ${u.result}

Docs: ${u.docs.map(([label, href]) => `[${label}](${DOCS}${href})`).join(', ')}`,
    )
    .join('\n\n')

  const body = `${LLMS}
## Questions and answers

${faq}

## Use cases in full

${cases}
`

  return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
