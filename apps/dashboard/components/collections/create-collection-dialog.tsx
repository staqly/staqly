'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { Controller, useForm } from 'react-hook-form'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { fieldsOf, setFormApiError } from '@/lib/form-errors'
import { namedToast } from '@/lib/notify'
import { qk } from '@/lib/queries'

import { ColorPicker } from './color-picker'

const schema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters'),
  description: z.string().max(2000, 'At most 2000 characters'),
  color: z.string(),
})

type Values = z.infer<typeof schema>

export function CreateCollectionDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const qc = useQueryClient()
  const router = useRouter()
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { name: '', description: '', color: '' },
  })
  const { register, handleSubmit, formState, reset, setError, control } = form

  const create = useMutation({
    mutationFn: (v: Values) =>
      api.createCollection({
        name: v.name.trim(),
        description: v.description.trim(),
        color: v.color,
        default_view: 'table',
      }),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      namedToast(c.name, 'was created', 'Agents can add items to it by its ID or name.')
      reset()
      onOpenChange(false)
      router.push(`/collections/${c.id}`)
    },
    onError: (err) => setFormApiError(err, setError, fieldsOf('name', 'description', 'color')),
  })

  // The dialog's own close (Esc, outside, X) and Cancel both start the next opening empty.
  function handleOpenChange(o: boolean) {
    if (!o) {
      reset()
    }

    onOpenChange(o)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit((v) => create.mutate(v))} className="grid gap-4" noValidate>
          <DialogHeader>
            <DialogTitle>New collection</DialogTitle>
            <DialogDescription>
              Agents add items to collections by ID or by exact name — pick something they can reference.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup className="gap-4">
            <Field data-invalid={!!formState.errors.name}>
              <FieldLabel htmlFor="collection-name" required>
                Name
              </FieldLabel>
              <Input
                id="collection-name"
                placeholder="Instagram Avatar"
                aria-invalid={!!formState.errors.name}
                aria-required
                {...register('name')}
              />
              <FieldError errors={[formState.errors.name]} />
            </Field>
            <Field data-invalid={!!formState.errors.description}>
              <FieldLabel htmlFor="collection-description">Description</FieldLabel>
              <Textarea
                id="collection-description"
                rows={3}
                placeholder="What lands here and who reviews it?"
                aria-invalid={!!formState.errors.description}
                {...register('description')}
              />
              <FieldError errors={[formState.errors.description]} />
            </Field>
            <Field data-invalid={!!formState.errors.color}>
              <FieldLabel>Colour</FieldLabel>
              <Controller
                control={control}
                name="color"
                render={({ field }) => <ColorPicker value={field.value} onChange={field.onChange} />}
              />
              <FieldError errors={[formState.errors.color]} />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? 'Creating...' : 'Create collection'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
