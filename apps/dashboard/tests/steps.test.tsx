import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { stepCounts, Stepper, STEPS } from '@/components/inbox/steps'
import type { Flow } from '@/lib/api'

const states: Flow['states'] = [
  { decision: 'pending', run: 'none', run_action: '', archived: false, items: 3 },
  { decision: 'pending', run: 'completed', run_action: 'regenerate', archived: false, items: 1 },
  { decision: 'approved', run: 'failed', run_action: 'publish', archived: false, items: 2 },
  { decision: 'rejected', run: 'none', run_action: '', archived: true, items: 5 },
]

describe('inbox steps', () => {
  it('counts every item once: a finished regeneration is back in review, archived on the side', () => {
    expect(stepCounts(states)).toMatchObject({ review: 4, failed: 2, archived: 5, rejected: 0, done: 0 })
    expect(STEPS.failed.holds({ decision: 'approved', run: 'failed', archived_at: null })).toBe(true)
    expect(STEPS.review.holds({ decision: 'pending', run: 'none', archived_at: '2026-01-01T00:00:00Z' })).toBe(false)
  })

  it('shows the path with counts; a click picks the step', async () => {
    const onPick = vi.fn<(s: string) => void>()

    render(<Stepper active="review" counts={stepCounts(states)} onPick={onPick} />)
    expect(screen.getByTestId('step-review')).toHaveTextContent('Review4')
    expect(screen.getByTestId('step-review')).toHaveAttribute('aria-selected', 'true')
    await userEvent.setup().click(screen.getByTestId('step-failed'))
    expect(onPick).toHaveBeenCalledWith('failed')
  })
})
