import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { agentActivity } from '@/components/integrations/agents'
import { IntegrationsPage } from '@/components/integrations/pages'
import type { Agent } from '@/lib/api'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const now = Date.parse('2026-10-14T12:00:00Z')

function agent(over: Partial<Agent> = {}): Agent {
  return {
    id: 'agt_1',
    kind: 'mcp',
    name: 'claude-code',
    version: '2.1.0',
    principal_id: 'local',
    principal_name: '',
    first_seen_at: '2026-10-14T10:00:00Z',
    last_seen_at: '2026-10-14T10:00:00Z',
    requests: 12,
    posts_created: 3,
    ...over,
  }
}

const AGENTS = [agent(), agent({ id: 'agt_2', kind: 'api', name: 'n8n', version: '1.80' })]

describe('agents', () => {
  it('active within 2 minutes, recent today, idle after', () => {
    expect(agentActivity({ last_seen_at: '2026-10-14T11:59:00Z' }, now)).toBe('active')
    expect(agentActivity({ last_seen_at: '2026-10-14T08:00:00Z' }, now)).toBe('recent')
    expect(agentActivity({ last_seen_at: '2026-10-12T08:00:00Z' }, now)).toBe('idle')
  })

  it('lists agents on Integrations > Agents & activity; recent activity shows every agent until one is selected; forget removes one', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      'GET /v1/agents': { data: AGENTS },
      'GET /v1/events': { data: [] },
      'DELETE /v1/agents/agt_2': null,
    })

    nav.set('/integrations', 'view=agents')
    renderWithProviders(<IntegrationsPage />, { mode: 'local' })
    const agents = await screen.findByTestId('agents')

    expect(await within(agents).findByText('claude-code')).toBeInTheDocument()
    expect(within(agents).getByText('MCP')).toBeInTheDocument()
    // MCP clients are agents; REST clients are API connections.
    expect(within(within(agents).getByRole('region', { name: 'Agents' })).getByText('claude-code')).toBeInTheDocument()
    expect(within(within(agents).getByRole('region', { name: 'API connections' })).getByText('n8n')).toBeInTheDocument()
    expect(within(agents).getAllByText(/No key \(this machine\)/)).toHaveLength(2)

    // Nothing selected: every agent, whatever tab is open.
    const lastQuery = () => api.find('GET', '/v1/events').at(-1)?.query

    await waitFor(() => expect(lastQuery()).toBeDefined())
    expect(lastQuery()?.getAll('agent')).toEqual([])
    expect(screen.getByTestId('recent-activity')).not.toHaveTextContent('Recent activity ·')

    // Selecting an agent narrows to it (and its credential); Show all goes back.
    await user.click(within(agents).getByRole('button', { name: /^n8n/, pressed: false }))
    await waitFor(() => expect(lastQuery()?.getAll('agent')).toEqual(['n8n']))
    expect(lastQuery()?.get('actor_id')).toBe('local')
    expect(screen.getByTestId('recent-activity')).toHaveTextContent('Recent activity · n8n')
    await user.click(screen.getByRole('button', { name: 'Show all' }))
    expect(screen.getByTestId('recent-activity')).not.toHaveTextContent('Recent activity ·')
    expect(within(agents).getByRole('button', { name: /^n8n/ })).toHaveAttribute('aria-pressed', 'false')

    await user.click(within(agents).getByRole('button', { name: 'Remove n8n from the list' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/agents/agt_2')).toHaveLength(1))
  })
})
