import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AddItemDialog } from '@/components/collections/add-item-dialog'
import type { Item } from '@/lib/api'

import { item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

const PATH = '/v1/collections/col_1/items'

function setup(reply: unknown = { status: 201, body: item({ title: 'Created' }) }) {
  const api = mockApi({ [`POST ${PATH}`]: reply })
  const onOpenChange = vi.fn<(open: boolean) => void>()
  const onCreated = vi.fn<(item: Item) => void>()
  const user = userEvent.setup()

  renderWithProviders(<AddItemDialog collectionId="col_1" open onOpenChange={onOpenChange} onCreated={onCreated} />, {
    mode: 'cloud',
  })

  return { api, onOpenChange, onCreated, user }
}

describe('add item dialog', () => {
  it('requires a file, then uploads it as multipart with the item fields', async () => {
    const { api, onOpenChange, onCreated, user } = setup()

    expect(screen.getByText('File', { selector: 'label' })).toHaveTextContent('File*')
    expect(screen.getByLabelText('File')).toHaveAttribute('aria-required', 'true')
    await user.click(screen.getByRole('button', { name: 'Add item' }))
    expect(await screen.findByText('Choose a file to upload')).toHaveAttribute('data-slot', 'field-error')
    expect(screen.getByLabelText('File')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('File').closest('[data-slot=field]')).toHaveAttribute('data-invalid', 'true')
    expect(screen.getByText('Title is required')).toBeInTheDocument()
    expect(screen.getByLabelText('Title*')).toHaveAttribute('aria-required', 'true')
    expect(api.find('POST', PATH)).toHaveLength(0)

    const file = new File(['png'], 'avatar.png', { type: 'image/png' })

    await user.upload(screen.getByTestId('file-input'), file)
    expect(screen.getByText('avatar.png')).toBeInTheDocument()
    await user.type(screen.getByLabelText(/^title/i), ' Hero ')
    await user.type(screen.getByLabelText(/^metadata/i), '{{"seed":7}')
    await user.click(screen.getByRole('button', { name: 'Add item' }))

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ title: 'Created' })))
    expect(onOpenChange).toHaveBeenCalledWith(false)

    const form = api.find('POST', PATH)[0].body as FormData

    expect(form).toBeInstanceOf(FormData)
    expect(JSON.parse(form.get('item') as string)).toEqual({
      status: 'pending',
      source: 'dashboard',
      title: 'Hero',
      metadata: { seed: 7 },
    })
    expect((form.get('file') as File).name).toBe('avatar.png')
    expect(await screen.findByText('“Created” was added')).toBeInTheDocument()
  })

  it('creates a text item as JSON with the chosen status', async () => {
    const { api, user } = setup({ status: 201, body: item({ title: 'Note', status: 'draft', decision: null }) })

    await user.click(screen.getByRole('tab', { name: /text/i }))
    await user.click(screen.getByRole('button', { name: 'Add item' }))
    expect(await screen.findByText('Write some text')).toBeInTheDocument()
    expect(api.find('POST', PATH)).toHaveLength(0)

    expect(screen.getByLabelText('Text*')).toHaveAttribute('aria-invalid', 'true')
    await user.type(screen.getByLabelText('Title*'), 'Note')
    await user.type(screen.getByLabelText('Text*'), 'First line')
    // Fixed: the error goes away before the next submit.
    expect(screen.queryByText('Write some text')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Text*')).toHaveAttribute('aria-invalid', 'false')
    await user.click(screen.getByRole('combobox', { name: 'Initial status' }))
    await user.click(await screen.findByRole('option', { name: 'Draft' }))
    await user.click(screen.getByRole('button', { name: 'Add item' }))

    await waitFor(() => expect(api.find('POST', PATH)).toHaveLength(1))
    expect(api.find('POST', PATH)[0].body).toEqual({
      status: 'draft',
      source: 'dashboard',
      title: 'Note',
      type: 'text',
      content: 'First line',
    })
    // A draft is not waiting for review.
    expect(await screen.findByText('It is a draft: not in the inbox yet.')).toBeInTheDocument()
  })

  it('validates link URLs and sends the optional description', async () => {
    const { api, user } = setup()

    await user.click(screen.getByRole('tab', { name: /link/i }))
    await user.type(screen.getByLabelText('URL*'), 'example.com/post')
    await user.click(screen.getByRole('button', { name: 'Add item' }))
    expect(await screen.findByText('Enter an absolute http(s) URL')).toBeInTheDocument()
    expect(screen.getByLabelText('URL*')).toHaveAttribute('aria-invalid', 'true')
    expect(api.find('POST', PATH)).toHaveLength(0)

    await user.clear(screen.getByLabelText('URL*'))
    await user.type(screen.getByLabelText('URL*'), ' https://example.com/post ')
    await user.type(screen.getByLabelText('Title*'), 'Post')
    await user.type(screen.getByLabelText('Description (optional)'), 'Worth reading')
    await user.click(screen.getByRole('button', { name: 'Add item' }))

    await waitFor(() => expect(api.find('POST', PATH)).toHaveLength(1))
    expect(api.find('POST', PATH)[0].body).toEqual({
      status: 'pending',
      source: 'dashboard',
      title: 'Post',
      type: 'link',
      url: 'https://example.com/post',
      content: 'Worth reading',
    })
  })

  it('parses JSON content and rejects invalid JSON or metadata', async () => {
    const { api, user } = setup()

    await user.click(screen.getByRole('tab', { name: /json/i }))

    const json = screen.getByLabelText('JSON*')

    await user.type(screen.getByLabelText('Title*'), 'Caption')

    await user.type(json, '{{"caption":')
    await user.type(screen.getByLabelText(/^metadata/i), '[[1]')
    await user.click(screen.getByRole('button', { name: 'Add item' }))
    expect(await screen.findByText('Not valid JSON')).toBeInTheDocument()
    expect(screen.getByText('Metadata must be a JSON object')).toBeInTheDocument()
    expect(api.find('POST', PATH)).toHaveLength(0)

    await user.type(json, '"hi"}')
    await user.clear(screen.getByLabelText(/^metadata/i))
    await user.type(screen.getByLabelText(/^metadata/i), 'nope')
    await user.click(screen.getByRole('button', { name: 'Add item' }))
    expect(await screen.findByText('Metadata is not valid JSON')).toBeInTheDocument()
    expect(screen.queryByText('Not valid JSON')).not.toBeInTheDocument()

    await user.clear(screen.getByLabelText(/^metadata/i))
    await user.click(screen.getByRole('button', { name: 'Add item' }))
    await waitFor(() => expect(api.find('POST', PATH)).toHaveLength(1))
    expect(api.find('POST', PATH)[0].body).toEqual({
      status: 'pending',
      source: 'dashboard',
      title: 'Caption',
      type: 'json',
      content: { caption: 'hi' },
    })
  })

  it('shows server field errors and stays open', async () => {
    const { onOpenChange, user } = setup({
      status: 422,
      body: { error: { code: 'validation_failed', message: 'validation failed', fields: { title: 'too long' } } },
    })

    await user.click(screen.getByRole('tab', { name: /text/i }))
    await user.type(screen.getByLabelText('Title*'), 'Long')
    await user.type(screen.getByLabelText('Text*'), 'x')
    await user.click(screen.getByRole('button', { name: 'Add item' }))
    expect(await screen.findByText('Too long')).toHaveAttribute('data-slot', 'field-error')
    expect(screen.getByLabelText(/^title/i)).toHaveAttribute('aria-invalid', 'true')
    expect(onOpenChange).not.toHaveBeenCalled()
    // A field error is not repeated as a toast, and it clears once the field changes.
    expect(screen.queryByText(/validation failed/)).not.toBeInTheDocument()
    await user.type(screen.getByLabelText(/^title/i), 'y')
    expect(screen.queryByText('Too long')).not.toBeInTheDocument()
    expect(screen.getByLabelText(/^title/i)).toHaveAttribute('aria-invalid', 'false')
  })
})
