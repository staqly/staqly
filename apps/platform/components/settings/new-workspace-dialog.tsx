'use client'

import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api, errorMessage } from '@/lib/api'
import { useBilling } from '@/lib/queries'
import { browserTimeZone } from '@/lib/time-zone'

/**
 * The account owner adds a workspace; it opens right away. With billing it has no plan yet, so it opens on the
 * welcome, whose last step is the plan (Stripe Checkout); without billing on the schedule. A modal, not a drawer: one field.
 */
export function NewWorkspaceDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const paid = !!useBilling().data?.enabled
  const create = useMutation({
    mutationFn: async () => {
      const w = await api.createAccountWorkspace(name.trim(), browserTimeZone())

      await api.switchWorkspace(w.id)
    },
    onSuccess: () => window.location.assign(paid ? '/welcome' : '/schedule'),
    onError: (e) => setError(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New workspace</DialogTitle>
          <DialogDescription>
            {paid
              ? 'It starts empty and you are its owner. Next you set it up and choose its plan; billing is per workspace.'
              : 'It starts empty, with a free 7-day Starter trial. You are its owner.'}
          </DialogDescription>
        </DialogHeader>
        <form
          noValidate
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()

            if (!name.trim()) {
              setError('Give the workspace a name, such as Client work.')

              return
            }

            create.mutate()
          }}
        >
          <Field data-invalid={!!error}>
            <FieldLabel htmlFor="workspace-new-name" required>
              Name
            </FieldLabel>
            <Input
              id="workspace-new-name"
              maxLength={100}
              placeholder="Client work"
              value={name}
              aria-invalid={!!error}
              onChange={(e) => setName(e.target.value)}
            />
            <FieldDescription>You can rename it later in Settings.</FieldDescription>
            <FieldError>{error}</FieldError>
          </Field>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? 'Creating...' : paid ? 'Create and choose a plan' : 'Create workspace'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
