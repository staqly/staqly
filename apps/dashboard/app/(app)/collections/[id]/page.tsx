import type { Metadata } from 'next'
import { Suspense } from 'react'

import { CollectionRoute } from '@/components/collections/collection-route'

export const metadata: Metadata = { title: 'Collection' }

// One prerendered shell for every id; the Go web server maps /collections/* onto it.
export function generateStaticParams() {
  return [{ id: '_' }]
}

export default function Page() {
  return (
    <Suspense>
      <CollectionRoute />
    </Suspense>
  )
}
