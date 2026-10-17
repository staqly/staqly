import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet'

// Most of our dialogs open from state (an inbox row, ⌘K, a menu item), not from a Radix Trigger. Radix then had no
// element to return focus to and dropped it on <body>.
function Harness({ kind }: { kind: 'dialog' | 'sheet' }) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <button type="button">Before</button>
      <button type="button" onClick={() => setOpen(true)}>
        Open it
      </button>
      {kind === 'dialog' ? (
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogContent>
            <DialogTitle>Dialog</DialogTitle>
            <DialogDescription>Body</DialogDescription>
          </DialogContent>
        </Dialog>
      ) : (
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetContent>
            <SheetTitle>Sheet</SheetTitle>
            <SheetDescription>Body</SheetDescription>
          </SheetContent>
        </Sheet>
      )}
    </>
  )
}

describe('dialogs without a Trigger', () => {
  it.each(['dialog', 'sheet'] as const)('%s: focus goes back to what opened it', async (kind) => {
    const user = userEvent.setup()

    render(<Harness kind={kind} />)
    await user.click(screen.getByRole('button', { name: 'Open it' }))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Open it' })).toHaveFocus()
  })
})
