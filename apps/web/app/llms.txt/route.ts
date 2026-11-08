import { LLMS } from '@/lib/llms'

export const dynamic = 'force-static'

export function GET() {
  return new Response(LLMS, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}
