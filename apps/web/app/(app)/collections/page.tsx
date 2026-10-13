import type { Metadata } from 'next'
import { Suspense } from 'react'

import { CollectionsIndex } from '@/components/collections/collections-index'

export const metadata: Metadata = { title: 'Collections' }

export default function CollectionsPage() {
  return (
    <Suspense>
      <CollectionsIndex />
    </Suspense>
  )
}
