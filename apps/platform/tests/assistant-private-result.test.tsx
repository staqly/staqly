import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { AssistantPrivateResult } from '@/components/assistant/assistant-private-result'

import { renderWithProviders } from './utils'

describe('assistant private tool results', () => {
  it.each(['create_schedule', 'update_schedule'])('lets the person reveal and copy a %s secret', async (name) => {
    const user = userEvent.setup()
    const value = 'generated-endpoint-secret'

    renderWithProviders(<AssistantPrivateResult name={name} result={JSON.stringify({ endpoint_secret: value })} />)

    expect(screen.getByTestId('secret-Endpoint secret')).not.toHaveTextContent(value)
    expect(screen.getByText(/withheld from the AI model/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Reveal Endpoint secret' }))
    expect(screen.getByTestId('secret-Endpoint secret')).toHaveTextContent(value)
    await user.click(screen.getByRole('button', { name: 'Copy Endpoint secret' }))
    expect(await navigator.clipboard.readText()).toBe(value)
    await user.click(screen.getByRole('button', { name: 'Hide Endpoint secret' }))
    expect(screen.getByTestId('secret-Endpoint secret')).not.toHaveTextContent(value)
  })

  it.each(['create_channel_link', 'create_preview_link'])('copies the complete %s bearer link', async (name) => {
    const user = userEvent.setup()
    const value = 'https://app.staqly.ai/preview#token=private-token'

    renderWithProviders(
      <AssistantPrivateResult name={name} result={JSON.stringify({ url: value, token: 'private-token' })} />,
    )

    expect(screen.getByTestId('secret-Private link')).not.toHaveTextContent(value)
    await user.click(screen.getByRole('button', { name: 'Copy Private link' }))
    expect(await navigator.clipboard.readText()).toBe(value)
  })

  it.each([
    ['update_schedule', '{"endpoint_secret":"****abcd"}'],
    ['create_schedule', '{"id":"sch_1"}'],
    ['create_preview_link', 'Error: not allowed'],
    ['create_preview_link', 'null'],
    ['list_posts', '{"url":"https://example.com"}'],
  ])('does not offer a private value for %s result %s', (name, result) => {
    renderWithProviders(<AssistantPrivateResult name={name} result={result} />)

    expect(screen.queryByRole('button', { name: /copy/i })).not.toBeInTheDocument()
  })
})
