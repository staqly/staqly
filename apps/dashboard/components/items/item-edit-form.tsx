'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Lock, RotateCcw, Save } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { Callout } from '@/components/common/callout'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel, FieldTitle } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, type Item, type UpdateItemInput } from '@/lib/api'
import { setFormApiError } from '@/lib/form-errors'
import { formatBytes, prettyJSON } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, qk, syncItemInCache, useCollections } from '@/lib/queries'

// Statuses whose content is locked (status itself changes only through actions). Mirrors the API
// (apps/api/internal/app/items.go): processing is locked here too, an automation is working on it.
const LOCKED = new Set(['approved', 'processing', 'completed', 'archived'])
const RESUBMITS = new Set(['rejected', 'failed', 'expired'])

function isJSONObject(s: string) {
  try {
    const v = JSON.parse(s)

    return v !== null && typeof v === 'object' && !Array.isArray(v)
  } catch {
    return false
  }
}

const schema = z
  .object({
    title: z.string().trim().min(1, 'Title is required').max(500, 'At most 500 characters'),
    type: z.string(),
    content: z.string().max(1_048_576, 'At most 1 MiB'),
    url: z
      .string()
      .trim()
      .refine((v) => v === '' || /^https?:\/\/\S+$/i.test(v), 'Must be an absolute http(s) URL'),
    metadata: z.string().refine((v) => v.trim() === '' || isJSONObject(v), 'Metadata must be a JSON object'),
    status: z.string(),
    source: z.string().max(200, 'At most 200 characters'),
    external_ref: z.string().max(200, 'At most 200 characters'),
    collection_id: z.string(),
  })
  .superRefine((v, ctx) => {
    // Text and JSON items are their content; a link is its URL (its content is an optional description).
    if ((v.type === 'text' || v.type === 'json') && v.content.trim() === '') {
      ctx.addIssue({ code: 'custom', path: ['content'], message: 'Content is required' })
    }

    if (v.type === 'link' && v.url.trim() === '') {
      ctx.addIssue({ code: 'custom', path: ['url'], message: 'URL is required' })
    }

    if (v.type === 'json' && v.content.trim() !== '') {
      try {
        JSON.parse(v.content)
      } catch {
        ctx.addIssue({ code: 'custom', path: ['content'], message: 'Content must be valid JSON for JSON items' })
      }
    }
  })

type FormValues = z.infer<typeof schema>

function toForm(item: Item): FormValues {
  return {
    title: item.title,
    type: item.type,
    content: item.type === 'json' ? (item.content ? prettyJSON(item.content) : '') : item.content,
    url: item.url,
    metadata: JSON.stringify(item.metadata ?? {}, null, 2),
    status: item.status,
    source: item.source,
    external_ref: item.external_ref,
    collection_id: item.collection_id,
  }
}

export function buildItemPatch(item: Item, v: FormValues): UpdateItemInput {
  const patch: UpdateItemInput = {}

  if (v.title !== item.title) {
    patch.title = v.title
  }

  if (item.type === 'json') {
    const next = v.content.trim() === '' ? '' : JSON.stringify(JSON.parse(v.content))

    if (next !== item.content) {
      patch.content = next === '' ? '' : JSON.parse(v.content)
    }
  } else if (v.content !== item.content) {
    patch.content = v.content
  }

  if (v.url.trim() !== item.url) {
    patch.url = v.url.trim()
  }

  const meta = v.metadata.trim() === '' ? {} : JSON.parse(v.metadata)

  if (JSON.stringify(meta) !== JSON.stringify(item.metadata ?? {})) {
    patch.metadata = meta
  }

  if (v.source !== item.source) {
    patch.source = v.source
  }

  if (v.external_ref !== item.external_ref) {
    patch.external_ref = v.external_ref
  }

  if (v.collection_id !== item.collection_id) {
    patch.collection_id = v.collection_id
  }

  return patch
}

export function ItemEditForm({ item }: { item: Item }) {
  return <ItemEditFormInner key={item.id} latest={item} />
}

/**
 * The form edits a fixed base version of the item. Saves send that version as
 * expected_version, so concurrent changes surface as a 409 instead of being
 * silently overwritten. When the item changes and there are no local edits,
 * the form quietly rebases onto the latest version.
 */
function ItemEditFormInner({ latest }: { latest: Item }) {
  // React Hook Form's register() returns refs and handlers the React Compiler must not
  // memoise: after reset() a memoised input stays detached and edits are lost.
  'use no memo'

  const qc = useQueryClient()
  const collections = useCollections(true)
  const [item, setBase] = useState(latest)
  const [conflict, setConflict] = useState<string | null>(null)
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: toForm(latest) })
  const { register, handleSubmit, formState, control, reset } = form
  // Status changes only through actions; after a decision the reviewed content is locked (the API returns 409).
  const locked = LOCKED.has(item.status)
  const resubmits = RESUBMITS.has(item.status)
  const isDirty = formState.isDirty

  useEffect(() => {
    // Only move forward: right after a save the cache can still hold the older version.
    if (latest.version > item.version && !isDirty) {
      // oxlint-disable-next-line react/set-state-in-effect -- syncs the query cache (an external store) into the form's base version
      setBase(latest)
      reset(toForm(latest))
    }
  }, [latest, item.version, isDirty, reset])

  function rebase(next: Item) {
    setBase(next)
    reset(toForm(next))
  }

  const save = useMutation({
    mutationFn: (patch: UpdateItemInput) => api.updateItem(item.id, { ...patch, expected_version: item.version }),
    onSuccess: (updated) => {
      rebase(updated)
      syncItemInCache(qc, updated)
      invalidateAfterItemChange(qc, updated.id)
      setConflict(null)
      namedToast(updated.title, 'was saved')
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) {
        setConflict(err.message)

        return
      }

      setFormApiError(err, form.setError, (k) =>
        k === 'type' ? 'content' : k in form.getValues() ? (k as keyof FormValues) : null,
      )
    },
  })

  const onSubmit = handleSubmit((values) => {
    const patch = buildItemPatch(item, values)

    if (Object.keys(patch).length === 0) {
      toast.info('No changes to save')

      return
    }

    save.mutate(patch)
  })

  return (
    <form onSubmit={onSubmit} className="grid gap-5" data-testid="item-edit-form" noValidate>
      {conflict && (
        <Callout
          role="alert"
          action={
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={async () => {
                try {
                  const fresh = await qc.fetchQuery({ queryKey: qk.item(item.id), queryFn: () => api.item(item.id) })

                  setConflict(null)
                  rebase(fresh)
                } catch (e) {
                  toastApiError(e)
                }
              }}
            >
              <RotateCcw /> Reload latest
            </Button>
          }
        >
          <div className="font-medium">Someone else changed this item</div>
          <div className="text-muted-foreground">{conflict}. Reload the latest version and re-apply your edits.</div>
        </Callout>
      )}
      {locked && (
        <Callout tone="neutral" icon={Lock} data-testid="content-locked">
          <p className="text-muted-foreground">
            The content of a{item.status === 'approved' || item.status === 'archived' ? 'n' : ''} {item.status} item is
            locked: what was approved stays as it was. Metadata, source and collection can still change here, tags under
            Properties. To change the content, regenerate it or add a new item.
          </p>
        </Callout>
      )}
      {resubmits && (
        <Callout tone="neutral" icon={RotateCcw} data-testid="content-resubmits">
          <p className="text-muted-foreground">
            Fixing the title or content sends it back to <strong className="text-foreground">Pending</strong> for a new
            review.
          </p>
        </Callout>
      )}
      <FieldGroup className="gap-4">
        <Field data-invalid={!!formState.errors.title}>
          <FieldLabel htmlFor="item-title" required>
            Title
          </FieldLabel>
          <Input
            id="item-title"
            {...register('title')}
            readOnly={locked}
            aria-invalid={!!formState.errors.title}
            aria-required
          />
          <FieldError errors={[formState.errors.title]} />
        </Field>
        {/* An uploaded file is shown, not edited here; a URL is the item's source when there is no file
            (required only for links). */}
        {item.media && (
          <Field>
            <FieldTitle>File</FieldTitle>
            <div className="rounded-lg border bg-muted/40 px-3 py-2 text-sm">
              <span className="font-medium">{item.media.original_name}</span>
              <span className="text-muted-foreground">
                {' '}
                · {item.media.mime_type} · {formatBytes(item.media.size_bytes)}
              </span>
            </div>
          </Field>
        )}
        {(item.type === 'link' || item.url || (!item.media && item.type !== 'text' && item.type !== 'json')) && (
          <Field data-invalid={!!formState.errors.url}>
            <FieldLabel htmlFor="item-url" required={item.type === 'link'}>
              URL
            </FieldLabel>
            <Input
              id="item-url"
              {...register('url')}
              readOnly={locked}
              className="font-mono text-xs"
              placeholder="https://..."
              aria-invalid={!!formState.errors.url}
              aria-required={item.type === 'link'}
            />
            <FieldError errors={[formState.errors.url]} />
          </Field>
        )}
        <Field data-invalid={!!formState.errors.content}>
          <FieldLabel htmlFor="item-content" required={item.type === 'text' || item.type === 'json'}>
            {item.type === 'json' ? 'JSON content' : item.type === 'text' ? 'Content' : 'Description'}
          </FieldLabel>
          <Textarea
            id="item-content"
            rows={item.type === 'text' || item.type === 'json' ? 10 : 3}
            {...register('content')}
            readOnly={locked}
            className={item.type === 'json' ? 'font-mono text-xs' : undefined}
            aria-invalid={!!formState.errors.content}
            aria-required={item.type === 'text' || item.type === 'json'}
          />
          <FieldError errors={[formState.errors.content]} />
        </Field>
        <Field data-invalid={!!formState.errors.metadata}>
          <FieldLabel htmlFor="item-metadata">Metadata</FieldLabel>
          <Textarea
            id="item-metadata"
            rows={6}
            spellCheck={false}
            {...register('metadata')}
            className="font-mono text-xs"
            aria-invalid={!!formState.errors.metadata}
          />
          <FieldDescription>A JSON object. Saving replaces the whole metadata object.</FieldDescription>
          <FieldError errors={[formState.errors.metadata]} />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field data-invalid={!!formState.errors.collection_id}>
            <FieldLabel htmlFor="item-collection">Collection</FieldLabel>
            <Controller
              control={control}
              name="collection_id"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger
                    id="item-collection"
                    className="w-full"
                    aria-label="Collection"
                    aria-invalid={!!formState.errors.collection_id}
                  >
                    <SelectValue placeholder="Collection" />
                  </SelectTrigger>
                  <SelectContent>
                    {(collections.data ?? []).map((c) => (
                      <SelectItem key={c.id} value={c.id}>
                        {c.name}
                        {c.archived ? ' (archived)' : ''}
                      </SelectItem>
                    ))}
                    {!collections.data?.some((c) => c.id === item.collection_id) && (
                      <SelectItem value={item.collection_id}>{item.collection_id}</SelectItem>
                    )}
                  </SelectContent>
                </Select>
              )}
            />
            <FieldError errors={[formState.errors.collection_id]} />
          </Field>
          <Field data-invalid={!!formState.errors.source}>
            <FieldLabel htmlFor="item-source">Source</FieldLabel>
            <Input
              id="item-source"
              {...register('source')}
              placeholder="e.g. n8n"
              aria-invalid={!!formState.errors.source}
            />
            <FieldError errors={[formState.errors.source]} />
          </Field>
          <Field data-invalid={!!formState.errors.external_ref}>
            <FieldLabel htmlFor="item-external-ref">External ref</FieldLabel>
            <Input
              id="item-external-ref"
              {...register('external_ref')}
              className="font-mono text-xs"
              aria-invalid={!!formState.errors.external_ref}
            />
            <FieldError errors={[formState.errors.external_ref]} />
          </Field>
        </div>
      </FieldGroup>
      <div className="flex items-center justify-end gap-2">
        <Button type="button" variant="ghost" disabled={!formState.isDirty} onClick={() => reset(toForm(item))}>
          Discard
        </Button>
        {/* Nothing edited means nothing to save: the current values (status included) are not a change. */}
        <Button type="submit" disabled={save.isPending || !isDirty}>
          <Save /> {save.isPending ? 'Saving...' : 'Save changes'}
        </Button>
      </div>
    </form>
  )
}
