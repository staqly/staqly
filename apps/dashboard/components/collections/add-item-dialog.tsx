'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Braces, FileText, Link2, Upload } from 'lucide-react'
import { useRef, useState } from 'react'

import { ChipsInput } from '@/components/common/chips-input'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { api, type CreateItemInput, type Item } from '@/lib/api'
import { charCount } from '@/lib/chars'
import { focusFirstInvalid, formApiError, useFormErrors } from '@/lib/form-errors'
import { formatBytes } from '@/lib/format'
import { namedToast } from '@/lib/notify'
import { invalidateAfterItemChange, refetchItemListsFromStart } from '@/lib/queries'
import { cn } from '@/lib/utils'

type Mode = 'file' | 'text' | 'link' | 'json'

const MODES: { value: Mode; label: string; Icon: typeof Upload }[] = [
  { value: 'file', label: 'Upload', Icon: Upload },
  { value: 'text', label: 'Text', Icon: FileText },
  { value: 'link', label: 'Link', Icon: Link2 },
  { value: 'json', label: 'JSON', Icon: Braces },
]

function parseMetadata(raw: string): Record<string, unknown> | undefined | Error {
  if (!raw.trim()) {
    return undefined
  }

  try {
    const v = JSON.parse(raw)

    if (v === null || typeof v !== 'object' || Array.isArray(v)) {
      return new Error('Metadata must be a JSON object')
    }

    return v
  } catch {
    return new Error('Metadata is not valid JSON')
  }
}

function parseJSON(raw: string): { ok: true; value: unknown } | { ok: false } {
  try {
    return { ok: true, value: JSON.parse(raw) }
  } catch {
    return { ok: false }
  }
}

type ErrorKey = 'file' | 'url' | 'title' | 'content' | 'metadata' | 'status'

interface Draft {
  mode: Mode
  file: File | null
  title: string
  content: string
  url: string
  status: 'pending' | 'draft'
  metadata: string
  tags?: string[]
}

/** Client-side checks plus the request body they allow. */
function validate(d: Draft): { errors: Partial<Record<ErrorKey, string>>; input: CreateItemInput } {
  const errors: Partial<Record<ErrorKey, string>> = {}
  const meta = parseMetadata(d.metadata)
  const input: CreateItemInput = { status: d.status, source: 'dashboard' }

  if (d.tags?.length) {
    input.tags = d.tags
  }

  if (meta instanceof Error) {
    errors.metadata = meta.message
  } else if (meta) {
    input.metadata = meta
  }

  if (!d.title.trim()) {
    errors.title = 'Title is required'
  } else if (charCount(d.title.trim()) > 500) {
    errors.title = 'At most 500 characters'
  } else {
    input.title = d.title.trim()
  }

  if (d.mode === 'file') {
    if (!d.file) {
      errors.file = 'Choose a file to upload'
    }
  } else if (d.mode === 'text') {
    if (!d.content.trim()) {
      errors.content = 'Write some text'
    }

    input.type = 'text'
    input.content = d.content
  } else if (d.mode === 'link') {
    if (!/^https?:\/\/\S+$/i.test(d.url.trim())) {
      errors.url = d.url.trim() ? 'Enter an absolute http(s) URL' : 'URL is required'
    }

    input.type = 'link'
    input.url = d.url.trim()

    if (d.content.trim()) {
      input.content = d.content
    }
  } else {
    const parsed = d.content.trim() ? parseJSON(d.content) : null

    if (!parsed) {
      errors.content = 'JSON is required'
    } else if (!parsed.ok) {
      errors.content = 'Not valid JSON'
    } else {
      input.type = 'json'
      input.content = parsed.value
    }
  }

  return { errors, input }
}

const API_FIELDS: Record<string, ErrorKey> = {
  file: 'file',
  media_id: 'file',
  url: 'url',
  title: 'title',
  content: 'content',
  type: 'content',
  metadata: 'metadata',
  status: 'status',
}

export function AddItemDialog({
  collectionId,
  open,
  onOpenChange,
  onCreated,
}: {
  collectionId: string
  open: boolean
  onOpenChange: (o: boolean) => void
  onCreated?: (item: Item) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {open && (
          <AddItemForm
            collectionId={collectionId}
            onDone={(it) => {
              onOpenChange(false)
              onCreated?.(it)
            }}
            onCancel={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function AddItemForm({
  collectionId,
  onDone,
  onCancel,
}: {
  collectionId: string
  onDone: (item: Item) => void
  onCancel: () => void
}) {
  const qc = useQueryClient()
  const [mode, setMode] = useState<Mode>('file')
  const [file, setFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [url, setUrl] = useState('')
  const [status, setStatus] = useState<'pending' | 'draft'>('pending')
  const [metadata, setMetadata] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const fileInput = useRef<HTMLInputElement>(null)
  const checked = validate({ mode, file, title, content, url, status, metadata, tags })
  const form = useFormErrors(checked.errors)
  const errors = form.errors

  const create = useMutation({
    mutationFn: (args: { input: CreateItemInput; file?: File }) =>
      args.file ? api.uploadItem(collectionId, args.file, args.input) : api.createItem(collectionId, args.input),
    onSuccess: (item) => {
      refetchItemListsFromStart(qc, item.id)
      invalidateAfterItemChange(qc, undefined, { usage: true })
      namedToast(
        item.title,
        'was added',
        item.status === 'draft' ? 'It is a draft: not in the inbox yet.' : 'It is waiting for review in the inbox.',
      )
      onDone(item)
    },
    onError: (err) => form.setServerErrors(formApiError(err, (k) => API_FIELDS[k])),
  })

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()

    if (!form.submit()) {
      focusFirstInvalid(e.currentTarget)

      return
    }

    create.mutate({ input: checked.input, file: mode === 'file' ? file! : undefined })
  }

  return (
    <form onSubmit={submit} className="grid gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>Add item</DialogTitle>
        <DialogDescription>
          Upload a file or paste content — the same thing your agents do via the API.
        </DialogDescription>
      </DialogHeader>
      <Tabs
        value={mode}
        onValueChange={(v) => {
          setMode(v as Mode)
          form.reset()
        }}
      >
        <TabsList className="w-full">
          {MODES.map(({ value, label, Icon }) => (
            <TabsTrigger key={value} value={value} className="flex-1">
              <Icon /> {label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      {mode === 'file' && (
        <Field data-invalid={!!errors.file}>
          <FieldLabel htmlFor="add-file" required>
            File
          </FieldLabel>
          {/* oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- mouse/drag affordance only; the sr-only file input inside is keyboard-focusable */}
          <div
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault()
              setDragging(false)
              const f = e.dataTransfer.files?.[0]

              if (f) {
                setFile(f)
                form.touch('file')
              }
            }}
            onClick={() => fileInput.current?.click()}
            className={cn(
              'flex cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border border-dashed px-4 py-8 text-center text-sm transition-colors',
              dragging ? 'border-brand bg-brand-soft' : 'hover:bg-muted/50',
              errors.file && 'border-destructive',
            )}
          >
            <Upload className="mb-1 size-5 text-muted-foreground" />
            {file ? (
              <>
                <span className="font-medium">{file.name}</span>
                <span className="text-xs text-muted-foreground">
                  {file.type || 'unknown type'} · {formatBytes(file.size)}
                </span>
              </>
            ) : (
              <>
                <span className="font-medium">Drop a file or click to browse</span>
                <span className="text-xs text-muted-foreground">
                  Images and videos get previews; other files are downloadable
                </span>
              </>
            )}
            <input
              ref={fileInput}
              id="add-file"
              type="file"
              className="sr-only"
              data-testid="file-input"
              aria-label="File"
              aria-invalid={!!errors.file}
              aria-required
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null)
                form.touch('file')
              }}
            />
          </div>
          <FieldError>{errors.file}</FieldError>
        </Field>
      )}

      {mode === 'link' && (
        <Field data-invalid={!!errors.url}>
          <FieldLabel htmlFor="add-url" required>
            URL
          </FieldLabel>
          <Input
            id="add-url"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value)
              form.touch('url')
            }}
            placeholder="https://..."
            className="font-mono text-sm"
            aria-invalid={!!errors.url}
            aria-required
          />
          <FieldError>{errors.url}</FieldError>
        </Field>
      )}

      <Field data-invalid={!!errors.title}>
        <FieldLabel htmlFor="add-title" required>
          Title
        </FieldLabel>
        <Input
          id="add-title"
          value={title}
          onChange={(e) => {
            setTitle(e.target.value)
            form.touch('title')
          }}
          placeholder="e.g. Instagram avatar v1"
          aria-invalid={!!errors.title}
          aria-required
        />
        <FieldError>{errors.title}</FieldError>
      </Field>

      {(mode === 'text' || mode === 'json' || mode === 'link') && (
        <Field data-invalid={!!errors.content}>
          <FieldLabel htmlFor="add-content" required={mode !== 'link'}>
            {mode === 'json' ? 'JSON' : mode === 'link' ? 'Description (optional)' : 'Text'}
          </FieldLabel>
          <Textarea
            id="add-content"
            rows={mode === 'link' ? 3 : 8}
            value={content}
            onChange={(e) => {
              setContent(e.target.value)
              form.touch('content')
            }}
            spellCheck={mode !== 'json'}
            className={cn('max-h-[40vh]', mode === 'json' && 'font-mono text-xs')}
            placeholder={mode === 'json' ? '{\n  "caption": "..."\n}' : ''}
            aria-invalid={!!errors.content}
            aria-required={mode !== 'link'}
          />
          <FieldError>{errors.content}</FieldError>
        </Field>
      )}

      <div className="grid gap-4 sm:grid-cols-[160px_1fr]">
        <Field data-invalid={!!errors.status}>
          <FieldLabel htmlFor="add-status">Status</FieldLabel>
          <Select
            value={status}
            onValueChange={(v) => {
              setStatus(v as 'pending' | 'draft')
              form.touch('status')
            }}
          >
            <SelectTrigger
              id="add-status"
              className="w-full"
              aria-label="Initial status"
              aria-invalid={!!errors.status}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="pending">Pending review</SelectItem>
              <SelectItem value="draft">Draft</SelectItem>
            </SelectContent>
          </Select>
          <FieldError>{errors.status}</FieldError>
        </Field>
        <Field data-invalid={!!errors.metadata}>
          <FieldLabel htmlFor="add-metadata">
            Metadata <span className="font-normal text-muted-foreground">(JSON, optional)</span>
          </FieldLabel>
          <Input
            id="add-metadata"
            value={metadata}
            onChange={(e) => {
              setMetadata(e.target.value)
              form.touch('metadata')
            }}
            placeholder='{"prompt": "..."}'
            className="font-mono text-xs"
            aria-invalid={!!errors.metadata}
          />
          <FieldError>{errors.metadata}</FieldError>
        </Field>
      </div>
      <Field>
        <FieldLabel htmlFor="add-tags">
          Tags <span className="font-normal text-muted-foreground">(optional)</span>
        </FieldLabel>
        <ChipsInput
          id="add-tags"
          label="Tags"
          value={tags}
          onChange={setTags}
          placeholder="Add a tag, press Enter"
          max={20}
          maxLength={40}
        />
      </Field>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? (mode === 'file' ? 'Uploading...' : 'Adding...') : 'Add item'}
        </Button>
      </DialogFooter>
    </form>
  )
}
