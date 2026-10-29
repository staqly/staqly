'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Archive, ArchiveRestore, ChevronDown, Trash2 } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { z } from 'zod'

import { ConfirmNameField, confirmNameError } from '@/components/common/confirm-name-field'
import { IdChip } from '@/components/common/id-chip'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, type Collection } from '@/lib/api'
import { fieldsOf, focusFirstInvalid, formApiError, setFormApiError } from '@/lib/form-errors'
import { namedToast, toastApiError } from '@/lib/notify'
import { qk, refetchPostListsFromStart, useCollections, useSession } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'
import { zodResolver } from '@/lib/zod'

import { ColorPicker } from './color-picker'
import { SchedulesSection } from './schedules-section'
import { TimeoutSelect, timeoutOf } from './timeout-select'

const schema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters'),
  description: z.string().max(180, 'At most 180 characters'),
  color: z.string(),
  processing_timeout_seconds: z.string(),
})

type Values = z.infer<typeof schema>

/** A section that folds: the header is the switch, the body is hidden until opened (closed by default). */
function Foldable({
  title,
  description,
  children,
  testId,
  danger,
  defaultOpen = false,
}: {
  title: string
  description?: string
  children: React.ReactNode
  testId?: string
  danger?: boolean
  defaultOpen?: boolean
}) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <Collapsible open={open} onOpenChange={setOpen} data-testid={testId} className="grid min-w-0 gap-3">
      <CollapsibleTrigger asChild>
        <button
          type="button"
          className="group/fold flex w-full items-start justify-between gap-3 rounded-lg text-left outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background pointer-coarse:min-h-11"
        >
          <span className="grid gap-0.5">
            <span className={cn('text-sm font-semibold', danger && TONES.danger.text)}>{title}</span>
            {description && <span className="text-xs text-muted-foreground">{description}</span>}
          </span>
          <ChevronDown
            className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]/fold:rotate-180"
            aria-hidden
          />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4">{children}</CollapsibleContent>
    </Collapsible>
  )
}

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
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
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

/** The Default collection's current name (it can be renamed): where a deleted collection's posts go. */
const defaultNameOf = (cols: Collection[]) => cols.find((c) => c.is_default)?.name ?? 'Default'

/** What deleting a collection does, point by point, in the confirmation. */
function deleteConsequences(n: number, defaultName: string) {
  const posts = n === 1 ? 'Its post moves' : `Its ${n} posts move`

  return [
    'The collection and its schedules are deleted; runs they had planned are cancelled.',
    ...(n === 0
      ? []
      : [
          `${posts} to ${defaultName} unchanged: published posts stay as history, scheduled ones still go out at their time.`,
          `Drafts join ${defaultName}'s drafts, so one of its schedules may publish them.`,
        ]),
    'The collection itself cannot be brought back.',
  ]
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
      processing_timeout_seconds: collection.processing_timeout_seconds
        ? String(collection.processing_timeout_seconds)
        : '',
    },
  })
  const defaultName = useCollections(true, defaultNameOf).data ?? 'Default'
  const serverTimeout = useSession().data?.defaults?.processing_timeout_seconds ?? 3600
  const { register, handleSubmit, formState, reset, control, setError } = form
  const description = useWatch({ control, name: 'description' })

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
        processing_timeout_seconds: timeoutOf(v.processing_timeout_seconds),
      }),
    onSuccess: (c) => {
      onSaved(c)
      reset({
        name: c.name,
        description: c.description,
        color: c.color ?? '',
        processing_timeout_seconds: c.processing_timeout_seconds ? String(c.processing_timeout_seconds) : '',
      })
      namedToast(c.name, 'was updated')
    },
    onError: (e) => {
      // A name another collection has: under Name, like in the New collection dialog.
      if (e instanceof ApiError && e.status === 409 && /name/i.test(e.message)) {
        setError(
          'name',
          { type: 'server', message: 'A collection with this name already exists' },
          { shouldFocus: true },
        )

        return
      }

      setFormApiError(e, setError, fieldsOf('name', 'description', 'color'))
    },
  })

  const archive = useMutation({
    mutationFn: (archived: boolean) => api.updateCollection(collection.id, { archived }),
    onSuccess: (c) => {
      onSaved(c)

      if (c.archived) {
        namedToast(c.name, 'was archived', 'It is hidden from the list and takes no new posts.')
      } else {
        namedToast(c.name, 'was restored', 'It takes posts again.')
      }
    },
    onError: (e) => toastApiError(e),
  })

  const del = useMutation({
    mutationFn: () => api.deleteCollection(collection.id, confirmName),
    onSuccess: () => {
      qc.removeQueries({ queryKey: qk.collection(collection.id) })
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      // Every list from its first page: invalidating would reload each loaded page one by one.
      refetchPostListsFromStart(qc)
      namedToast(collection.name, 'was deleted', `Its posts are in ${defaultName} now.`)
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
    // Like every drawer: the header and the footer stay put, only this body scrolls.
    <>
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
        {/* minmax(0,1fr) tracks: long names, codes and rows shrink to the panel instead of widening it on phones. */}
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-6 p-4">
          <Section title="General">
            <form
              id="collection-settings-form"
              onSubmit={handleSubmit((v) => save.mutate(v))}
              className="grid grid-cols-[minmax(0,1fr)] gap-4"
              noValidate
            >
              <FieldGroup className="gap-4">
                <Field data-invalid={!!formState.errors.name}>
                  <FieldLabel htmlFor="settings-name" required>
                    Name
                  </FieldLabel>
                  <Input
                    id="settings-name"
                    {...register('name')}
                    aria-invalid={!!formState.errors.name}
                    aria-required
                  />
                  <FieldError errors={[formState.errors.name]} />
                </Field>
                <Field data-invalid={!!formState.errors.description}>
                  <FieldLabel htmlFor="settings-description">Description</FieldLabel>
                  <Textarea
                    id="settings-description"
                    rows={3}
                    {...register('description')}
                    value={description}
                    maxLength={180}
                    className="resize-none field-sizing-fixed"
                    aria-invalid={!!formState.errors.description}
                  />
                  <FieldError errors={[formState.errors.description]} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="settings-timeout">Fail publishing if nothing is reported within</FieldLabel>
                  <Controller
                    control={control}
                    name="processing_timeout_seconds"
                    render={({ field }) => (
                      <TimeoutSelect
                        id="settings-timeout"
                        value={field.value}
                        onChange={field.onChange}
                        fallback={serverTimeout}
                      />
                    )}
                  />
                  <FieldDescription className="text-xs">
                    When a post goes through your own automation, it has this long to report the result. Then that
                    channel fails, so nothing waits forever.
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
            </form>
          </Section>

          <Separator />
          <Foldable
            title="Schedules"
            description="When this collection's posts go out, and to which channels."
            testId="schedules-section"
            defaultOpen
          >
            <SchedulesSection collection={collection} />
          </Foldable>

          <Separator />
          {collection.is_default ? (
            <Section
              title="Default collection"
              description="Every workspace keeps one Default collection. You can rename it, but it cannot be archived or deleted."
            />
          ) : (
            <Foldable title="Danger zone" description="Archive or delete this collection." testId="danger-zone" danger>
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
              <Section
                danger
                title="Delete collection"
                description={`Permanently delete this collection and its schedules. Its posts move to ${defaultName}.`}
              >
                <div>
                  <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}>
                    <Trash2 /> Delete collection
                  </Button>
                </div>
              </Section>
            </Foldable>
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
                  <DialogDescription>What happens:</DialogDescription>
                </DialogHeader>
                <ul
                  className="grid list-disc gap-1 pl-5 text-sm text-muted-foreground"
                  data-testid="delete-consequences"
                >
                  {deleteConsequences(collection.post_count, defaultName).map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
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
      </div>
      <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        <Button type="button" variant="outline" onClick={onClose}>
          Close
        </Button>
        <Button type="submit" form="collection-settings-form" disabled={!formState.isDirty || save.isPending}>
          {save.isPending ? 'Saving...' : 'Save changes'}
        </Button>
      </div>
    </>
  )
}
