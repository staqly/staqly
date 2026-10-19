import type { Metadata } from 'next'

import { PreviewPage } from '@/components/preview/preview-page'

// The token is in the fragment; no-referrer keeps the page's address out of requests to other sites too.
export const metadata: Metadata = { title: 'Post preview', referrer: 'no-referrer', robots: { index: false } }

export default function Page() {
  return <PreviewPage />
}
