'use client'

import { Blocks } from 'lucide-react'
import type { ReactNode } from 'react'

import { PageHeader } from '@/components/common/page-header'
import { PageTabs } from '@/components/common/page-tabs'
import { useMode } from '@/lib/queries'

/** Integrations title and its sections: Setup, Agents & activity, (cloud mode) API keys and AI. */
export function IntegrationsHeader({
  active,
  setupHref = '/integrations',
  agentsHref = '/integrations?view=agents',
  actions,
}: {
  active: 'setup' | 'agents' | 'keys' | 'ai'
  setupHref?: string
  agentsHref?: string
  actions?: ReactNode
}) {
  const { isLocal } = useMode()

  return (
    <>
      <PageHeader
        title="Integrations"
        icon={<Blocks className="size-4" />}
        description="Connect an agent, see which agents talk to this workspace and what they did. Step-by-step guides live at docs.staqly.ai."
        actions={actions}
      />
      <PageTabs
        label="Integrations sections"
        className="mt-4"
        tabs={[
          { href: setupHref, label: 'Setup', active: active === 'setup' },
          { href: agentsHref, label: 'Agents & activity', active: active === 'agents', testId: 'agents-tab' },
          ...(isLocal ? [] : [{ href: '/integrations/api-keys', label: 'API keys', active: active === 'keys' }]),
          { href: '/integrations/ai', label: 'AI', active: active === 'ai', testId: 'ai-tab' },
        ]}
      />
    </>
  )
}
