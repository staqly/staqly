import { Settings } from 'lucide-react'

import { PageHeader } from '@/components/common/page-header'

/** The one header every Settings section shares. */
export function SettingsHeader() {
  return (
    <PageHeader
      title="Settings"
      icon={<Settings className="size-4" />}
      description="Workspace, plan, data and system status."
    />
  )
}
