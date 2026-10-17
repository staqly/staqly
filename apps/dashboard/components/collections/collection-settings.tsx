'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Archive, ArchiveRestore, Trash2 } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { z } from 'zod'

import { ConfirmNameField, confirmNameError } from '@/components/common/confirm-name-field'
import { IdChip } from '@/components/common/id-chip'
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
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { api, type Collection } from '@/lib/api'
import { fieldsOf, focusFirstInvalid, formApiError, setFormApiError } from '@/lib/form-errors'
import { namedToast, toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { ActionsEditor } from './actions-editor'
import { ColorPicker } from './color-picker'

const schema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters'),
  description: z.string().max(2000, 'At most 2000 characters'),
  color: z.string(),
})

type Values = z.infer<typeof schema>

function Section({
  title,
  children,
  description,
  danger,
  archive,
}: {
  title: string
  description?: string
  children?: React.ReactNode
  /** Destructive actions: a red outlined box. */
  danger?: boolean
  /** Archiving: an orange outlined box (attention: reversible, but it hides the collection). */
  archive?: boolean
}) {
  return (
    <section
      className={cn(
        'grid min-w-0 grid-cols-[minmax(0,1fr)] gap-3',
        (danger || archive) && 'rounded-xl border p-4',
        danger && TONES.danger.border,
        archive && TONES.attention.border,
      )}
    >
      <div>
        <h3 className={cn('text-sm font-semibold', danger && TONES.danger.text, archive && TONES.attention.text)}>
          {title}
        </h3>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      {children}
    </section>
  )
}

export function CollectionSettingsSheet({
  collection,
  open,
  onOpenChange,
}: {
  collection: Collection
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b">
          <SheetTitle>Collection settings</SheetTitle>
          <SheetDescription asChild>
            <div>
              <IdChip id={collection.id} label="collection" />
            </div>
          </SheetDescription>
        </SheetHeader>
        {open && <SettingsBody key={collection.id} collection={collection} onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  )
}

function SettingsBody({ collection, onClose }: { collection: Collection; onClose: () => void }) {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const qc = useQueryClient()
  const router = useRouter()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [confirmName, setConfirmName] = useState('')
  const [deleteTried, setDeleteTried] = useState(false)
  const [deleteServerError, setDeleteServerError] = useState<string | undefined>()
  const confirmError = deleteTried ? (confirmNameError(confirmName, collection.name) ?? deleteServerError) : undefined
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: collection.name,
      description: collection.description,
      color: collection.color ?? '',
    },
  })
  const { register, handleSubmit, formState, reset, control, setError } = form

  const onSaved = (c: Collection) => {
    qc.setQueryData(qk.collection(c.id), c)
    void qc.invalidateQueries({ queryKey: qk.collectionsAll })
  }

  const save = useMutation({
    mutationFn: (v: Values) =>
      api.updateCollection(collection.id, {
        name: v.name.trim(),
        description: v.description.trim(),
        color: v.color,
      }),
    onSuccess: (c) => {
      onSaved(c)
      reset({ name: c.name, description: c.description, color: c.color ?? '' })
      namedToast(c.name, 'was updated')
    },
    onError: (e) => setFormApiError(e, setError, fieldsOf('name', 'description', 'color')),
  })

  const archive = useMutation({
    mutationFn: (archived: boolean) => api.updateCollection(collection.id, { archived }),
    onSuccess: (c) => {
      onSaved(c)

      if (c.archived) {
        namedToast(c.name, 'was archived', 'It is hidden from the sidebar and agents cannot add items to it.')
      } else {
        namedToast(c.name, 'was restored', 'Agents can add items to it again.')
      }
    },
    onError: (e) => toastApiError(e),
  })

  const del = useMutation({
    mutationFn: () => api.deleteCollection(collection.id, confirmName),
    onSuccess: () => {
      qc.removeQueries({ queryKey: qk.collection(collection.id) })
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      void qc.invalidateQueries({ queryKey: qk.itemsAll })
      namedToast(collection.name, 'was deleted', 'The collection and its items are gone.')
      setDeleteOpen(false)
      onClose()
      router.push('/collections')
    },
    onError: (e) => setDeleteServerError(formApiError(e, (k) => (k === 'confirm' ? k : null)).confirm),
  })

  // Closing by Esc, outside click or Cancel: the next opening starts with an empty name and no error.
  function openDelete(o: boolean) {
    setDeleteOpen(o)

    if (!o) {
      setConfirmName('')
      setDeleteTried(false)
      setDeleteServerError(undefined)
    }
  }

  return (
    // minmax(0,1fr) tracks: long names, codes and rows shrink to the panel instead of widening it on phones.
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-6 p-4">
      <Section title="General">
        <form
          onSubmit={handleSubmit((v) => save.mutate(v))}
          className="grid grid-cols-[minmax(0,1fr)] gap-4"
          noValidate
        >
          <FieldGroup className="gap-4">
            <Field data-invalid={!!formState.errors.name}>
              <FieldLabel htmlFor="settings-name" required>
                Name
              </FieldLabel>
              <Input id="settings-name" {...register('name')} aria-invalid={!!formState.errors.name} aria-required />
              <FieldError errors={[formState.errors.name]} />
            </Field>
            <Field data-invalid={!!formState.errors.description}>
              <FieldLabel htmlFor="settings-description">Description</FieldLabel>
              <Textarea
                id="settings-description"
                rows={3}
                {...register('description')}
                aria-invalid={!!formState.errors.description}
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
          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={!formState.isDirty || save.isPending}>
              {save.isPending ? 'Saving...' : 'Save changes'}
            </Button>
          </div>
        </form>
      </Section>

      <Separator />
      <Section title="Actions" description="Buttons reviewers and agents can run on items in this collection.">
        <ActionsEditor collection={collection} />
      </Section>

      <Separator />
      {collection.is_default ? (
        <Section
          title="Default collection"
          description="Every workspace keeps one Default collection. You can rename it, but it cannot be archived or deleted."
        />
      ) : (
        <>
          <Section
            archive
            title={collection.archived ? 'Archived' : 'Archive'}
            description={
              collection.archived
                ? 'This collection is hidden from the sidebar and agents cannot add new items to it.'
                : 'Hide the collection and stop new items from being added. Existing items are kept.'
            }
          >
            <div>
              <Button
                variant="archive"
                size="sm"
                disabled={archive.isPending}
                onClick={() => archive.mutate(!collection.archived)}
              >
                {collection.archived ? <ArchiveRestore /> : <Archive />}
                {collection.archived ? 'Unarchive collection' : 'Archive collection'}
              </Button>
            </div>
          </Section>

          <Separator />
          <Section danger title="Danger zone" description="Permanently delete this collection and every item in it.">
            <div>
              <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
                <Trash2 /> Delete collection
              </Button>
            </div>
          </Section>
        </>
      )}

      <Dialog open={deleteOpen} onOpenChange={openDelete}>
        <DialogContent className="sm:max-w-md">
          <form
            className="grid gap-4"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              setDeleteTried(true)
              setDeleteServerError(undefined)

              if (confirmNameError(confirmName, collection.name)) {
                focusFirstInvalid(e.currentTarget)

                return
              }

              del.mutate()
            }}
          >
            <DialogHeader>
              <DialogTitle>Delete “{collection.name}”?</DialogTitle>
              <DialogDescription>
                This permanently deletes the collection and its {collection.item_count} item
                {collection.item_count === 1 ? '' : 's'}. This cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <ConfirmNameField
              id="confirm-delete"
              label="Type the collection name to confirm"
              expected={collection.name}
              value={confirmName}
              onChange={(v) => {
                setConfirmName(v)
                setDeleteServerError(undefined)
              }}
              error={confirmError}
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openDelete(false)}>
                Cancel
              </Button>
              <Button type="submit" variant="destructive" disabled={del.isPending}>
                {del.isPending ? 'Deleting...' : 'Delete permanently'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
