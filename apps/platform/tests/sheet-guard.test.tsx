import { QueryClient, QueryClientProvider, useMutation } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'

function Form({ onOpenChange }: { onOpenChange: (o: boolean) => void }) {
  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent aria-describedby={undefined}>
        <SheetTitle>Edit</SheetTitle>
        <input aria-label="Name" defaultValue="Daily tips" />
      </SheetContent>
    </Sheet>
  )
}

function SavingForm({ onOpenChange }: { onOpenChange: (o: boolean) => void }) {
  const save = useMutation({ mutationFn: async () => 'ok' })

  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent aria-describedby={undefined}>
        <SheetTitle>Edit</SheetTitle>
        <input aria-label="Name" defaultValue="Daily tips" />
        <button type="button" onClick={() => save.mutate()}>
          Save changes
        </button>
      </SheetContent>
    </Sheet>
  )
}

describe('sheet unsaved changes', () => {
  it('a sheet that stays open after a save closes without asking', async () => {
    const onOpenChange = vi.fn<(o: boolean) => void>()
    const user = userEvent.setup()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <SavingForm onOpenChange={onOpenChange} />
      </QueryClientProvider>,
    )
    await user.type(screen.getByLabelText('Name'), ' and more')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await new Promise((r) => setTimeout(r, 10))
    await user.keyboard('{Escape}')
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('closes at once when nothing changed', async () => {
    const onOpenChange = vi.fn<(o: boolean) => void>()
    const user = userEvent.setup()

    render(<Form onOpenChange={onOpenChange} />)
    await user.click(screen.getByLabelText('Name'))
    await user.keyboard('{Escape}')
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('asks before dropping a changed form, keeps it on Cancel and closes on Discard', async () => {
    const onOpenChange = vi.fn<(o: boolean) => void>()
    const user = userEvent.setup()

    render(<Form onOpenChange={onOpenChange} />)
    await user.type(screen.getByLabelText('Name'), ' and more')
    await user.click(screen.getByRole('button', { name: 'Close' }))
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(await screen.findByRole('alertdialog', { name: 'Discard your changes?' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onOpenChange).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Close' }))
    await user.click(await screen.findByRole('button', { name: 'Discard changes' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
