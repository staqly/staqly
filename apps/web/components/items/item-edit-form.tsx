'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, RotateCcw, Save } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Controller, useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, type Item, type UpdateItemInput } from '@/lib/api'
import { prettyJSON } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, qk, syncItemInCache, useCollections } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'

/** Statuses that can be set directly; approved/rejected go through review actions. */
const SETTABLE = ['draft', 'pending', 'processing', 'completed', 'failed', 'archived'] as const

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
    title: z.string().max(500, 'At most 500 characters'),
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

  if (v.status !== item.status) {
    patch.status = v.status as UpdateItemInput['status']
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
  const qc = useQueryClient()
  const collections = useCollections(true)
  const [item, setBase] = useState(latest)
  const [conflict, setConflict] = useState<string | null>(null)
  const form = useForm<FormValues>({ resolver: zodResolver(schema), defaultValues: toForm(latest) })
  const { register, handleSubmit, formState, control, reset } = form
  const decided = item.status === 'approved' || item.status === 'rejected'
  const isDirty = formState.isDirty

  useEffect(() => {
    if (latest.version !== item.version && !isDirty) {
      // Syncing from the query cache (an external store) into the form's base version.
      // oxlint-disable-next-line react/set-state-in-effect
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
      toast.success('Item saved')
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) {
        setConflict(err.message)

        return
      }

      if (err instanceof ApiError && err.fields) {
        for (const [k, msg] of Object.entries(err.fields)) {
          if (k in form.getValues()) {
            form.setError(k as keyof FormValues, { message: msg })
          }
        }
      }

      toastApiError(err)
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
    <form onSubmit={onSubmit} className="grid gap-5" data-testid="item-edit-form">
      {conflict && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm"
        >
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600" />
          <div className="flex-1">
            <div className="font-medium">Someone else changed this item</div>
            <div className="text-muted-foreground">{conflict}. Reload the latest version and re-apply your edits.</div>
          </div>
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
        </div>
      )}
      <FieldGroup className="gap-4">
        <Field data-invalid={!!formState.errors.title}>
          <FieldLabel htmlFor="item-title">Title</FieldLabel>
          <Input id="item-title" {...register('title')} aria-invalid={!!formState.errors.title} />
          <FieldError errors={[formState.errors.title]} />
        </Field>
        {(item.type === 'link' ||
          item.type === 'image' ||
          item.type === 'video' ||
          item.type === 'file' ||
          item.url) && (
          <Field data-invalid={!!formState.errors.url}>
            <FieldLabel htmlFor="item-url">URL</FieldLabel>
            <Input
              id="item-url"
              {...register('url')}
              className="font-mono text-xs"
              placeholder="https://…"
              aria-invalid={!!formState.errors.url}
            />
            <FieldError errors={[formState.errors.url]} />
          </Field>
        )}
        <Field data-invalid={!!formState.errors.content}>
          <FieldLabel htmlFor="item-content">{item.type === 'json' ? 'JSON content' : 'Content'}</FieldLabel>
          <Textarea
            id="item-content"
            rows={item.type === 'text' || item.type === 'json' ? 10 : 3}
            {...register('content')}
            className={item.type === 'json' ? 'font-mono text-xs' : undefined}
            aria-invalid={!!formState.errors.content}
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
          <Field>
            <FieldLabel>Status</FieldLabel>
            <Controller
              control={control}
              name="status"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger className="w-full" aria-label="Status">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {decided && (
                      <SelectItem value={item.status} disabled>
                        {STATUS_META[item.status].label} (decided)
                      </SelectItem>
                    )}
                    {SETTABLE.map((s) => (
                      <SelectItem key={s} value={s}>
                        {STATUS_META[s].label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
            <FieldDescription>Approve/reject via review actions.</FieldDescription>
          </Field>
          <Field>
            <FieldLabel>Collection</FieldLabel>
            <Controller
              control={control}
              name="collection_id"
              render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange}>
                  <SelectTrigger className="w-full" aria-label="Collection">
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
          </Field>
          <Field data-invalid={!!formState.errors.source}>
            <FieldLabel htmlFor="item-source">Source</FieldLabel>
            <Input id="item-source" {...register('source')} placeholder="e.g. n8n" />
            <FieldError errors={[formState.errors.source]} />
          </Field>
          <Field data-invalid={!!formState.errors.external_ref}>
            <FieldLabel htmlFor="item-external-ref">External ref</FieldLabel>
            <Input id="item-external-ref" {...register('external_ref')} className="font-mono text-xs" />
            <FieldError errors={[formState.errors.external_ref]} />
          </Field>
        </div>
      </FieldGroup>
      <div className="flex items-center justify-end gap-2">
        <Button type="button" variant="ghost" disabled={!formState.isDirty} onClick={() => reset(toForm(item))}>
          Discard
        </Button>
        <Button type="submit" disabled={save.isPending}>
          <Save /> {save.isPending ? 'Saving…' : 'Save changes'}
        </Button>
      </div>
    </form>
  )
}
