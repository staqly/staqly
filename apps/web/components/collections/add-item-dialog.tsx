'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Braces, FileText, Link2, Upload } from 'lucide-react'
import { useRef, useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, type CreateItemInput, type Item } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, qk } from '@/lib/queries'
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
  const [errors, setErrors] = useState<Record<string, string>>({})
  const fileInput = useRef<HTMLInputElement>(null)

  const create = useMutation({
    mutationFn: (args: { input: CreateItemInput; file?: File }) =>
      args.file ? api.uploadItem(collectionId, args.file, args.input) : api.createItem(collectionId, args.input),
    onSuccess: (item) => {
      void qc.invalidateQueries({ queryKey: qk.itemsAll })
      invalidateAfterItemChange(qc, undefined, { usage: true })
      toast.success(`Added “${item.title}”`)
      onDone(item)
    },
    onError: (err) => {
      if (err instanceof ApiError && err.fields) {
        setErrors(err.fields)
      }

      toastApiError(err)
    },
  })

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const errs: Record<string, string> = {}
    const meta = parseMetadata(metadata)

    if (meta instanceof Error) {
      errs.metadata = meta.message
    }

    const input: CreateItemInput = { status, source: 'dashboard' }

    if (title.trim()) {
      input.title = title.trim()
    }

    if (meta && !(meta instanceof Error)) {
      input.metadata = meta
    }

    if (mode === 'file') {
      if (!file) {
        errs.file = 'Choose a file to upload'
      }
    } else if (mode === 'text') {
      if (!content.trim()) {
        errs.content = 'Write some text'
      }

      input.type = 'text'
      input.content = content
    } else if (mode === 'link') {
      if (!/^https?:\/\/\S+$/i.test(url.trim())) {
        errs.url = 'Enter an absolute http(s) URL'
      }

      input.type = 'link'
      input.url = url.trim()

      if (content.trim()) {
        input.content = content
      }
    } else if (mode === 'json') {
      try {
        input.content = JSON.parse(content)
        input.type = 'json'
      } catch {
        errs.content = 'Not valid JSON'
      }
    }

    setErrors(errs)

    if (Object.keys(errs).length) {
      return
    }

    create.mutate({ input, file: mode === 'file' ? file! : undefined })
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
          setErrors({})
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
        <div className="grid gap-2">
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
              type="file"
              className="sr-only"
              data-testid="file-input"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          </div>
          {errors.file && <p className="text-sm text-destructive">{errors.file}</p>}
        </div>
      )}

      {mode === 'link' && (
        <div className="grid gap-2">
          <Label htmlFor="add-url">URL</Label>
          <Input
            id="add-url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://…"
            className="font-mono text-sm"
            aria-invalid={!!errors.url}
          />
          {errors.url && <p className="text-sm text-destructive">{errors.url}</p>}
        </div>
      )}

      <div className="grid gap-2">
        <Label htmlFor="add-title">
          Title {mode !== 'text' && <span className="font-normal text-muted-foreground">(optional)</span>}
        </Label>
        <Input
          id="add-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={mode === 'text' ? 'Defaults to the first line' : ''}
        />
      </div>

      {(mode === 'text' || mode === 'json' || mode === 'link') && (
        <div className="grid gap-2">
          <Label htmlFor="add-content">
            {mode === 'json' ? 'JSON' : mode === 'link' ? 'Description (optional)' : 'Text'}
          </Label>
          <Textarea
            id="add-content"
            rows={mode === 'link' ? 3 : 8}
            value={content}
            onChange={(e) => setContent(e.target.value)}
            spellCheck={mode !== 'json'}
            className={cn('max-h-[40vh]', mode === 'json' && 'font-mono text-xs')}
            placeholder={mode === 'json' ? '{\n  "caption": "…"\n}' : ''}
            aria-invalid={!!errors.content}
          />
          {errors.content && <p className="text-sm text-destructive">{errors.content}</p>}
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-[160px_1fr]">
        <div className="grid gap-2">
          <Label>Status</Label>
          <Select value={status} onValueChange={(v) => setStatus(v as 'pending' | 'draft')}>
            <SelectTrigger className="w-full" aria-label="Initial status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="pending">Pending review</SelectItem>
              <SelectItem value="draft">Draft</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="add-metadata">
            Metadata <span className="font-normal text-muted-foreground">(JSON, optional)</span>
          </Label>
          <Input
            id="add-metadata"
            value={metadata}
            onChange={(e) => setMetadata(e.target.value)}
            placeholder='{"prompt": "…"}'
            className="font-mono text-xs"
            aria-invalid={!!errors.metadata}
          />
          {errors.metadata && <p className="text-sm text-destructive">{errors.metadata}</p>}
        </div>
      </div>

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? (mode === 'file' ? 'Uploading…' : 'Adding…') : 'Add item'}
        </Button>
      </DialogFooter>
    </form>
  )
}
