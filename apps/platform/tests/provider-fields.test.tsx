import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProviderFields } from '@/components/channels/provider-fields'
import type { ProviderField } from '@/lib/api'

const board = { key: 'board', label: 'Board', kind: 'remote', required: true, max_items: 1 } as ProviderField

describe('a remote setting', () => {
  it('whose list fails shows a disabled select and the reason in an alert, not a text box', async () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ProviderFields
          fields={[board]}
          values={{}}
          onChange={() => {}}
          idPrefix="t"
          remote={{ channelId: 'c1', load: () => Promise.reject(new Error('Pinterest is on Trial access')) }}
        />
      </QueryClientProvider>,
    )

    expect(await screen.findByRole('alert')).toHaveTextContent('Pinterest is on Trial access')
    expect(screen.getByRole('combobox', { name: 'Board' })).toBeDisabled()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  })
})
