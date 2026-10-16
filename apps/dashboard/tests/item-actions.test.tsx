import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ItemActionBar, undoDelay, useRunAction } from '@/components/items/item-actions'
import type { ActionDef, Item } from '@/lib/api'

import { APPROVE, REGENERATE, REJECT, item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

const custom = (name: string, label: string): ActionDef => ({
  name,
  label,
  event: `item.${name}_requested`,
  requires_feedback: false,
  allowed_statuses: ['pending'],
})

/** Actions come from GET /v1/items/{id}/actions when the collection is not in the cached list. */
function routes(target: Item, actions: ActionDef[], over: Record<string, unknown> = {}) {
  return {
    'GET /v1/collections': { data: [] },
    [`GET /v1/items/${target.id}/actions`]: { data: actions },
    ...over,
  }
}

describe('item action bar', () => {
  it('approves with the loaded version and reports back', async () => {
    const itm = item({ version: 3 })
    const api = mockApi(
      routes(itm, [APPROVE, REJECT], {
        'POST /v1/items/itm_1/actions/approve': { ...itm, status: 'approved', version: 4 },
      }),
    )
    const onDone = vi.fn<(item: Item, action: string) => void>()
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} onDone={onDone} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Approve' }))

    await waitFor(() => expect(onDone).toHaveBeenCalledWith(expect.objectContaining({ status: 'approved' }), 'approve'))
    expect(api.find('POST', '/v1/items/itm_1/actions/approve')[0].body).toEqual({ expected_version: 3 })
    expect(await screen.findByText('“Avatar v1” was approved')).toBeInTheDocument()
  })

  it('always asks for feedback before rejecting, and sends it', async () => {
    const itm = item({ version: 2 })
    const api = mockApi(
      routes(itm, [APPROVE, REJECT], {
        'POST /v1/items/itm_1/actions/reject': { ...itm, status: 'rejected', version: 3 },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    // REJECT in the fixture has requires_feedback: false; the UI still requires it.
    await user.click(await screen.findByRole('button', { name: 'Reject' }))

    const dialog = await screen.findByRole('dialog')
    const submit = within(dialog).getByRole('button', { name: 'Reject' })
    const box = within(dialog).getByRole('textbox')

    expect(within(dialog).getByText('Avatar v1')).toBeInTheDocument()
    expect(box).toHaveAttribute('aria-required', 'true')
    expect(within(dialog).getByText('Feedback', { selector: 'label' })).toHaveTextContent('Feedback*')

    // Submitting empty feedback (button or Enter) flags the field instead of sending.
    await user.click(submit)
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Feedback is required for this action')
    expect(box).toHaveAttribute('aria-invalid', 'true')
    expect(box.closest('[data-slot=field]')).toHaveAttribute('data-invalid', 'true')
    await user.type(box, '{Enter}')
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Feedback is required for this action')
    expect(api.find('POST', '/v1/items/itm_1/actions/reject')).toHaveLength(0)

    await user.type(box, '  Make the face larger  ')
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
    await user.click(submit)

    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/reject')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/reject')[0].body).toEqual({
      feedback: 'Make the face larger',
      expected_version: 2,
    })
    expect(await screen.findByText('“Avatar v1” was rejected')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('explains a 409 when the item was decided elsewhere', async () => {
    const itm = item()

    mockApi(
      routes(itm, [APPROVE, REJECT], {
        'POST /v1/items/itm_1/actions/approve': {
          status: 409,
          body: { error: { code: 'conflict', message: 'item is already approved' } },
        },
        'GET /v1/items/itm_1': itm,
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Approve' }))
    expect(await screen.findByText('Already decided — item is already approved')).toBeInTheDocument()
  })

  it('explains a 409 version conflict', async () => {
    const itm = item()

    mockApi(
      routes(itm, [APPROVE, REJECT], {
        'POST /v1/items/itm_1/actions/approve': {
          status: 409,
          body: { error: { code: 'conflict', message: 'item was modified (version 2, expected 1)' } },
        },
        'GET /v1/items/itm_1': itm,
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Approve' }))
    expect(
      await screen.findByText('This item changed since you loaded it. The latest version has been loaded.'),
    ).toBeInTheDocument()
  })

  it('edits title and content without deciding; both are required', async () => {
    const itm = item({ type: 'text', media: null, title: 'Caption', content: 'Hello', version: 7 })
    const api = mockApi(
      routes(itm, [APPROVE, REJECT], { 'PATCH /v1/items/itm_1': { ...itm, title: 'Caption v2', version: 8 } }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('menuitem', { name: /^edit/i }))

    const dialog = await screen.findByRole('dialog', { name: /edit item/i })
    const title = within(dialog).getByLabelText('Title*')
    const content = within(dialog).getByLabelText('Content*')

    expect(title).toHaveAttribute('aria-required', 'true')
    expect(content).toHaveAttribute('aria-required', 'true')
    expect(within(dialog).queryByLabelText(/note/i)).not.toBeInTheDocument()
    await user.clear(title)
    await user.clear(content)
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
    expect(within(dialog).getByText('Title is required')).toBeInTheDocument()
    expect(within(dialog).getByText('Content is required')).toBeInTheDocument()
    expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(0)

    await user.type(title, 'Caption v2')
    await user.type(content, 'Hello world')
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/items/itm_1')[0].body).toEqual({
      title: 'Caption v2',
      content: 'Hello world',
      expected_version: 7,
    })
    expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(0)
  })

  it('sends nothing when nothing changed', async () => {
    const itm = item({ type: 'link', media: null, url: 'https://a.test', content: '' })
    const api = mockApi(routes(itm, [APPROVE]))
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('menuitem', { name: /^edit/i }))

    const dialog = await screen.findByRole('dialog', { name: /edit item/i })

    expect(within(dialog).getByLabelText('URL*')).toHaveValue('https://a.test')
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(0)
  })

  it('rejects invalid JSON edits client-side', async () => {
    const itm = item({ type: 'json', media: null, content: '{"a":1}' })
    const api = mockApi(routes(itm, [APPROVE]))
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('menuitem', { name: /^edit/i }))

    const dialog = await screen.findByRole('dialog', { name: /edit item/i })
    const json = within(dialog).getByLabelText('JSON*')

    expect(json).toHaveValue('{\n  "a": 1\n}')
    await user.clear(json)
    await user.type(json, 'nope')
    await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
    expect(await within(dialog).findByText('Content must be valid JSON')).toBeInTheDocument()
    expect(json).toHaveAttribute('aria-invalid', 'true')
    expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(0)
  })

  it('runs a custom action that requires feedback', async () => {
    const itm = item({ status: 'rejected', version: 4 })
    const api = mockApi(
      routes(itm, [REGENERATE], {
        'POST /v1/items/itm_1/actions/regenerate': { ...itm, status: 'processing' },
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'Regenerate' }))

    const dialog = await screen.findByRole('dialog', { name: 'Regenerate' })

    expect(within(dialog).getByText('This action requires feedback for the agent.')).toBeInTheDocument()
    await user.type(within(dialog).getByRole('textbox'), 'brighter{Enter}')

    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/regenerate')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/regenerate')[0].body).toEqual({
      feedback: 'brighter',
      expected_version: 4,
    })
    expect(await screen.findByText(/done for “Avatar v1”|requested for “Avatar v1”/)).toBeInTheDocument()
  })

  it('keeps custom actions of a pending item in the More menu', async () => {
    const itm = item()
    const api = mockApi(
      routes(
        itm,
        [
          APPROVE,
          custom('upscale', 'Upscale'),
          custom('crop', 'Crop'),
          custom('publish', 'Publish'),
          custom('tag', 'Tag'),
        ],
        {
          'POST /v1/items/itm_1/actions/crop': itm,
        },
      ),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: 'More actions' }))
    expect(screen.queryByRole('button', { name: 'Upscale' })).not.toBeInTheDocument()
    await user.click(await screen.findByRole('menuitem', { name: /crop/i }))
    // Collection actions start the automation: confirmed first.
    expect(api.find('POST', '/v1/items/itm_1/actions/crop')).toHaveLength(0)
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Crop' }))

    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/crop')).toHaveLength(1))
    expect(api.find('POST', '/v1/items/itm_1/actions/crop')[0].body).toEqual({ expected_version: 1 })
  })

  it('a decided item shows its main action; Archive is in More', async () => {
    const itm = item({ status: 'approved' })
    const archive = { ...custom('archive', 'Archive'), set_status: 'archived' as const }
    const publish = { ...custom('publish', 'Publish'), set_status: 'processing' as const }
    const user = userEvent.setup()

    mockApi(routes(itm, [archive, publish]))
    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    expect(await screen.findByRole('button', { name: /publish/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^archive$/i })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    expect(await screen.findByRole('menuitem', { name: /archive/i })).toBeInTheDocument()
  })

  it('shows nothing (no message) when no actions are available', async () => {
    const itm = item({ status: 'approved' })
    const api = mockApi(routes(itm, []))

    renderWithProviders(<ItemActionBar item={itm} />, { mode: 'cloud' })
    await waitFor(() => expect(api.find('GET', '/v1/items/itm_1/actions')).toHaveLength(1))
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.queryByText(/no actions available/i)).not.toBeInTheDocument()
  })

  it('uses big reject/approve buttons plus a menu in the compact layout', async () => {
    const itm = item()
    const api = mockApi(
      routes(itm, [APPROVE, REJECT, custom('upscale', 'Upscale')], {
        'POST /v1/items/itm_1/actions/upscale': itm,
      }),
    )
    const user = userEvent.setup()

    renderWithProviders(<ItemActionBar item={itm} compact />, { mode: 'cloud' })

    const bar = await screen.findByTestId('item-actions-compact')

    expect(within(bar).getByRole('button', { name: 'Reject' })).toBeInTheDocument()
    expect(within(bar).getByRole('button', { name: 'Approve' })).toBeInTheDocument()
    await user.click(within(bar).getByRole('button', { name: 'More actions' }))
    expect(await screen.findByRole('menuitem', { name: /^edit$/i })).toBeInTheDocument()
    await user.click(screen.getByRole('menuitem', { name: /upscale/i }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Upscale' }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/upscale')).toHaveLength(1))
  })

  it('renders nothing in the compact layout without actions', async () => {
    const itm = item({ status: 'approved' })

    mockApi(routes(itm, []))
    renderWithProviders(<ItemActionBar item={itm} compact />, { mode: 'cloud' })
    await waitFor(() => expect(document.querySelector('.animate-pulse')).toBeNull())
    expect(screen.queryByTestId('item-actions-compact')).not.toBeInTheDocument()
  })

  it('another action during the Undo window sends the approval first, then the action on its version', async () => {
    const itm = item({ version: 3 })
    const api = mockApi({
      'GET /v1/collections': { data: [] },
      'POST /v1/items/itm_1/actions/approve': { ...itm, status: 'approved', version: 4 },
      'POST /v1/items/itm_1/actions/publish': { ...itm, status: 'processing', version: 5 },
    })
    const user = userEvent.setup()

    function Harness() {
      const { mutate } = useRunAction({ silent: true })

      return (
        <>
          <button onClick={() => mutate({ item: itm, action: 'approve', body: { expected_version: 3 } })}>a</button>
          <button onClick={() => mutate({ item: itm, action: 'publish', body: { expected_version: 3 } })}>p</button>
        </>
      )
    }

    undoDelay.ms = 60_000

    try {
      renderWithProviders(<Harness />, { mode: 'cloud' })
      await user.click(screen.getByRole('button', { name: 'a' }))
      expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(0)
      await user.click(screen.getByRole('button', { name: 'p' }))
      await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/actions/publish')).toHaveLength(1))
      expect(api.find('POST', '/v1/items/itm_1/actions/approve')).toHaveLength(1)
      expect(api.find('POST', '/v1/items/itm_1/actions/publish')[0].body).toEqual({ expected_version: 4 })
    } finally {
      undoDelay.ms = 0
    }
  })
})
