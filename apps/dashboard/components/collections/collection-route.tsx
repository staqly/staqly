'use client'

import { CollectionPage } from '@/components/collections/collection-page'
import { useRouteId } from '@/hooks/use-route-id'

export function CollectionRoute() {
  const id = useRouteId()

  return id && <CollectionPage id={id} />
}
