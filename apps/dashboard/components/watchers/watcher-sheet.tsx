'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, FolderPlus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { Callout } from '@/components/common/callout'
import { ChipsInput } from '@/components/common/chips-input'
import { MultiSelect } from '@/components/common/multi-select'
import { useShell } from '@/components/shell/app-shell'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Watcher, type WatcherInput } from '@/lib/api'
import { setFormApiError } from '@/lib/form-errors'
import { formatInterval, hostOf } from '@/lib/format'
import { qk, useCollections } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { INTERVAL_OPTIONS, intervalOption } from '@/lib/watchers'

import { IntervalSelect, KIND_LABELS, useMinInterval } from './watcher-shared'

export type WatcherSheetState = { mode: 'add' } | { mode: 'edit'; watcher: Watcher }

export function WatcherSheet({
  open,
  onOpenChange,
  state,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  state: WatcherSheetState
}) {
  const close = () => onOpenChange(false)

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        {open &&
          (state.mode === 'edit' ? (
            <EditWatcher key={state.watcher.id} watcher={state.watcher} onDone={close} />
          ) : (
            <>
              <Header
                title="Add a watcher"
                description="staqly polls a URL over HTTP. New entries land in a collection as pending items."
              />
              <WatcherForm onDone={close} />
            </>
          ))}
      </SheetContent>
    </Sheet>
  )
}

function Header({ title, description }: { title: string; description: string }) {
  return (
    <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))] pr-12">
      <SheetTitle>{title}</SheetTitle>
      <SheetDescription>{description}</SheetDescription>
    </SheetHeader>
  )
}

// ---- add ----

const URL_RE = /^https?:\/\/[^\s/?#]+[^\s]*$/i

/** API field → form field for a 422. */
const API_FIELDS: Record<string, keyof FormValues> = {
  name: 'name',
  url: 'url',
  collection: 'collections',
  collections: 'collections',
  every: 'every_seconds',
  keywords: 'keywords',
  exclude_keywords: 'exclude_keywords',
}

/** One schema for adding and editing a watcher (the edit form sends only the changed fields). */
const watcherSchema = z.object({
  name: z.string().trim().min(1, 'Give the watcher a name').max(100, 'At most 100 characters'),
  url: z.string().trim().regex(URL_RE, 'Enter an absolute http(s) URL').max(2000),
  collections: z.array(z.string()).min(1, 'Pick at least one collection').max(20, 'At most 20 collections'),
  every_seconds: z.number().int().positive(),
  keywords: z.array(z.string()).max(50, 'At most 50 keywords'),
  exclude_keywords: z.array(z.string()).max(50, 'At most 50 keywords'),
})

type FormValues = z.infer<typeof watcherSchema>

/** Hourly, or the plan minimum when that is longer. */
function defaultSeconds(minSeconds: number) {
  return (INTERVAL_OPTIONS.find((o) => o.seconds >= Math.max(3600, minSeconds)) ?? intervalOption(minSeconds)).seconds
}

function CollectionField({
  value,
  onChange,
  error,
}: {
  value: string[]
  onChange: (v: string[]) => void
  error?: string
}) {
  const collections = useCollections(true)
  const { openCreateCollection } = useShell()
  const list = (collections.data ?? []).filter((c) => !c.archived || value.includes(c.id))

  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor="watcher-collection" required>
        Add new entries to
      </FieldLabel>
      {collections.isLoading ? (
        <Skeleton className="h-8" />
      ) : list.length ? (
        <MultiSelect
          id="watcher-collection"
          label="Collections"
          value={value}
          onChange={onChange}
          options={list.map((c) => ({ value: c.id, label: c.name }))}
          placeholder="Choose collections..."
          invalid={!!error}
        />
      ) : (
        <div
          className={cn(
            'flex flex-wrap items-center gap-2 rounded-lg border border-dashed p-3 text-sm text-muted-foreground',
            error && 'border-destructive',
          )}
        >
          <span className="flex-1">You need a collection to collect entries into.</span>
          <Button type="button" size="sm" variant="outline" onClick={openCreateCollection}>
            <FolderPlus /> New collection
          </Button>
        </div>
      )}
      <FieldDescription className="text-xs">
        Each new entry is added to every collection you pick, as a pending item ready for review in the Inbox.
      </FieldDescription>
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

function KeywordFields({
  control,
  errors,
}: {
  control: ReturnType<typeof useForm<FormValues>>['control']
  errors: { keywords?: string; exclude_keywords?: string }
}) {
  const [keywords, exclude] = useWatch({ control, name: ['keywords', 'exclude_keywords'] })
  const used = keywords.length + exclude.length
  // Opens by itself only when the form starts with filters; after that the user decides.
  const [startOpen] = useState(used > 0)

  return (
    // Advanced: closed unless filters are set (or wrong), so a plain watcher is URL + name + collections.
    <details className="group rounded-lg border" open={startOpen || !!errors.keywords || !!errors.exclude_keywords}>
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-sm font-medium select-none">
        <ChevronRight className="size-4 text-muted-foreground transition-transform group-open:rotate-90" />
        Advanced: filter entries
        {used > 0 && <span className="text-xs font-normal text-muted-foreground">{used} keywords</span>}
      </summary>
      <div className="grid gap-3 border-t p-3">
        <p className="text-xs text-muted-foreground">
          Keywords are matched in each entry&apos;s title and summary (for a page: its text), ignoring case. With
          &quot;Only with&quot; set, an entry needs at least one of those words; any &quot;Skip&quot; word drops it.
          Entries left out are not added later.
        </p>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field data-invalid={!!errors.keywords}>
            <FieldLabel htmlFor="watcher-keywords">
              Only with keywords <span className="font-normal text-muted-foreground">(optional)</span>
            </FieldLabel>
            <Controller
              control={control}
              name="keywords"
              render={({ field }) => (
                <ChipsInput
                  id="watcher-keywords"
                  label="Keywords"
                  value={field.value}
                  onChange={field.onChange}
                  placeholder="e.g. portrait"
                  invalid={!!errors.keywords}
                />
              )}
            />
            <FieldError>{errors.keywords}</FieldError>
          </Field>
          <Field data-invalid={!!errors.exclude_keywords}>
            <FieldLabel htmlFor="watcher-exclude">
              Skip keywords <span className="font-normal text-muted-foreground">(optional)</span>
            </FieldLabel>
            <Controller
              control={control}
              name="exclude_keywords"
              render={({ field }) => (
                <ChipsInput
                  id="watcher-exclude"
                  label="Exclude keywords"
                  value={field.value}
                  onChange={field.onChange}
                  placeholder="e.g. sponsored"
                  tone="exclude"
                  invalid={!!errors.exclude_keywords}
                />
              )}
            />
            <FieldError>{errors.exclude_keywords}</FieldError>
          </Field>
        </div>
      </div>
    </details>
  )
}

function WatcherForm({ onDone }: { onDone: () => void }) {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const qc = useQueryClient()
  const collections = useCollections(true)
  const { minSeconds } = useMinInterval()
  const firstCollection = (collections.data ?? []).find((c) => !c.archived)?.id ?? ''

  const form = useForm<FormValues>({
    resolver: zodResolver(watcherSchema),
    defaultValues: {
      name: '',
      url: '',
      collections: firstCollection ? [firstCollection] : [],
      every_seconds: defaultSeconds(minSeconds),
      keywords: [],
      exclude_keywords: [],
    },
  })
  const { register, control, handleSubmit, formState, setValue, getValues } = form

  // Collections may load after the form mounts.
  useEffect(() => {
    if (getValues('collections').length === 0 && firstCollection) {
      setValue('collections', [firstCollection])
    }
  }, [firstCollection, getValues, setValue])

  const create = useMutation({
    mutationFn: (v: FormValues) =>
      api.createWatcher({
        name: v.name.trim(),
        url: v.url.trim(),
        collections: v.collections,
        every: intervalOption(v.every_seconds).every,
        keywords: v.keywords,
        exclude_keywords: v.exclude_keywords,
      }),
    onSuccess: (w) => {
      void qc.invalidateQueries({ queryKey: qk.watchers })
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success(`Watching ${w.name}`, {
        description:
          w.kind === 'rss'
            ? "It's a feed: each new entry becomes an item. The first check only records what's already there."
            : "It's a page: an item is added whenever its content changes. The first check records how it looks now.",
      })
      onDone()
    },
    onError: (e) => setFormApiError(e, form.setError, (k) => API_FIELDS[k] ?? null, 'Could not create the watcher'),
  })

  const errs = formState.errors

  return (
    <form
      onSubmit={handleSubmit((v) => create.mutate(v))}
      noValidate
      className="flex min-h-0 flex-1 flex-col"
      data-testid="watcher-form"
    >
      <div className="grid flex-1 content-start gap-5 overflow-y-auto overscroll-contain p-4">
        <Field data-invalid={!!errs.url}>
          <FieldLabel htmlFor="watcher-url" required>
            URL
          </FieldLabel>
          <Input
            id="watcher-url"
            {...register('url')}
            placeholder="https://example.com/feed.xml"

            className="font-mono"
            inputMode="url"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            aria-invalid={!!errs.url}
            aria-required
          />
          <FieldDescription className="text-xs">
            An RSS or Atom feed adds one item per new entry; any other page or JSON endpoint adds one when it changes.
          </FieldDescription>
          <FieldError errors={[errs.url]} />
        </Field>

        <Field data-invalid={!!errs.name}>
          <FieldLabel htmlFor="watcher-name" required>
            Name
          </FieldLabel>
          <Input
            id="watcher-name"
            {...register('name')}
            placeholder="Design blog"
            aria-invalid={!!errs.name}
            aria-required
            autoComplete="off"
          />
          <FieldError errors={[errs.name]} />
        </Field>

        <Controller
          control={control}
          name="collections"
          render={({ field }) => (
            <CollectionField value={field.value} onChange={field.onChange} error={errs.collections?.message} />
          )}
        />

        <Field data-invalid={!!errs.every_seconds}>
          <FieldLabel htmlFor="watcher-every">Check</FieldLabel>
          <Controller
            control={control}
            name="every_seconds"
            render={({ field }) => (
              <IntervalSelect
                id="watcher-every"
                value={field.value}
                onChange={field.onChange}
                invalid={!!errs.every_seconds}
              />
            )}
          />
          <FieldError errors={[errs.every_seconds]} />
        </Field>

        <KeywordFields
          control={control}
          errors={{ keywords: errs.keywords?.message, exclude_keywords: errs.exclude_keywords?.message }}
        />

        <Callout tone="info" role="note">
          The first check only records what&apos;s already there, so you won&apos;t get a flood of old posts. Fetched
          content is marked <strong className="font-medium">External content</strong>.
        </Callout>
      </div>
      <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? 'Creating...' : 'Start watching'}
        </Button>
      </div>
    </form>
  )
}

// ---- edit ----

function EditWatcher({ watcher: w, onDone }: { watcher: Watcher; onDone: () => void }) {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const qc = useQueryClient()
  const form = useForm<FormValues>({
    resolver: zodResolver(watcherSchema),
    defaultValues: {
      name: w.name,
      url: w.url,
      collections: w.collection_ids ?? [w.collection_id],
      every_seconds: w.every_seconds,
      keywords: w.keywords ?? [],
      exclude_keywords: w.exclude_keywords ?? [],
    },
  })
  const { register, control, handleSubmit, formState, setError } = form
  const save = useMutation({
    mutationFn: (v: FormValues) => {
      const d = formState.dirtyFields
      const body: WatcherInput = {}

      if (d.name) {
        body.name = v.name.trim()
      }

      if (d.url) {
        body.url = v.url.trim()
      }

      if (d.collections) {
        body.collections = v.collections
      }

      if (d.every_seconds) {
        body.every = intervalOption(v.every_seconds).every
      }

      if (d.keywords) {
        body.keywords = v.keywords
      }

      if (d.exclude_keywords) {
        body.exclude_keywords = v.exclude_keywords
      }

      return api.updateWatcher(w.id, body)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.watchers })
      toast.success('Watcher saved')
      onDone()
    },
    onError: (e) => setFormApiError(e, setError, (k) => API_FIELDS[k] ?? null, 'Could not save the watcher'),
  })
  const errs = formState.errors

  return (
    <>
      <Header
        title="Edit watcher"
        description={`${KIND_LABELS[w.kind] ?? w.kind} · ${hostOf(w.url)} · every ${formatInterval(w.every_seconds)}`}
      />
      <form
        onSubmit={handleSubmit((v) => save.mutate(v))}
        noValidate
        className="flex min-h-0 flex-1 flex-col"
        data-testid="watcher-edit-form"
      >
        <div className="grid flex-1 content-start gap-5 overflow-y-auto overscroll-contain p-4">
          <Field data-invalid={!!errs.name}>
            <FieldLabel htmlFor="edit-name" required>
              Name
            </FieldLabel>
            <Input id="edit-name" {...register('name')} aria-invalid={!!errs.name} aria-required autoComplete="off" />
            <FieldError errors={[errs.name]} />
          </Field>
          <Field data-invalid={!!errs.url}>
            <FieldLabel htmlFor="edit-url" required>
              URL
            </FieldLabel>
            <Input
              id="edit-url"
              {...register('url')}
              className="font-mono"
              aria-invalid={!!errs.url}
              aria-required
              autoCapitalize="off"
              spellCheck={false}
            />
            <FieldError errors={[errs.url]} />
          </Field>
          <Controller
            control={control}
            name="collections"
            render={({ field }) => (
              <CollectionField value={field.value} onChange={field.onChange} error={errs.collections?.message} />
            )}
          />
          <Field data-invalid={!!errs.every_seconds}>
            <FieldLabel htmlFor="edit-every">Check</FieldLabel>
            <Controller
              control={control}
              name="every_seconds"
              render={({ field }) => (
                <IntervalSelect
                  id="edit-every"
                  value={field.value}
                  onChange={field.onChange}
                  invalid={!!errs.every_seconds}
                />
              )}
            />
            <FieldError errors={[errs.every_seconds]} />
          </Field>
          <KeywordFields
            control={control}
            errors={{ keywords: errs.keywords?.message, exclude_keywords: errs.exclude_keywords?.message }}
          />
        </div>
        <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          <Button type="button" variant="outline" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" disabled={save.isPending || !formState.isDirty}>
            {save.isPending ? 'Saving...' : 'Save watcher'}
          </Button>
        </div>
      </form>
    </>
  )
}
