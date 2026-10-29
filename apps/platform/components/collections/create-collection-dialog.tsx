'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError } from '@/lib/api'
import { fieldsOf, setFormApiError } from '@/lib/form-errors'
import { namedToast } from '@/lib/notify'
import { useSession } from '@/lib/queries'
import { qk } from '@/lib/queries'
import { zodResolver } from '@/lib/zod'

import { ColorPicker } from './color-picker'
import { TimeoutSelect } from './timeout-select'

const schema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters'),
  description: z.string().max(180, 'At most 180 characters'),
  color: z.string(),
  processing_timeout_seconds: z.string(),
})

type Values = z.infer<typeof schema>

/**
 * New collection, in a drawer laid out like Collection settings: the header stays put, the fields scroll,
 * Cancel and Create sit in the footer. (The name is kept for the places that open it.)
 */
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
  const serverTimeout = useSession().data?.defaults?.processing_timeout_seconds ?? 3600
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: '',
      description: '',
      color: '',
      processing_timeout_seconds: '',
    },
  })
  const { register, handleSubmit, formState, reset, setError, control } = form
  const description = useWatch({ control, name: 'description' })

  const create = useMutation({
    mutationFn: (v: Values) =>
      api.createCollection({
        name: v.name.trim(),
        description: v.description.trim(),
        color: v.color,
        ...(v.processing_timeout_seconds ? { processing_timeout_seconds: Number(v.processing_timeout_seconds) } : {}),
      }),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      namedToast(c.name, 'was created', 'Posts and schedules can live here now.')
      reset()
      onOpenChange(false)
      router.push(`/posts?collection=${c.id}`)
    },
    onError: (err) => {
      // The one conflict is a name in use: say so under Name instead of in a toast.
      if (err instanceof ApiError && err.status === 409 && /name/i.test(err.message)) {
        setError(
          'name',
          { type: 'server', message: 'A collection with this name already exists' },
          { shouldFocus: true },
        )

        return
      }

      setFormApiError(err, setError, (k) =>
        k === 'actions' ? 'root.actions' : fieldsOf('name', 'description', 'color')(k),
      )
    },
  })

  // The drawer's own close (Esc, outside, X) and Cancel both start the next opening empty.
  function handleOpenChange(o: boolean) {
    if (!o) {
      reset()
    }

    onOpenChange(o)
  }

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle>New collection</SheetTitle>
          <SheetDescription>
            A collection groups posts (a brand, a client, a campaign) and holds their publishing schedules. Agents refer
            to it by name.
          </SheetDescription>
        </SheetHeader>
        <form
          onSubmit={handleSubmit((v) => create.mutate(v))}
          className="flex min-h-0 flex-1 flex-col"
          noValidate
          data-testid="create-collection-form"
        >
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4">
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
                  placeholder="What gets posted here, and where?"
                  aria-invalid={!!formState.errors.description}
                  {...register('description')}
                  value={description}
                  maxLength={180}
                  className="resize-none field-sizing-fixed"
                />
                <FieldError errors={[formState.errors.description]} />
              </Field>
              <Field>
                <FieldLabel htmlFor="collection-timeout">Fail publishing if nothing is reported within</FieldLabel>
                <Controller
                  control={control}
                  name="processing_timeout_seconds"
                  render={({ field }) => (
                    <TimeoutSelect
                      id="collection-timeout"
                      value={field.value}
                      onChange={field.onChange}
                      fallback={serverTimeout}
                    />
                  )}
                />
                <FieldDescription className="text-xs">
                  When a post goes through your own automation, it has this long to report the result. Then that channel
                  fails, so nothing waits forever.
                </FieldDescription>
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
          </div>
          <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? 'Creating...' : 'Create collection'}
            </Button>
          </div>
        </form>
      </SheetContent>
    </Sheet>
  )
}
