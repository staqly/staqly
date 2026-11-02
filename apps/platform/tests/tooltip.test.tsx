import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

function setup() {
  render(
    <TooltipProvider>
      <button type="button">Before</button>
      <div role="dialog">
        <button type="button">Close</button>
      </div>
      <Tooltip>
        <TooltipTrigger>Invite</TooltipTrigger>
        <TooltipContent>Invite someone</TooltipContent>
      </Tooltip>
    </TooltipProvider>,
  )

  return screen.getByRole('button', { name: 'Invite' })
}

describe('TooltipTrigger focus', () => {
  it('opens when focus is moved to it', () => {
    const trigger = setup()

    fireEvent.focus(trigger, { relatedTarget: screen.getByRole('button', { name: 'Before' }) })
    expect(screen.queryByRole('tooltip')).not.toBeNull()
  })

  it('stays closed when a dialog hands focus back, or focus comes from nowhere', () => {
    const trigger = setup()

    fireEvent.focus(trigger, { relatedTarget: screen.getByRole('button', { name: 'Close' }) })
    expect(screen.queryByRole('tooltip')).toBeNull()
    fireEvent.focus(trigger, { relatedTarget: null })
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('opens when focus moves within the dialog it sits in', () => {
    render(
      <TooltipProvider>
        <div role="dialog">
          <button type="button">Other</button>
          <Tooltip>
            <TooltipTrigger>Inside</TooltipTrigger>
            <TooltipContent>Inside tip</TooltipContent>
          </Tooltip>
        </div>
      </TooltipProvider>,
    )
    fireEvent.focus(screen.getByRole('button', { name: 'Inside' }), {
      relatedTarget: screen.getByRole('button', { name: 'Other' }),
    })
    expect(screen.queryByRole('tooltip')).not.toBeNull()
  })
})
