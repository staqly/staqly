import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { describeSchedule, SchedulesSection } from '@/components/collections/schedules-section'

import { channel, collection, schedule } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('collection schedules', () => {
  it('describes a rule in words', () => {
    expect(describeSchedule(schedule())).toBe('Mon to Fri at 09:00')
    expect(describeSchedule(schedule({ rule: { times: ['08:00', '18:00'], weekdays: [0, 1, 2, 3, 4, 5, 6] } }))).toBe(
      'Every day at 08:00, 18:00',
    )
    // 0 is Sunday (the API's numbering); it reads last, like on the picker.
    expect(describeSchedule(schedule({ rule: { times: ['10:00'], weekdays: [0, 6] } }))).toBe('Sat, Sun at 10:00')
    expect(describeSchedule(schedule({ rule: { every_minutes: 240 } }))).toBe('Every 4 h')
    expect(describeSchedule(schedule({ kind: 'once', run_at: null }))).toBe('Once')
  })

  it('lists the schedules with what they publish, toggles one off and runs one now', async () => {
    const api = mockApi({
      'GET /v1/schedules': {
        data: [schedule(), schedule({ id: 'sch_2', name: 'Agent fill', pick: 'all', source: 'agent' })],
      },
      'GET /v1/channels': { data: [channel()] },
      'PATCH /v1/schedules/sch_1': schedule({ enabled: false }),
      'POST /v1/schedules/sch_2/run': schedule({ id: 'sch_2', last_result: 'Asked for posts' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<SchedulesSection collection={collection()} />, { mode: 'local' })
    const rows = await screen.findAllByTestId('schedule-row')

    expect(rows).toHaveLength(2)
    expect(within(rows[0]!).getByText('Mon to Fri at 09:00')).toBeInTheDocument()
    expect(within(rows[0]!).getByText('Publishes the next waiting post')).toBeInTheDocument()
    expect(within(rows[1]!).getByText(/asks your agent when nothing waits/)).toBeInTheDocument()
    await user.click(within(rows[0]!).getByRole('switch'))
    await waitFor(() => expect(api.find('PATCH', '/v1/schedules/sch_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/schedules/sch_1')[0].body).toEqual({ enabled: false })
    await user.click(within(rows[1]!).getByRole('button', { name: 'Actions for Agent fill' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Run now' }))
    await waitFor(() => expect(api.find('POST', '/v1/schedules/sch_2/run')).toHaveLength(1))
  })

  it('creates a recurring schedule from the drawer with times, weekdays, channels and an agent source', async () => {
    const api = mockApi({
      'GET /v1/schedules': { data: [] },
      'GET /v1/channels': { data: [channel(), channel({ id: 'ch_2', name: 'Brand on Bluesky', provider: 'bluesky' })] },
      'POST /v1/schedules': schedule({ id: 'sch_9', name: 'Mornings' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<SchedulesSection collection={collection()} />, { mode: 'local' })
    await user.click(await screen.findByTestId('new-schedule'))
    const sheet = await screen.findByRole('dialog', { name: 'New schedule' })
    const name = within(sheet).getByLabelText(/^Name/)

    await user.clear(name)
    await user.type(name, 'Mornings')
    // Saturday on, the default weekdays stay.
    await user.click(within(sheet).getByRole('checkbox', { name: 'Sat' }))
    await user.click(within(sheet).getByRole('radio', { name: 'Ask my agent for new posts' }))
    await user.click(within(sheet).getByRole('button', { name: 'Create schedule' }))
    await waitFor(() => expect(api.find('POST', '/v1/schedules')).toHaveLength(1))
    expect(api.find('POST', '/v1/schedules')[0].body).toMatchObject({
      collection: 'col_1',
      name: 'Mornings',
      kind: 'recurring',
      rule: { times: ['09:00'], weekdays: [1, 2, 3, 4, 5, 6] },
      pick: 'next',
      source: 'agent',
      enabled: true,
    })
  })
})
