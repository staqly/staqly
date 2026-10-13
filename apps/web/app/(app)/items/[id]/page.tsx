import type { Metadata } from 'next'

import { ItemRoute } from '@/components/items/item-route'

export const metadata: Metadata = { title: 'Item' }

// One prerendered shell for every id; the Go web server maps /items/* onto it.
export function generateStaticParams() {
  return [{ id: '_' }]
}

export default function Page() {
  return <ItemRoute />
}
