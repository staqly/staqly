'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Archive, ArchiveRestore, Trash2 } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { toast } from 'sonner'
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
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api, type Collection } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'

import { ActionsEditor } from './actions-editor'
import { VIEW_OPTIONS } from './create-collection-dialog'

const schema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters'),
  description: z.string().max(2000, 'At most 2000 characters'),
  default_view: z.enum(['gallery', 'table', 'list']),
})

type Values = z.infer<typeof schema>

function Section({ title, children, description }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <section className="grid gap-3">
      <div>
        <h3 className="text-sm font-semibold">{title}</h3>
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
          <SheetDescription className="font-mono text-xs">{collection.id}</SheetDescription>
        </SheetHeader>
        {open && <SettingsBody key={collection.id} collection={collection} onClose={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  )
}

function SettingsBody({ collection, onClose }: { collection: Collection; onClose: () => void }) {
  const qc = useQueryClient()
  const router = useRouter()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [confirmName, setConfirmName] = useState('')
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      name: collection.name,
      description: collection.description,
      default_view: collection.default_view,
    },
  })
  const { register, handleSubmit, formState, control, reset } = form

  const onSaved = (c: Collection) => {
    qc.setQueryData(qk.collection(c.id), c)
    void qc.invalidateQueries({ queryKey: qk.collectionsAll })
  }

  const save = useMutation({
    mutationFn: (v: Values) =>
      api.updateCollection(collection.id, {
        name: v.name.trim(),
        description: v.description.trim(),
        default_view: v.default_view,
      }),
    onSuccess: (c) => {
      onSaved(c)
      reset({ name: c.name, description: c.description, default_view: c.default_view })
      toast.success('Collection updated')
    },
    onError: (e) => toastApiError(e),
  })

  const archive = useMutation({
    mutationFn: (archived: boolean) => api.updateCollection(collection.id, { archived }),
    onSuccess: (c) => {
      onSaved(c)
      toast.success(c.archived ? 'Collection archived' : 'Collection restored')
    },
    onError: (e) => toastApiError(e),
  })

  const del = useMutation({
    mutationFn: () => api.deleteCollection(collection.id, confirmName),
    onSuccess: () => {
      qc.removeQueries({ queryKey: qk.collection(collection.id) })
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      void qc.invalidateQueries({ queryKey: qk.itemsAll })
      toast.success(`Deleted “${collection.name}”`)
      setDeleteOpen(false)
      onClose()
      router.push('/collections')
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <div className="grid gap-6 p-4">
      <Section title="General">
        <form onSubmit={handleSubmit((v) => save.mutate(v))} className="grid gap-4" noValidate>
          <FieldGroup className="gap-4">
            <Field data-invalid={!!formState.errors.name}>
              <FieldLabel htmlFor="settings-name">Name</FieldLabel>
              <Input id="settings-name" {...register('name')} aria-invalid={!!formState.errors.name} />
              <FieldError errors={[formState.errors.name]} />
            </Field>
            <Field data-invalid={!!formState.errors.description}>
              <FieldLabel htmlFor="settings-description">Description</FieldLabel>
              <Textarea id="settings-description" rows={3} {...register('description')} />
              <FieldError errors={[formState.errors.description]} />
            </Field>
            <Field>
              <FieldLabel>Default view</FieldLabel>
              <Controller
                control={control}
                name="default_view"
                render={({ field }) => (
                  <ToggleGroup
                    type="single"
                    variant="outline"
                    value={field.value}
                    onValueChange={(v) => v && field.onChange(v)}
                    className="w-full"
                  >
                    {VIEW_OPTIONS.map(({ value, label, Icon }) => (
                      <ToggleGroupItem key={value} value={value} className="flex-1">
                        <Icon /> {label}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                )}
              />
            </Field>
          </FieldGroup>
          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={!formState.isDirty || save.isPending}>
              {save.isPending ? 'Saving…' : 'Save changes'}
            </Button>
          </div>
        </form>
      </Section>

      <Separator />
      <Section title="Actions" description="Buttons reviewers and agents can run on items in this collection.">
        <ActionsEditor collection={collection} />
      </Section>

      <Separator />
      <Section
        title={collection.archived ? 'Archived' : 'Archive'}
        description={
          collection.archived
            ? 'This collection is hidden from the sidebar and agents cannot add new items to it.'
            : 'Hide the collection and stop new items from being added. Existing items are kept.'
        }
      >
        <div>
          <Button
            variant="outline"
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
      <Section title="Danger zone" description="Permanently delete this collection and every item in it.">
        <div>
          <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
            <Trash2 /> Delete collection
          </Button>
        </div>
      </Section>

      <Dialog
        open={deleteOpen}
        onOpenChange={(o) => {
          setDeleteOpen(o)

          if (!o) {
            setConfirmName('')
          }
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Delete “{collection.name}”?</DialogTitle>
            <DialogDescription>
              This permanently deletes the collection and its {collection.item_count} item
              {collection.item_count === 1 ? '' : 's'}. This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="confirm-delete">
              Type <span className="font-mono font-semibold">{collection.name}</span> to confirm
            </Label>
            <Input
              id="confirm-delete"
              autoComplete="off"
              value={confirmName}
              onChange={(e) => setConfirmName(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={confirmName !== collection.name || del.isPending}
              onClick={() => del.mutate()}
            >
              {del.isPending ? 'Deleting…' : 'Delete permanently'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
