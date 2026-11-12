import { QueryClient, QueryClientProvider, useMutation } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { Sheet, SheetContent, SheetTitle, useSheetSaved } from '@/components/ui/sheet'

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

function SavingFields({ own }: { own: boolean }) {
  const saved = useSheetSaved()
  const save = useMutation({ mutationFn: async () => 'ok', onSuccess: () => own && saved() })

  return (
    <>
      <input aria-label="Name" defaultValue="Daily tips" />
      <button type="button" onClick={() => save.mutate()}>
        {own ? 'Save changes' : 'Upload'}
      </button>
    </>
  )
}

/** `own`: the mutation is this form's save; otherwise something else that succeeds meanwhile (an upload). */
function SavingForm({ onOpenChange, own = true }: { onOpenChange: (o: boolean) => void; own?: boolean }) {
  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent aria-describedby={undefined}>
        <SheetTitle>Edit</SheetTitle>
        <SavingFields own={own} />
      </SheetContent>
    </Sheet>
  )
}

function Attachments({ onOpenChange }: { onOpenChange: (o: boolean) => void }) {
  const [files, setFiles] = useState<string[]>([])

  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent aria-describedby={undefined}>
        <SheetTitle>Edit</SheetTitle>
        <form data-form-value={files.join(' ')}>
          <button type="button" onClick={() => setFiles((f) => [...f, 'med_1'])}>
            Attach
          </button>
        </form>
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

  it("a mutation that is not this form's save (an upload, a save elsewhere) leaves the changes unsaved", async () => {
    const onOpenChange = vi.fn<(o: boolean) => void>()
    const user = userEvent.setup()

    render(
      <QueryClientProvider client={new QueryClient()}>
        <SavingForm onOpenChange={onOpenChange} own={false} />
      </QueryClientProvider>,
    )
    await user.type(screen.getByLabelText('Name'), ' and more')
    await user.click(screen.getByRole('button', { name: 'Upload' }))
    await new Promise((r) => setTimeout(r, 10))
    await user.keyboard('{Escape}')
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(await screen.findByRole('alertdialog', { name: 'Discard your changes?' })).toBeInTheDocument()
  })

  it('state outside the fields (attachments, data-form-value) counts as a change', async () => {
    const onOpenChange = vi.fn<(o: boolean) => void>()
    const user = userEvent.setup()

    render(<Attachments onOpenChange={onOpenChange} />)
    await user.click(screen.getByRole('button', { name: 'Attach' }))
    await user.keyboard('{Escape}')
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(await screen.findByRole('alertdialog', { name: 'Discard your changes?' })).toBeInTheDocument()
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
