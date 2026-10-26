'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Clapperboard,
  Download,
  FileText,
  Folder,
  Maximize2,
  Music,
  Palette,
  PenSquare,
  Trash2,
  Undo2,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { CopyButton } from '@/components/common/copy-button'
import { MultiSelect } from '@/components/common/multi-select'
import { PostStatusBadge } from '@/components/common/status-badge'
import {
  invalidateLists,
  mediaKeys,
  patchMedia,
  refreshMediaCounts,
  removeMedia,
  restoreInCache,
} from '@/components/media/media-cache'
import { FolderPickerDialog } from '@/components/media/media-folder-dialogs'
import { folderPath } from '@/components/media/media-folders'
import { deletedToast } from '@/components/media/media-trash'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { useAiStatus } from '@/lib/ai-status'
import { api, mediaThumbUrl, mediaUrl, type MediaLibraryItem, type MediaPatch } from '@/lib/api'
import { formatBytes, formatDateTime, timeAgo } from '@/lib/format'
import {
  cropShare,
  fitVerdict,
  formatDuration,
  mediaKind,
  PLACEMENTS,
  ratioLabel,
  shapeName,
  typeLabel,
} from '@/lib/media'
import { toastApiError } from '@/lib/notify'
import { ProviderIcon } from '@/lib/providers'
import { useRole } from '@/lib/queries'
import { normalizeTag } from '@/lib/tags'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

/** Transparent pictures read on a checkerboard; the theme's own colours keep it calm in light and dark. */
const CHECKER = {
  background: 'repeating-conic-gradient(var(--muted) 0 25%, var(--background) 0 50%) 0 0 / 16px 16px',
}

/** "med_7utw...j6q": long IDs keep their start and end. */
const shortId = (id: string) => (id.length > 16 ? `${id.slice(0, 8)}...${id.slice(-4)}` : id)

/**
 * A file's drawer: preview, facts and actions on top, then tabs (File, Details, Where it fits, Used in), Delete,
 * Close and Save changes at the bottom. `onOpenMedia` opens another file (the oldest copy of a duplicate).
 */
const AiVideoDialog = dynamic(() => import('@/components/media/ai-video-dialog').then((m) => m.AiVideoDialog), {
  ssr: false,
})

export function MediaDetail({
  item,
  onOpenChange,
  onNewPost,
  onOpenMedia,
}: {
  item: MediaLibraryItem | null
  onOpenChange: (open: boolean) => void
  onNewPost: (item: MediaLibraryItem) => void
  onOpenMedia?: (id: string) => void
}) {
  return (
    <Sheet open={!!item} onOpenChange={onOpenChange}>
      <SheetContent
        className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl"
        data-testid="media-detail"
        // Focus stays on the drawer itself, so it opens at the preview instead of scrolling to the first button.
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        {item && (
          <DetailForm
            key={item.id}
            item={item}
            onClose={() => onOpenChange(false)}
            onNewPost={onNewPost}
            onOpenMedia={onOpenMedia}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

const sameTags = (a: string[], b: string[]) => a.length === b.length && a.every((t, i) => t === b[i])

type Tab = 'file' | 'details' | 'fits' | 'used'
type Saved = { name: string; alt: string; folder: string; tags: string[] }

function DetailForm({
  item,
  onClose,
  onNewPost,
  onOpenMedia,
}: {
  item: MediaLibraryItem
  onClose: () => void
  onNewPost: (item: MediaLibraryItem) => void
  onOpenMedia?: (id: string) => void
}) {
  const qc = useQueryClient()
  const { canWrite } = useRole()
  const initial = {
    name: item.original_name,
    alt: item.alt_text ?? '',
    folder: item.folder_id ?? '',
    tags: item.tags ?? [],
  }
  // What is saved: the drawer stays open after a save, so it compares against the server's answer.
  const [saved, setSaved] = useState(initial)
  const [name, setName] = useState(initial.name)
  const [alt, setAlt] = useState(initial.alt)
  const [folder, setFolder] = useState(initial.folder)
  const [tags, setTags] = useState(initial.tags)
  const [tab, setTab] = useState<Tab>('file')
  // Errors show from the first Save on, then follow every change (the dashboard's form pattern).
  const [submitted, setSubmitted] = useState(false)
  const nameError = submitted && !name.trim() ? 'Give the file a name.' : undefined
  const [picking, setPicking] = useState(false)
  const [purging, setPurging] = useState(false)
  const animate = useLazyDialog()
  const ai = useAiStatus()
  const kind = mediaKind(item.mime_type)
  const visual = kind === 'image' || kind === 'video'
  const trashed = !!item.deleted_at
  const usage = useQuery({ queryKey: ['media', 'usage', item.id], queryFn: () => api.mediaUsage(item.id) })
  const tree = useQuery({ queryKey: mediaKeys.folders, queryFn: () => api.mediaFolders() })
  const folders = tree.data?.data ?? []
  const uses = usage.data?.data ?? []
  const dirty =
    name.trim() !== saved.name || alt.trim() !== saved.alt || folder !== saved.folder || !sameTags(tags, saved.tags)
  const editable = canWrite && !trashed
  const fits = item.width && item.height ? fitRows(item.width, item.height) : []
  const fitCount = fits.filter((r) => r.verdict.tone === 'fits').length

  const save = useMutation({
    mutationFn: (sent: Saved) => {
      const patch: MediaPatch = { name: sent.name, alt_text: sent.alt }

      if (sent.folder !== saved.folder) {
        patch.folder_id = sent.folder
      }

      if (!sameTags(sent.tags, saved.tags)) {
        patch.tags = sent.tags
      }

      return api.updateMedia(item.id, patch)
    },
    onSuccess: (m, sent) => {
      const next = { name: m.original_name, alt: m.alt_text ?? '', folder: m.folder_id ?? '', tags: m.tags ?? [] }

      setSaved(next)
      // The fields stay editable while saving: what was typed meanwhile stays, only what was sent takes the answer.
      setName((cur) => (cur.trim() === sent.name ? next.name : cur))
      setAlt((cur) => (cur.trim() === sent.alt ? next.alt : cur))
      setFolder((cur) => (cur === sent.folder ? next.folder : cur))
      setTags((cur) => (sameTags(cur, sent.tags) ? next.tags : cur))
      patchMedia(qc, [m.id], (old) => ({ ...old, ...m }))
      // Lists the file may now belong to: another folder or tag, a search or a by-name order.
      invalidateLists(
        qc,
        (query) =>
          (!!query.folder && next.folder !== saved.folder) ||
          (!!query.tag && !sameTags(next.tags, saved.tags)) ||
          (next.name !== saved.name && (!!query.q || query.sort === 'name_asc')),
      )

      if (next.folder !== saved.folder || !sameTags(next.tags, saved.tags)) {
        void qc.invalidateQueries({ queryKey: mediaKeys.folders, exact: true })
      }

      toast.success('Saved')
    },
    onError: (e) => toastApiError(e),
  })
  const remove = useMutation({
    mutationFn: () => api.deleteMedia(item.id),
    onSuccess: () => {
      deletedToast(qc, [item.id], undefined, item.original_name)
      onClose()
    },
    onError: (e) => toastApiError(e),
  })
  const restore = useMutation({
    mutationFn: () => api.restoreMedia(item.id),
    onSuccess: (m) => {
      restoreInCache(qc, [m], new Map([[item.id, item]]))
      toast.success(`Restored “${item.original_name}”`)
      onClose()
    },
    onError: (e) => toastApiError(e),
  })
  const purge = useMutation({
    mutationFn: () => api.deleteMedia(item.id, { permanent: true }),
    onSuccess: () => {
      removeMedia(qc, [item.id])
      invalidateLists(qc, (query) => !!query.duplicates)
      refreshMediaCounts(qc)
      toast.success(`Deleted “${item.original_name}” for good`)
      onClose()
    },
    onError: (e) => toastApiError(e),
  })

  const newPost = () => onNewPost({ ...item, original_name: saved.name, alt_text: saved.alt })
  const facts = [
    typeLabel(item.mime_type).split(' ')[0],
    formatBytes(item.size_bytes),
    ...(item.width && item.height ? [`${item.width} × ${item.height}`, ratioLabel(item.width, item.height)] : []),
    ...(item.duration_ms ? [formatDuration(item.duration_ms)] : []),
  ]
  const badge = 'rounded-full bg-muted px-1.5 text-[11px] leading-4 font-normal tabular-nums'

  return (
    <>
      <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
        <div className="flex min-w-0 items-start gap-2">
          <div className="grid min-w-0 flex-1 gap-1.5">
            <SheetTitle className="truncate" title={item.original_name}>
              {item.original_name}
            </SheetTitle>
            <SheetDescription asChild>
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <span className={cn(trashed && TONES.danger.text)}>
                  {trashed
                    ? `In Deleted: deleted for good ${timeAgo(item.purge_at)}`
                    : kind === 'image'
                      ? 'Picture'
                      : kind === 'video'
                        ? 'Video'
                        : kind === 'audio'
                          ? 'Audio'
                          : 'File'}
                </span>
                <span
                  className="inline-flex max-w-full min-w-0 items-center gap-0.5 rounded-md bg-muted pl-1.5 ring-1 ring-border/60 ring-inset"
                  data-testid="media-id"
                >
                  <code
                    className="truncate font-mono text-[11px] text-foreground/80"
                    title={item.id}
                    aria-label={item.id}
                  >
                    {shortId(item.id)}
                  </code>
                  <CopyButton
                    value={item.id}
                    label="Copy file ID"
                    size="icon-xs"
                    className="text-muted-foreground"
                    onCopied={() => toast.success('Copied', { description: item.id })}
                  />
                </span>
              </div>
            </SheetDescription>
          </div>
          {/* What you can do with the file: icon buttons beside the close button, like a post's header. */}
          <div className="flex shrink-0 items-center gap-0.5" data-testid="media-actions">
            {editable && visual && (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="New post with this"
                tooltip="New post with this"
                onClick={newPost}
              >
                <PenSquare />
              </Button>
            )}
            {editable && kind === 'image' && ai.data?.operations.some((o) => o.id === 'image_to_video') && (
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label="Animate with AI"
                tooltip="Animate with AI"
                onClick={() => animate.setOpen(true)}
              >
                <Clapperboard />
              </Button>
            )}
            {editable && kind === 'image' && (
              <Button variant="ghost" size="icon-sm" aria-label="Edit" tooltip="Edit in the design editor" asChild>
                <a href={`/media/edit?ids=${item.id}`}>
                  <Palette />
                </a>
              </Button>
            )}
            <Button variant="ghost" size="icon-sm" aria-label="Download" tooltip="Download" asChild>
              <a href={mediaUrl(item.id, { download: true })} download={item.original_name}>
                <Download />
              </a>
            </Button>
          </div>
        </div>
      </SheetHeader>
      {animate.used && <AiVideoDialog open={animate.open} onOpenChange={animate.setOpen} from={item} />}

      <form
        className="flex min-h-0 flex-1 flex-col"
        onSubmit={(e) => {
          e.preventDefault()

          setSubmitted(true)

          if (!name.trim()) {
            setTab('file')
            requestAnimationFrame(() => document.getElementById('media-name')?.focus())

            return
          }

          save.mutate({ name: name.trim(), alt: alt.trim(), folder, tags })
        }}
      >
        <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="block min-h-0 flex-1 overflow-y-auto">
          <div className="grid gap-3 p-4 pb-3">
            <Preview item={item} />
            <p
              className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground"
              data-testid="media-facts"
            >
              <span>{facts.join(' · ')}</span>
              <span aria-hidden>·</span>
              <span title={formatDateTime(item.created_at)}>Uploaded {timeAgo(item.created_at)}</span>
            </p>
          </div>

          {/* The tabs stay under the header while the drawer scrolls. */}
          <div className="sticky top-0 z-10 overflow-x-auto bg-popover px-4 py-2">
            <TabsList aria-label="File sections">
              <TabsTrigger value="file">File</TabsTrigger>
              <TabsTrigger value="details">Details</TabsTrigger>
              {fits.length > 0 && (
                <TabsTrigger value="fits">
                  Where it fits
                  <span className={badge} aria-label={`fits ${fitCount} of ${fits.length}`}>
                    {fitCount}/{fits.length}
                  </span>
                </TabsTrigger>
              )}
              <TabsTrigger value="used">
                Used in
                <span className={badge}>{item.uses}</span>
              </TabsTrigger>
            </TabsList>
          </div>

          <TabsContent value="file" className="grid content-start gap-4 p-4">
            <Field data-invalid={!!nameError || undefined}>
              <FieldLabel htmlFor="media-name" required>
                Name
              </FieldLabel>
              <Input
                id="media-name"
                value={name}
                maxLength={200}
                disabled={!editable}
                aria-required
                aria-invalid={!!nameError || undefined}
                onChange={(e) => setName(e.target.value)}
              />
              <FieldError>{nameError}</FieldError>
            </Field>
            {visual && (
              <Field>
                <FieldLabel htmlFor="media-alt">Alt text</FieldLabel>
                <Textarea
                  id="media-alt"
                  value={alt}
                  maxLength={1500}
                  rows={3}
                  disabled={!editable}
                  placeholder="A runner crossing the finish line at sunset"
                  onChange={(e) => setAlt(e.target.value)}
                />
                <FieldDescription>
                  Describes the file for people who can&apos;t see it. Platforms that take a description receive it.
                </FieldDescription>
              </Field>
            )}
            <Field>
              <FieldLabel htmlFor="media-tags">Tags</FieldLabel>
              <MultiSelect
                id="media-tags"
                label="Tags"
                placeholder="Add tags"
                className="w-full"
                value={tags}
                onChange={setTags}
                disabled={!editable}
                options={(tree.data?.tags ?? []).map((t) => ({ value: t.tag, label: `#${t.tag}` }))}
                creatable={{ normalize: normalizeTag }}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="media-folder">Folder</FieldLabel>
              <Button
                id="media-folder"
                type="button"
                variant="outline"
                className="w-full justify-start font-normal"
                disabled={!editable}
                onClick={() => setPicking(true)}
              >
                <Folder className="text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate text-left">
                  {folder ? folderPath(folders, folder) : 'No folder'}
                </span>
                <span className="text-xs text-muted-foreground">Move...</span>
              </Button>
            </Field>
          </TabsContent>

          <TabsContent value="details" className="p-4">
            <Details item={item} onOpenMedia={onOpenMedia} />
          </TabsContent>

          {fits.length > 0 && (
            <TabsContent value="fits" className="p-4">
              <Fits item={item} rows={fits} />
            </TabsContent>
          )}

          <TabsContent value="used" className="p-4">
            {usage.isLoading ? (
              <p className="text-sm text-muted-foreground">Loading</p>
            ) : uses.length === 0 ? (
              <div className="grid justify-items-start gap-3">
                <p className="text-sm text-muted-foreground">No post uses this file yet.</p>
                {editable && visual && (
                  <Button type="button" variant="outline" onClick={newPost}>
                    <PenSquare /> New post with this
                  </Button>
                )}
              </div>
            ) : (
              <ul className="grid divide-y" data-testid="media-used-in">
                {uses.map((u) => (
                  <li key={u.post_id} className="flex min-h-10 items-center gap-3 py-1.5 text-sm">
                    <Link
                      href={`/posts/${u.post_id}`}
                      prefetch={false}
                      className="min-w-0 flex-1 truncate underline-offset-4 hover:underline pointer-coarse:py-2.5"
                    >
                      {u.title || 'Untitled post'}
                    </Link>
                    {u.publish_at && (
                      <time
                        dateTime={u.publish_at}
                        title={formatDateTime(u.publish_at)}
                        className="shrink-0 text-xs text-muted-foreground"
                      >
                        {timeAgo(u.publish_at)}
                      </time>
                    )}
                    <PostStatusBadge status={u.status} />
                  </li>
                ))}
              </ul>
            )}
          </TabsContent>
        </Tabs>

        <div className="flex flex-wrap items-center gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          {canWrite &&
            (trashed ? (
              <>
                <Button type="button" variant="outline" onClick={() => restore.mutate()} disabled={restore.isPending}>
                  <Undo2 /> Restore
                </Button>
                <Button type="button" variant="destructive" onClick={() => setPurging(true)} disabled={purge.isPending}>
                  <Trash2 /> Delete forever
                </Button>
              </>
            ) : (
              <Button type="button" variant="destructive" onClick={() => remove.mutate()} disabled={remove.isPending}>
                <Trash2 /> Delete file
              </Button>
            ))}
          <span className="ml-auto flex gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Close
            </Button>
            {editable && (
              <Button type="submit" disabled={!dirty || save.isPending}>
                {save.isPending ? 'Saving...' : 'Save changes'}
              </Button>
            )}
          </span>
        </div>
      </form>

      {picking && (
        <FolderPickerDialog
          open
          onOpenChange={setPicking}
          title="Move to folder"
          folders={folders}
          current={folder}
          onPick={setFolder}
        />
      )}
      <ConfirmDialog
        open={purging}
        onOpenChange={setPurging}
        title={`Delete “${item.original_name}” for good?`}
        description={
          uses.length > 0
            ? `${uses.length} ${uses.length === 1 ? 'post uses' : 'posts use'} this file and will lose it. This can't be undone.`
            : "The file is removed for good. This can't be undone."
        }
        confirmLabel="Delete forever"
        variant="destructive"
        onConfirm={() => purge.mutate()}
      />
    </>
  )
}

/** The file's facts: muted label on the left, the value on the right, 32 px rows with hairlines between. */
function Details({ item, onOpenMedia }: { item: MediaLibraryItem; onOpenMedia?: (id: string) => void }) {
  const rows: [string, React.ReactNode][] = [
    [
      'Type',
      <>
        {typeLabel(item.mime_type)} <span className="text-muted-foreground">{item.mime_type}</span>
      </>,
    ],
    ['Size', formatBytes(item.size_bytes)],
  ]

  if (item.width && item.height) {
    rows.push(['Dimensions', `${item.width} × ${item.height} px`])
    rows.push(['Aspect ratio', `${ratioLabel(item.width, item.height)} · ${shapeName(item.width, item.height)}`])
  }

  if (item.duration_ms) {
    rows.push(['Duration', formatDuration(item.duration_ms)])
  }

  rows.push([
    'Uploaded',
    <>
      {formatDateTime(item.created_at)} <span className="text-muted-foreground">{timeAgo(item.created_at)}</span>
    </>,
  ])
  rows.push([
    'Fingerprint',
    <span key="sha" className="flex min-w-0 items-center gap-0.5">
      <code className="truncate font-mono text-xs" title={`SHA-256 ${item.sha256}`}>
        {item.sha256.slice(0, 12)}
      </code>
      <CopyButton value={item.sha256} label="Copy fingerprint" size="icon-xs" className="text-muted-foreground" />
    </span>,
  ])

  if (item.duplicate_of) {
    const twin = item.duplicate_of

    rows.push([
      'Duplicate of',
      onOpenMedia ? (
        <button
          key="twin"
          type="button"
          onClick={() => onOpenMedia(twin)}
          className="truncate font-mono text-xs underline-offset-4 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {shortId(twin)}
        </button>
      ) : (
        <code key="twin" className="font-mono text-xs">
          {shortId(twin)}
        </code>
      ),
    ])
  }

  return (
    <dl className="grid divide-y text-sm" data-testid="media-details">
      {rows.map(([label, value]) => (
        <div key={label} className="grid min-h-8 grid-cols-[7.5rem_minmax(0,1fr)] items-center gap-3 py-1">
          <dt className="text-muted-foreground">{label}</dt>
          <dd className="min-w-0 truncate">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

type FitRow = (typeof PLACEMENTS)[number] & { verdict: ReturnType<typeof fitVerdict> }

function fitRows(width: number, height: number): FitRow[] {
  return PLACEMENTS.map((p) => Object.assign({ verdict: fitVerdict(cropShare(width, height, p.w, p.h)) }, p))
}

/** Per platform: its mark, name and how many placements fit; then each placement with a tiny crop preview. */
function Fits({ item, rows }: { item: MediaLibraryItem; rows: FitRow[] }) {
  const groups: FitRow[][] = []

  for (const r of rows) {
    const last = groups.at(-1)

    if (last && last[0].provider === r.provider) {
      last.push(r)
    } else {
      groups.push([r])
    }
  }

  return (
    <div className="grid gap-3" data-testid="media-fits">
      <ul className="grid divide-y">
        {groups.map((g) => {
          const ok = g.filter((r) => r.verdict.tone === 'fits').length

          return (
            <li key={g[0].provider} className="py-2 first:pt-0 last:pb-0">
              <div className="flex h-7 items-center gap-2 text-sm font-semibold">
                <span aria-hidden className="flex">
                  <ProviderIcon id={g[0].provider} className="size-4" />
                </span>
                {g[0].platform}
                <span className="ml-auto text-xs font-normal text-muted-foreground">
                  {ok} of {g.length} {g.length === 1 ? 'fits' : 'fit'}
                </span>
              </div>
              <ul className="grid">
                {g.map((p) => (
                  <li
                    key={p.name}
                    className="flex h-8 items-center gap-2 pl-6 text-sm"
                    data-testid="media-fit"
                    title={`${p.platform} ${p.name}: ${p.verdict.label}`}
                  >
                    <CropFrame item={item} w={p.w} h={p.h} />
                    <span className="min-w-0 truncate">
                      <span className="sr-only">{p.platform} </span>
                      {p.name}
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">{ratioLabel(p.w * 100, p.h * 100)}</span>
                    <span
                      className={cn(
                        'ml-auto shrink-0 text-xs font-medium',
                        TONES[p.verdict.tone === 'fits' ? 'success' : p.verdict.tone === 'some' ? 'warning' : 'danger']
                          .text,
                      )}
                    >
                      {p.verdict.label}
                    </span>
                  </li>
                ))}
              </ul>
            </li>
          )
        })}
      </ul>
      <p className="text-xs text-muted-foreground">
        The outline is what each placement keeps. Common shapes; platforms change their specs now and then.
      </p>
    </div>
  )
}

/** The file's shape in a 24 px box, the part a w:h placement keeps outlined and the rest dimmed. */
function CropFrame({ item, w, h }: { item: MediaLibraryItem; w: number; h: number }) {
  const box = 24
  const r = item.width! / item.height!
  const shape = r >= 1 ? { width: box, height: box / r } : { width: box * r, height: box }
  const target = w / h
  const keep =
    target > r
      ? { width: shape.width, height: shape.width / target }
      : { width: shape.height * target, height: shape.height }

  return (
    <span aria-hidden className="flex size-6 shrink-0 items-center justify-center">
      <span
        className="relative overflow-hidden rounded-[3px] bg-muted-foreground/30 bg-cover bg-center"
        style={{
          ...shape,
          backgroundImage: mediaKind(item.mime_type) === 'image' ? `url(${mediaThumbUrl(item.id, 160)})` : undefined,
        }}
      >
        <span
          className="absolute top-1/2 left-1/2 -translate-1/2 rounded-[2px] shadow-[0_0_0_40px_rgb(0_0_0/0.55)] ring-1 ring-white"
          style={keep}
        />
      </span>
    </span>
  )
}

/**
 * The preview: one box of the same height for every file, on a checkerboard, the picture centred and never
 * stretched (tiny pictures are scaled up pixel by pixel). A picture opens full screen.
 */
function Preview({ item }: { item: MediaLibraryItem }) {
  const kind = mediaKind(item.mime_type)
  const [broken, setBroken] = useState(false)
  const [full, setFull] = useState(false)
  const tiny = !!item.width && !!item.height && item.width < 64 && item.height < 64
  const box =
    'relative flex aspect-video w-full items-center justify-center overflow-hidden rounded-xl border sm:aspect-auto sm:h-56'

  if (kind === 'video') {
    return (
      <div className={cn(box, 'bg-black')}>
        <video src={mediaUrl(item.id)} controls playsInline preload="metadata" className="size-full object-contain">
          <track kind="captions" />
        </video>
      </div>
    )
  }

  if (kind === 'audio') {
    return (
      <div className={cn(box, 'flex-col gap-3 bg-muted p-4 text-muted-foreground')}>
        <Music className="size-8" aria-hidden />
        <audio src={mediaUrl(item.id)} controls preload="metadata" className="w-full max-w-sm">
          <track kind="captions" />
        </audio>
      </div>
    )
  }

  if (kind !== 'image' || broken) {
    return (
      <div className={cn(box, 'flex-col gap-2 bg-muted text-muted-foreground')}>
        <FileText className="size-8" aria-hidden />
        <span className="text-sm">{broken ? "The file couldn't be loaded" : 'No preview for this file type'}</span>
      </div>
    )
  }

  const picture = (big: boolean) => (
    <img
      src={mediaUrl(item.id)}
      alt={item.alt_text || item.original_name}
      draggable={false}
      onError={() => setBroken(true)}
      className={cn('max-h-full max-w-full object-contain', tiny && '[image-rendering:pixelated]')}
      // Scaled by whole pixels to about 160 px (640 px full screen), never blurred.
      style={
        tiny
          ? {
              width: item.width! * Math.max(1, Math.floor((big ? 640 : 160) / Math.max(item.width!, item.height!))),
              height: 'auto',
            }
          : undefined
      }
    />
  )

  return (
    <>
      <button
        type="button"
        aria-label="Open full screen preview"
        onClick={() => setFull(true)}
        className={cn(box, 'group cursor-zoom-in p-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50')}
        style={CHECKER}
      >
        {picture(false)}
        <span className="absolute top-2 right-2 rounded-md bg-background/90 p-1 opacity-0 shadow-sm transition group-hover:opacity-100 group-focus-visible:opacity-100">
          <Maximize2 className="size-3.5" aria-hidden />
        </span>
      </button>
      <Dialog open={full} onOpenChange={setFull}>
        <DialogContent
          className="flex h-svh w-screen max-w-none flex-col gap-3 rounded-none border-0 bg-black/95 p-4 text-white sm:max-w-none"
          data-testid="media-lightbox"
        >
          <div className="flex min-w-0 items-center gap-3 pr-10">
            <DialogTitle className="min-w-0 truncate text-base text-white">{item.original_name}</DialogTitle>
            <DialogDescription className="shrink-0 text-white/80">
              {item.width} × {item.height}
            </DialogDescription>
            <Button variant="secondary" asChild className="ml-auto shrink-0">
              <a href={mediaUrl(item.id, { download: true })} download={item.original_name}>
                <Download /> Download
              </a>
            </Button>
          </div>
          <button
            type="button"
            aria-label="Close the preview"
            onClick={() => setFull(false)}
            className="flex min-h-0 flex-1 cursor-zoom-out items-center justify-center outline-none"
          >
            <span className="flex max-h-full max-w-full" style={CHECKER}>
              {picture(true)}
            </span>
          </button>
        </DialogContent>
      </Dialog>
    </>
  )
}
