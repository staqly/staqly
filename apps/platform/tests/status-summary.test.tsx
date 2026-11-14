import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { StatusSummary } from '@/components/posts/status-counts'

describe('status summary', () => {
  it('shows each status as a filter toggle, Publishing only while something goes out, failed in red', async () => {
    const onSelect = vi.fn<(status: string | undefined) => void>()
    const { rerender } = render(<StatusSummary counts={{ scheduled: 3, failed: 1 }} onSelect={onSelect} />)

    expect(screen.queryByTestId('status-count-publishing')).toBeNull()
    expect(screen.getByTestId('status-count-draft')).toBeDisabled()
    expect(screen.getByTestId('status-count-failed')).toHaveTextContent('Retry or reschedule')
    await userEvent.click(screen.getByTestId('status-count-failed'))
    expect(onSelect).toHaveBeenLastCalledWith('failed')

    // Pressed again, the filter comes off.
    rerender(<StatusSummary counts={{ scheduled: 3, failed: 1, publishing: 1 }} active="failed" onSelect={onSelect} />)
    expect(screen.getByTestId('status-count-publishing')).toBeInTheDocument()
    expect(screen.getByTestId('status-count-failed')).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(screen.getByTestId('status-count-failed'))
    expect(onSelect).toHaveBeenLastCalledWith(undefined)
  })
})
