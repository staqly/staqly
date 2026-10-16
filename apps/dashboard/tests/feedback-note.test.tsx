import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { FeedbackCard, FeedbackNote } from '@/components/items/feedback-note'

import { item } from './fixtures'
import { renderWithProviders } from './utils'

describe('feedback note', () => {
  it('shows the feedback in the shared card style', () => {
    render(
      <FeedbackNote item={item({ status: 'rejected', feedback: 'Too dark', decided_at: '2026-01-01T10:00:00Z' })} />,
    )
    const note = screen.getByTestId('feedback-note')

    expect(note).toHaveTextContent('Feedback')
    expect(note).toHaveTextContent('Too dark')
    // One style everywhere: not tinted by the item's status.
    expect(note.className).not.toMatch(/rose|teal|sky/)
  })

  it('renders nothing without feedback', () => {
    const { container } = render(<FeedbackNote item={item({ feedback: '' })} />)

    expect(container).toBeEmptyDOMElement()
  })

  it('in history it starts collapsed with a preview, opens on its header and can then be copied', async () => {
    const user = userEvent.setup()

    render(<FeedbackCard text={'Too dark\nMake the face larger'} />)
    const toggle = screen.getByRole('button', { name: 'Feedback' })

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveTextContent('Too dark')
    expect(screen.queryByRole('button', { name: 'Copy feedback' })).not.toBeInTheDocument()
    await user.click(toggle)
    expect(screen.getByTestId('feedback-body')).toHaveTextContent('Make the face larger')
    expect(screen.getByRole('button', { name: 'Copy feedback' })).toBeInTheDocument()
  })
})

describe('failed note', () => {
  it('shows why an item failed and tidies old durations; Retry lives in the toolbar, not here', async () => {
    const { FailedNote } = await import('@/components/items/failed-note')
    const { unmount } = renderWithProviders(
      <FailedNote item={item({ status: 'failed', metadata: { error: 'No result reported within 1h0m0s' } })} />,
    )

    expect(screen.getByRole('alert')).toHaveTextContent(/^Timed out.*No result reported within 1h: your automation/)
    expect(screen.getByRole('link', { name: 'give it more time' })).toHaveAttribute('href', '/settings')
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
    unmount()

    renderWithProviders(<FailedNote item={item({ status: 'failed', metadata: {} })} />)
    expect(screen.getByRole('alert')).toHaveTextContent('No reason was reported')
  })
})

const ev = (data: Record<string, unknown>) =>
  ({ id: 'evt_1', type: 'item.failed', data, created_at: '2026-01-01T00:00:00Z' }) as never

describe('event titles', () => {
  it('calls a processing timeout "Timed out", other failures "Failed"', async () => {
    const { eventTitle } = await import('@/components/items/event-timeline')

    expect(eventTitle(ev({ timeout: true, error: 'No result reported within 1h' }))).toBe('Timed out')
    expect(eventTitle(ev({ error: 'Instagram API returned 500' }))).toBe('Failed')
  })
})
