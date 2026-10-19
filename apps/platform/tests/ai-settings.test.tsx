import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { AiSettings } from '@/components/integrations/ai-settings'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const MODELS = ['dall-e-2', 'dall-e-3', 'gpt-image-1', 'gpt-image-1-mini']
const OFF = {
  configured: false,
  provider: 'openai',
  model: '',
  key_hint: '',
  capabilities: { generate: false, edit: false, inpaint: false, expand: false },
  reason: 'Add an OpenAI API key under Integrations to use the AI tools.',
  models: MODELS,
  can_manage: true,
}
const ON = {
  ...OFF,
  configured: true,
  model: 'gpt-image-1',
  key_hint: '...a1b2',
  capabilities: { generate: true, edit: true, inpaint: true, expand: true },
  reason: '',
}

describe('Integrations › AI', () => {
  it('adds a key in a modal and shows what the model can do', async () => {
    const api = mockApi({ 'GET /v1/ai': OFF, 'PUT /v1/ai': ON })

    nav.set('/integrations/ai')
    const user = userEvent.setup()

    renderWithProviders(<AiSettings />, { mode: 'cloud' })
    expect(await screen.findAllByText(/^Not available/)).toHaveLength(4)
    expect(screen.getByText('No key yet')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Add key' }))
    const dialog = await screen.findByRole('dialog')

    await user.click(within(dialog).getByTestId('ai-save'))
    expect(await within(dialog).findByText('Paste the key from your OpenAI account')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('API key'), 'sk-proj-test-0123456789a1b2')
    await user.click(within(dialog).getByTestId('ai-save'))
    await waitFor(() => expect(api.find('PUT', '/v1/ai')).toHaveLength(1))
    expect(api.find('PUT', '/v1/ai')[0]!.body).toEqual({ model: 'gpt-image-1', api_key: 'sk-proj-test-0123456789a1b2' })
    expect(await screen.findByText(/Key \.\.\.a1b2/)).toBeInTheDocument()
    expect(screen.getAllByText(/^Available/)).toHaveLength(4)
  })

  it('shows people who cannot manage it the status only', async () => {
    mockApi({ 'GET /v1/ai': { ...OFF, can_manage: false } })
    nav.set('/integrations/ai')
    renderWithProviders(<AiSettings />, { mode: 'cloud' })
    expect(await screen.findByText(/An admin of this workspace can add/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add key' })).not.toBeInTheDocument()
  })
})
