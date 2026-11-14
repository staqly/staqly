'use client'

import { Fragment, type ReactNode } from 'react'

import { useSession } from '@/lib/queries'
import { setDisplayZone } from '@/lib/zone'

/**
 * Every date below is shown and picked in the workspace's time zone. Set during render, before the children read
 * it; a new zone (another workspace, Settings saved) remounts them, so memoised rows and cached labels redraw.
 */
export function WorkspaceZone({ children }: { children: ReactNode }) {
  const zone = useSession().data?.workspace?.timezone ?? ''

  setDisplayZone(zone)

  return <Fragment key={zone}>{children}</Fragment>
}
