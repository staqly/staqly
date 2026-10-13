'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, FolderPlus, Globe, Rss, Search, Settings2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { ChipsInput } from '@/components/common/chips-input'
import { ErrorState } from '@/components/common/error-state'
import { NativeSelect } from '@/components/common/native-select'
import { SkeletonList } from '@/components/common/skeleton-list'
import { useShell } from '@/components/shell/app-shell'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError, type Watcher, type WatcherInput, type WatcherKind, type WatcherPreset } from '@/lib/api'
import { formatInterval, hostOf } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useCollections, useWatcherPresets } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { defaultInterval, intervalOption, sortCategories } from '@/lib/watchers'

import { CATEGORY_ICONS, IntervalSelect, KIND_LABELS, SourceIcon, useMinInterval } from './watcher-shared'

export type WatcherSheetState =
  | { mode: 'add'; presetId?: string; custom?: boolean }
  | { mode: 'edit'; watcher: Watcher }

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
  const key = state.mode === 'edit' ? state.watcher.id : `add:${state.presetId ?? ''}:${state.custom ? 1 : 0}`

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        {open &&
          (state.mode === 'edit' ? (
            <EditWatcher key={key} watcher={state.watcher} onDone={close} />
          ) : (
            <AddWatcher key={key} initialPresetId={state.presetId} initialCustom={state.custom} onDone={close} />
          ))}
      </SheetContent>
    </Sheet>
  )
}

function Header({ title, description, onBack }: { title: string; description: string; onBack?: () => void }) {
  return (
    <SheetHeader className="flex-row items-start gap-2 border-b pt-[max(1rem,env(safe-area-inset-top))] pr-12">
      {onBack && (
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          onClick={onBack}
          aria-label="Back to sources"
          className="-ml-1.5 shrink-0"
        >
          <ArrowLeft />
        </Button>
      )}
      <div className="min-w-0">
        <SheetTitle>{title}</SheetTitle>
        <SheetDescription>{description}</SheetDescription>
      </div>
    </SheetHeader>
  )
}

// ---- add: gallery → form ----

function AddWatcher({
  initialPresetId,
  initialCustom,
  onDone,
}: {
  initialPresetId?: string
  initialCustom?: boolean
  onDone: () => void
}) {
  const presets = useWatcherPresets()
  const [presetId, setPresetId] = useState<string | undefined>(initialPresetId)
  const [custom, setCustom] = useState(!!initialCustom)
  const preset = presets.data?.find((p) => p.id === presetId)

  if (custom) {
    return (
      <>
        <Header
          title="Custom feed or page"
          description="Any RSS/Atom feed, or a page to watch for changes."
          onBack={() => setCustom(false)}
        />
        <WatcherForm onDone={onDone} />
      </>
    )
  }

  if (presetId && preset) {
    return (
      <>
        <Header title={preset.title} description={preset.description} onBack={() => setPresetId(undefined)} />
        <WatcherForm preset={preset} onDone={onDone} />
      </>
    )
  }

  return (
    <>
      <Header title="Add a watcher" description="Pick a source. New entries land in a collection as pending items." />
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        {presets.isLoading ? (
          <div className="grid gap-2">
            <SkeletonList count={5} className="h-16 rounded-xl" />
          </div>
        ) : presets.error ? (
          <ErrorState error={presets.error} onRetry={() => presets.refetch()} />
        ) : (
          <PresetGallery
            presets={presets.data ?? []}
            onPick={(p) => setPresetId(p.id)}
            onCustom={() => setCustom(true)}
          />
        )}
      </div>
    </>
  )
}

export function PresetGallery({
  presets,
  onPick,
  onCustom,
}: {
  presets: WatcherPreset[]
  onPick: (p: WatcherPreset) => void
  onCustom: () => void
}) {
  const [q, setQ] = useState('')
  const needle = q.trim().toLowerCase()
  const filtered = needle
    ? presets.filter((p) => `${p.title} ${p.description} ${p.category}`.toLowerCase().includes(needle))
    : presets
  const categories = sortCategories(filtered.map((p) => p.category))

  return (
    <div className="grid gap-5" data-testid="preset-gallery">
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search sources — reddit, youtube, github…"
          aria-label="Search sources"
          className="pl-8"
        />
      </div>
      {categories.map((cat) => {
        const Icon = CATEGORY_ICONS[cat] ?? Rss

        return (
          <section key={cat} className="grid gap-2" aria-label={cat}>
            <h3 className="flex items-center gap-1.5 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
              <Icon className="size-3.5" /> {cat}
            </h3>
            <div className="grid gap-2 sm:grid-cols-2">
              {filtered
                .filter((p) => p.category === cat)
                .map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => onPick(p)}
                    data-testid={`preset-${p.id}`}
                    className="flex min-h-16 items-start gap-3 rounded-xl border bg-card p-3 text-left transition-colors hover:border-foreground/20 hover:bg-muted/40"
                  >
                    <SourceIcon presetId={p.id} kind={p.kind} className="size-8" />
                    <span className="min-w-0">
                      <span className="block text-sm leading-tight font-medium">{p.title}</span>
                      <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">{p.description}</span>
                    </span>
                  </button>
                ))}
            </div>
          </section>
        )
      })}
      {!categories.length && <p className="text-center text-sm text-muted-foreground">No matching sources.</p>}
      <section className="grid gap-2" aria-label="Advanced">
        <h3 className="flex items-center gap-1.5 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          <Settings2 className="size-3.5" /> Advanced
        </h3>
        <button
          type="button"
          onClick={onCustom}
          className="flex items-start gap-3 rounded-xl border border-dashed p-3 text-left transition-colors hover:bg-muted/40"
        >
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
            <Settings2 className="size-4" />
          </span>
          <span className="min-w-0">
            <span className="block text-sm font-medium">Custom feed / page</span>
            <span className="block text-xs text-muted-foreground">
              Paste any RSS/Atom URL, or watch a page for changes.
            </span>
          </span>
        </button>
      </section>
    </div>
  )
}

// ---- the form (preset or custom) ----

interface FormValues {
  name: string
  kind: WatcherKind
  url: string
  params: Record<string, string>
  collection: string
  every_seconds: number
  keywords: string[]
  exclude_keywords: string[]
}

const URL_RE = /^https?:\/\/[^\s/?#]+[^\s]*$/i

function buildSchema(preset?: WatcherPreset) {
  const params: Record<string, z.ZodString> = {}

  for (const p of preset?.params ?? []) {
    if (p.name === 'keywords') {
      continue
    } // edited as chips

    params[p.name] = p.required
      ? z.string().trim().min(1, `${p.label} is required`).max(2000)
      : z.string().trim().max(2000)
  }

  return z
    .object({
      name: preset
        ? z.string().trim().max(100, 'At most 100 characters')
        : z.string().trim().min(1, 'Give the watcher a name').max(100, 'At most 100 characters'),
      kind: z.enum(['rss', 'url_diff']),
      url: z.string().trim(),
      params: z.object(params),
      collection: z.string().min(1, 'Pick a collection'),
      every_seconds: z.number().int().positive(),
      keywords: z.array(z.string()).max(50, 'At most 50 keywords'),
      exclude_keywords: z.array(z.string()).max(50, 'At most 50 keywords'),
    })
    .superRefine((v, ctx) => {
      if (!preset && (!URL_RE.test(v.url) || v.url.length > 2000)) {
        ctx.addIssue({ code: 'custom', path: ['url'], message: 'Enter an absolute http(s) URL' })
      }

      for (const p of preset?.params ?? []) {
        const val = v.params[p.name]?.trim() ?? ''

        if (val && p.name === 'url' && !URL_RE.test(val)) {
          ctx.addIssue({ code: 'custom', path: ['params', p.name], message: 'Enter an absolute http(s) URL' })
        }
      }
    })
}

function CollectionField({ value, onChange, error }: { value: string; onChange: (v: string) => void; error?: string }) {
  const collections = useCollections(true)
  const { openCreateCollection } = useShell()
  const list = (collections.data ?? []).filter((c) => !c.archived || c.id === value)

  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor="watcher-collection">Add new entries to</FieldLabel>
      {collections.isLoading ? (
        <Skeleton className="h-8" />
      ) : list.length ? (
        <NativeSelect
          id="watcher-collection"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={!!error}
        >
          {!value && <option value="">Choose a collection…</option>}
          {list.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </NativeSelect>
      ) : (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
          <span className="flex-1">You need a collection to collect entries into.</span>
          <Button type="button" size="sm" variant="outline" onClick={openCreateCollection}>
            <FolderPlus /> New collection
          </Button>
        </div>
      )}
      <FieldDescription className="text-xs">
        They arrive as pending link items, ready for review in the Inbox.
      </FieldDescription>
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

function KeywordFields({ control }: { control: ReturnType<typeof useForm<FormValues>>['control'] }) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field>
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
            />
          )}
        />
      </Field>
      <Field>
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
            />
          )}
        />
      </Field>
    </div>
  )
}

function WatcherForm({ preset, onDone }: { preset?: WatcherPreset; onDone: () => void }) {
  const qc = useQueryClient()
  const collections = useCollections(true)
  const { minSeconds } = useMinInterval()
  const schema = useMemo(() => buildSchema(preset), [preset])
  const firstCollection = (collections.data ?? []).find((c) => !c.archived)?.id ?? ''
  const keywordParam = preset?.params.some((p) => p.name === 'keywords') ?? false

  const form = useForm<FormValues>({
    resolver: zodResolver(schema) as never,
    defaultValues: {
      name: '',
      kind: preset?.kind ?? 'rss',
      url: '',
      params: Object.fromEntries(
        (preset?.params ?? []).filter((p) => p.name !== 'keywords').map((p) => [p.name, p.default ?? '']),
      ),
      collection: firstCollection,
      every_seconds: defaultInterval(preset?.default_every ?? '1h', minSeconds).seconds,
      keywords: [],
      exclude_keywords: [],
    },
  })
  const { register, control, handleSubmit, formState, setValue, getValues } = form

  // Collections may load after the form mounts.
  useEffect(() => {
    if (!getValues('collection') && firstCollection) {
      setValue('collection', firstCollection)
    }
  }, [firstCollection, getValues, setValue])
  const kind = useWatch({ control, name: 'kind' })

  const create = useMutation({
    mutationFn: async (v: FormValues) => {
      const every = intervalOption(v.every_seconds).every

      if (!preset) {
        const body: WatcherInput = {
          name: v.name.trim(),
          kind: v.kind,
          url: v.url.trim(),
          collection: v.collection,
          every,
          keywords: v.keywords,
          exclude_keywords: v.exclude_keywords,
        }

        return api.createWatcher(body)
      }

      const params: Record<string, string> = {}

      for (const [k, val] of Object.entries(v.params)) {
        if (val.trim()) {
          params[k] = val.trim()
        }
      }

      if (keywordParam && v.keywords.length) {
        params.keywords = v.keywords.join(',')
      }

      let w = await api.createWatcherFromPreset({
        preset_id: preset.id,
        params,
        collection: v.collection,
        every,
        ...(v.name.trim() ? { name: v.name.trim() } : {}),
      })
      // from-preset has no keyword fields; apply the remaining filters in a follow-up PATCH.
      const extraKeywords = keywordParam ? [] : v.keywords

      if (extraKeywords.length || v.exclude_keywords.length) {
        w = await api.updateWatcher(w.id, {
          ...(extraKeywords.length ? { keywords: [...new Set([...(w.keywords ?? []), ...extraKeywords])] } : {}),
          ...(v.exclude_keywords.length ? { exclude_keywords: v.exclude_keywords } : {}),
        })
      }

      return w
    },
    onSuccess: (w) => {
      void qc.invalidateQueries({ queryKey: qk.watchers })
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success(`Watching ${w.name}`, {
        description: "The first check records what's already there; new entries show up in the Inbox after that.",
      })
      onDone()
    },
    onError: (e) => {
      if (e instanceof ApiError && e.fields) {
        for (const [k, msg] of Object.entries(e.fields)) {
          const path = k.startsWith('params.') ? k : k === 'collection' ? 'collection' : k === 'url' ? 'url' : null

          if (path) {
            form.setError(path as never, { message: `${msg}` })
          }
        }
      }

      toastApiError(e, 'Could not create the watcher')
    },
  })

  const visibleParams = (preset?.params ?? []).filter((p) => p.name !== 'keywords')
  const errs = formState.errors

  return (
    <form
      onSubmit={handleSubmit((v) => create.mutate(v))}
      noValidate
      className="flex min-h-0 flex-1 flex-col"
      data-testid="watcher-form"
    >
      <div className="grid flex-1 content-start gap-5 overflow-y-auto overscroll-contain p-4">
        {preset ? (
          visibleParams.length > 0 && (
            <div className="grid gap-4">
              {visibleParams.map((p) => {
                const err = (errs.params as Record<string, { message?: string }> | undefined)?.[p.name]

                return (
                  <Field key={p.name} data-invalid={!!err}>
                    <FieldLabel htmlFor={`param-${p.name}`}>
                      {p.label}
                      {p.required ? (
                        <span className="text-destructive" aria-hidden>
                          *
                        </span>
                      ) : (
                        !/optional/i.test(p.label) && (
                          <span className="font-normal text-muted-foreground">(optional)</span>
                        )
                      )}
                    </FieldLabel>
                    <Input
                      id={`param-${p.name}`}
                      {...register(`params.${p.name}`)}
                      placeholder={p.placeholder || p.default || ''}
                      aria-invalid={!!err}
                      aria-required={p.required}
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      className={cn(p.name === 'url' && 'font-mono')}
                    />
                    {err && <FieldError>{err.message}</FieldError>}
                  </Field>
                )
              })}
            </div>
          )
        ) : (
          <>
            <Field>
              <FieldLabel>What to watch</FieldLabel>
              <div role="radiogroup" aria-label="Watcher kind" className="grid grid-cols-2 gap-2">
                {(['rss', 'url_diff'] as const).map((k) => {
                  const on = kind === k
                  const Icon = k === 'rss' ? Rss : Globe

                  return (
                    <button
                      key={k}
                      type="button"
                      role="radio"
                      aria-checked={on}
                      onClick={() => setValue('kind', k)}
                      className={cn(
                        'flex items-start gap-2 rounded-xl border p-3 text-left transition-colors',
                        on ? 'border-brand/60 bg-brand-soft/60 ring-1 ring-brand/30' : 'hover:bg-muted/60',
                      )}
                    >
                      <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                      <span className="min-w-0">
                        <span className="block text-sm font-medium">
                          {k === 'rss' ? 'RSS / Atom feed' : 'Page changes'}
                        </span>
                        <span className="block text-xs text-muted-foreground">
                          {k === 'rss' ? 'One item per new entry' : 'One item when the text changes'}
                        </span>
                      </span>
                    </button>
                  )
                })}
              </div>
            </Field>
            <Field data-invalid={!!errs.url}>
              <FieldLabel htmlFor="watcher-url">{kind === 'rss' ? 'Feed URL' : 'Page URL'}</FieldLabel>
              <Input
                id="watcher-url"
                {...register('url')}
                placeholder={kind === 'rss' ? 'https://example.com/feed.xml' : 'https://example.com/changelog'}
                className="font-mono"
                inputMode="url"
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                aria-invalid={!!errs.url}
              />
              <FieldError errors={[errs.url]} />
            </Field>
          </>
        )}

        <Field data-invalid={!!errs.name}>
          <FieldLabel htmlFor="watcher-name">
            Name {preset && <span className="font-normal text-muted-foreground">(optional)</span>}
          </FieldLabel>
          <Input
            id="watcher-name"
            {...register('name')}
            placeholder={preset?.title ?? 'Design blog'}
            aria-invalid={!!errs.name}
            autoComplete="off"
          />
          <FieldError errors={[errs.name]} />
        </Field>

        <Controller
          control={control}
          name="collection"
          render={({ field }) => (
            <CollectionField value={field.value} onChange={field.onChange} error={errs.collection?.message} />
          )}
        />

        <Field>
          <FieldLabel htmlFor="watcher-every">Check</FieldLabel>
          <Controller
            control={control}
            name="every_seconds"
            render={({ field }) => <IntervalSelect id="watcher-every" value={field.value} onChange={field.onChange} />}
          />
        </Field>

        <KeywordFields control={control} />

        <p className="rounded-lg bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
          The first check only records what&apos;s already there, so you won&apos;t get a flood of old posts. Fetched
          content is marked <strong className="font-medium text-foreground">External content</strong>.
        </p>
      </div>
      <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? 'Creating…' : 'Start watching'}
        </Button>
      </div>
    </form>
  )
}

// ---- edit ----

const editSchema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters'),
  url: z.string().trim().regex(URL_RE, 'Enter an absolute http(s) URL').max(2000),
  collection: z.string().min(1, 'Pick a collection'),
  every_seconds: z.number().int().positive(),
  keywords: z.array(z.string()).max(50),
  exclude_keywords: z.array(z.string()).max(50),
})

type EditValues = z.infer<typeof editSchema>

function EditWatcher({ watcher: w, onDone }: { watcher: Watcher; onDone: () => void }) {
  const qc = useQueryClient()
  const form = useForm<EditValues>({
    resolver: zodResolver(editSchema),
    defaultValues: {
      name: w.name,
      url: w.url,
      collection: w.collection_id,
      every_seconds: w.every_seconds,
      keywords: w.keywords ?? [],
      exclude_keywords: w.exclude_keywords ?? [],
    },
  })
  const { register, control, handleSubmit, formState } = form
  const save = useMutation({
    mutationFn: (v: EditValues) => {
      const d = formState.dirtyFields
      const body: WatcherInput = {}

      if (d.name) {
        body.name = v.name.trim()
      }

      if (d.url) {
        body.url = v.url.trim()
      }

      if (d.collection) {
        body.collection = v.collection
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
    onError: (e) => toastApiError(e, 'Could not save the watcher'),
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
            <FieldLabel htmlFor="edit-name">Name</FieldLabel>
            <Input id="edit-name" {...register('name')} aria-invalid={!!errs.name} autoComplete="off" />
            <FieldError errors={[errs.name]} />
          </Field>
          <Field data-invalid={!!errs.url}>
            <FieldLabel htmlFor="edit-url">{w.kind === 'rss' ? 'Feed URL' : 'Page URL'}</FieldLabel>
            <Input
              id="edit-url"
              {...register('url')}
              className="font-mono"
              aria-invalid={!!errs.url}
              autoCapitalize="off"
              spellCheck={false}
            />
            <FieldError errors={[errs.url]} />
          </Field>
          <Controller
            control={control}
            name="collection"
            render={({ field }) => (
              <CollectionField value={field.value} onChange={field.onChange} error={errs.collection?.message} />
            )}
          />
          <Field>
            <FieldLabel htmlFor="edit-every">Check</FieldLabel>
            <Controller
              control={control}
              name="every_seconds"
              render={({ field }) => <IntervalSelect id="edit-every" value={field.value} onChange={field.onChange} />}
            />
          </Field>
          <KeywordFields control={control as never} />
        </div>
        <div className="flex justify-end gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          <Button type="button" variant="outline" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" disabled={save.isPending || !formState.isDirty}>
            {save.isPending ? 'Saving…' : 'Save watcher'}
          </Button>
        </div>
      </form>
    </>
  )
}
