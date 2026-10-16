'use client'

import { PageContainer } from '@/components/common/page-header'
import { ItemDetail } from '@/components/items/item-detail'
import { useRouteId } from '@/hooks/use-route-id'

export function ItemRoute() {
  const id = useRouteId()

  return <PageContainer>{id && <ItemDetail id={id} />}</PageContainer>
}
