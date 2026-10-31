'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { AlignLeft, Check, ImageIcon, ListTree, Loader2, RotateCcw, Sparkles, Zap } from 'lucide-react'
import { useEffect, useState } from 'react'

import { Callout } from '@/components/common/callout'
import { insertMedia } from '@/components/media/media-cache'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Kbd } from '@/components/ui/kbd'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { allowanceNote, allowanceUsedUp, requestKey } from '@/lib/ai'
import { isLive, useAiJob, useAiStatus } from '@/lib/ai-status'
import { aiResultUrl, api, errorMessage, type AIPostDraft, type AIPostRequest, type Media } from '@/lib/api'
import { cn } from '@/lib/utils'

type Format = NonNullable<AIPostRequest['format']>
type Voice = NonNullable<AIPostRequest['voice']>
type Tone = NonNullable<AIPostRequest['tone']>

const FORMATS: { id: Format; label: string; hint: string; icon: typeof Zap }[] = [
  { id: 'short', label: 'Short post', hint: 'One to three sentences', icon: Zap },
  { id: 'long', label: 'Long post', hint: 'A hook, a few paragraphs', icon: AlignLeft },
  { id: 'thread', label: 'Thread', hint: 'A post and comments under it', icon: ListTree },
]

const VOICES: { id: Voice; label: string }[] = [
  { id: 'personal', label: 'Personal ("I")' },
  { id: 'brand', label: 'Brand ("we")' },
  { id: 'neutral', label: 'Neutral' },
]

const TONES: { id: Tone; label: string }[] = [
  { id: 'friendly', label: 'Friendly' },
  { id: 'professional', label: 'Professional' },
  { id: 'witty', label: 'Witty' },
  { id: 'bold', label: 'Bold' },
  { id: 'informative', label: 'Informative' },
]

// A start for the brief, so an empty box isn't the first thing asked of anyone.
const STARTERS = [
  'Announce our newest feature: ',
  'Share a practical tip about ',
  'Invite people to our event on ',
  'Tell the story behind ',
]

// Square: what fits every platform's feed.
const PICTURE = { width: 1024, height: 1024 }

/** What the dialog hands the composer: the draft and, if one was made, its picture (already in the library). */
export interface AiPostResult {
  draft: AIPostDraft
  picture?: Media
}

function Chips<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: readonly { id: T; label: string }[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-1.5">
        {options.map((o) => (
          <button
            key={o.id}
            type="button"
            role="radio"
            aria-checked={value === o.id}
            onClick={() => onChange(o.id)}
            className={cn(
              'rounded-full border px-3 py-1 text-sm transition-colors outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:py-2',
              value === o.id && 'border-brand bg-brand-soft text-brand hover:bg-brand-soft',
            )}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  )
}

/**
 * Write with AI: a brief and how the post should read (format, voice, tone, a picture or not) become a draft to
 * look over, then "Use this" puts its title, text and comments into the composer (and the picture into its
 * media). Nothing is saved or published here.
 */
export function AiPostDialog({
  open,
  onOpenChange,
  maxChars,
  replaces,
  onUse,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The shortest limit of the picked channels (0: none). */
  maxChars: number
  /** The composer already has text: "Use this" says it replaces it. */
  replaces: boolean
  onUse: (r: AiPostResult) => void
}) {
  const qc = useQueryClient()
  const status = useAiStatus()
  const canPicture = !!status.data?.operations.some((o) => o.id === 'generate')
  const [brief, setBrief] = useState('')
  const [format, setFormat] = useState<Format>('short')
  const [voice, setVoice] = useState<Voice>('personal')
  const [tone, setTone] = useState<Tone>('friendly')
  const [picture, setPicture] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)
  const [pictureError, setPictureError] = useState('')
  const job = useAiJob(jobId)
  const j = job.data
  const withPicture = picture && canPicture

  const write = useMutation({
    mutationFn: () => api.aiPost({ brief: brief.trim(), format, voice, tone, max_chars: maxChars, image: withPicture }),
    onSuccess: (draft) => {
      setPictureError('')

      if (withPicture && draft.image_prompt) {
        api
          .startAiJob({ operation: 'generate', prompt: draft.image_prompt, fields: PICTURE }, requestKey())
          .then((started) => setJobId(started.id))
          .catch((e: unknown) => setPictureError(errorMessage(e)))
      }
    },
  })
  const draft = write.data

  const dropPicture = () => {
    if (jobId && !j?.media_id) {
      void api.discardAiJob(jobId).catch(() => undefined)
    }

    setJobId(null)
  }

  const again = () => {
    dropPicture()
    write.reset()
  }

  const close = (o: boolean) => {
    if (!o) {
      dropPicture()
      write.reset()
    }

    onOpenChange(o)
  }

  const keep = useMutation({
    mutationFn: async () => {
      const made = jobId && j?.status === 'succeeded' ? await api.acceptAiJob(jobId) : undefined

      return { draft: draft!, picture: made }
    },
    onSuccess: (r) => {
      if (r.picture) {
        insertMedia(qc, [{ ...r.picture, uses: 0 }])
      }

      setJobId(null)
      onUse(r)
      close(false)
    },
    onError: (e) => setPictureError(errorMessage(e)),
  })

  const picturePending = !!jobId && (!j || isLive(j))
  const pictureFailed = !!jobId && !!j && !isLive(j) && j.status !== 'succeeded'
  const canWrite = brief.trim().length > 0 && !write.isPending
  const note = allowanceNote(status.data)

  // ⌘/Ctrl+Enter writes from anywhere in the form.
  useEffect(() => {
    if (!open || draft) {
      return
    }

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && canWrite) {
        e.preventDefault()
        write.mutate()
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [open, draft, canWrite, write])

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-xl" data-testid="ai-post-dialog">
        <DialogHeader className="relative gap-1 border-b bg-gradient-to-br from-brand-soft/70 via-transparent to-transparent px-6 pt-6 pb-4">
          <span className="mb-2 flex size-10 items-center justify-center rounded-xl bg-brand text-brand-foreground shadow-sm">
            <Sparkles className="size-5" />
          </span>
          <DialogTitle className="text-lg">Write with AI</DialogTitle>
          <DialogDescription>
            {draft
              ? 'Look it over. Use it and change anything in the post before it goes out.'
              : 'Say what the post is about. AI drafts the title and the text; nothing is published.'}
          </DialogDescription>
        </DialogHeader>

        <div className="grid max-h-[65svh] gap-5 overflow-y-auto overscroll-contain px-6 py-5">
          {draft ? (
            <div className="grid gap-3" data-testid="ai-post-draft">
              <div className="grid gap-1 rounded-xl border bg-card p-4">
                <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Title</p>
                <p className="font-medium">{draft.title}</p>
              </div>
              <div className="grid gap-1 rounded-xl border bg-card p-4">
                <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Post</p>
                <p className="text-sm whitespace-pre-wrap">{draft.content}</p>
              </div>
              {draft.comments.map((c, i) => (
                <div key={c} className="ml-4 grid gap-1 rounded-xl border border-dashed bg-card/60 p-4">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Comment {i + 1}</p>
                  <p className="text-sm whitespace-pre-wrap">{c}</p>
                </div>
              ))}
              {jobId && (
                <div className="grid gap-2 rounded-xl border bg-card p-4">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Picture</p>
                  {j?.status === 'succeeded' ? (
                    <img
                      src={aiResultUrl(j.id)}
                      alt={draft.image_prompt ?? ''}
                      className="max-h-64 w-auto self-start rounded-lg border"
                    />
                  ) : pictureFailed ? (
                    <p className="text-sm text-destructive">The picture could not be made. Use the post without it.</p>
                  ) : (
                    <div className="flex aspect-square max-h-64 animate-pulse items-center justify-center rounded-lg bg-muted text-sm text-muted-foreground">
                      <Loader2 className="mr-2 size-4 animate-spin" /> Making the picture...
                    </div>
                  )}
                </div>
              )}
              {pictureError && (
                <Callout tone="danger" size="xs" role="alert">
                  Picture: {pictureError}
                </Callout>
              )}
            </div>
          ) : (
            <>
              <div className="grid gap-2">
                <Label htmlFor="ai-post-brief">What is it about?</Label>
                <Textarea
                  id="ai-post-brief"
                  value={brief}
                  onChange={(e) => setBrief(e.target.value)}
                  placeholder="A few words or notes: what happened, what you want people to know or do, links..."
                  maxLength={4000}
                  rows={5}
                  className="min-h-32 resize-y"
                  data-testid="ai-post-brief"
                />
                {!brief && (
                  <div className="flex flex-wrap gap-1.5">
                    {STARTERS.map((s) => (
                      <button
                        key={s}
                        type="button"
                        onClick={() => {
                          setBrief(s)
                          document.getElementById('ai-post-brief')?.focus()
                        }}
                        className="rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-muted/70 hover:text-foreground pointer-coarse:py-2"
                      >
                        {s.replace(/[: ]+$/, '')}
                      </button>
                    ))}
                  </div>
                )}
              </div>

              <div className="grid gap-2">
                <Label>Format</Label>
                <div role="radiogroup" aria-label="Format" className="grid gap-2 sm:grid-cols-3">
                  {FORMATS.map((f) => (
                    <button
                      key={f.id}
                      type="button"
                      role="radio"
                      aria-label={`${f.label}: ${f.hint}`}
                      aria-checked={format === f.id}
                      onClick={() => setFormat(f.id)}
                      className={cn(
                        'flex items-start gap-2.5 rounded-xl border bg-card p-3 text-left transition-colors outline-none hover:border-brand/40 hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring',
                        format === f.id && 'border-brand bg-brand-soft/60 hover:bg-brand-soft/60',
                      )}
                    >
                      <f.icon
                        className={cn(
                          'mt-0.5 size-4 shrink-0',
                          format === f.id ? 'text-brand' : 'text-muted-foreground',
                        )}
                      />
                      <span className="grid gap-0.5">
                        <span className="text-sm leading-tight font-medium">{f.label}</span>
                        <span className="text-xs text-muted-foreground">{f.hint}</span>
                      </span>
                    </button>
                  ))}
                </div>
              </div>

              <Chips label="Voice" options={VOICES} value={voice} onChange={setVoice} />
              <Chips label="Tone" options={TONES} value={tone} onChange={setTone} />

              {canPicture && (
                <label className="flex cursor-pointer items-center gap-3 rounded-xl border bg-card p-3">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                    <ImageIcon className="size-4" />
                  </span>
                  <span className="grid flex-1 gap-0.5">
                    <span className="text-sm font-medium">Add a picture</span>
                    <span className="text-xs text-muted-foreground">AI makes one to go with the post.</span>
                  </span>
                  <Switch checked={picture} onCheckedChange={setPicture} aria-label="Add a picture" />
                </label>
              )}

              {write.error && (
                <Callout tone="danger" size="xs" role="alert">
                  {errorMessage(write.error)}
                </Callout>
              )}
              {maxChars > 0 && (
                <p className="text-xs text-muted-foreground">
                  Kept within {maxChars} characters, the shortest limit of the picked channels.
                </p>
              )}
            </>
          )}
        </div>

        <DialogFooter className="items-center border-t bg-muted/30 px-6 py-4 sm:justify-between">
          <p className="text-xs text-muted-foreground max-sm:hidden">{note}</p>
          {draft ? (
            <div className="flex flex-wrap justify-end gap-2">
              <Button variant="outline" onClick={again} disabled={keep.isPending}>
                <RotateCcw /> Change the brief
              </Button>
              <Button
                variant="outline"
                onClick={() => {
                  dropPicture()
                  write.mutate()
                }}
                disabled={write.isPending || keep.isPending}
              >
                {write.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />} Try again
              </Button>
              <Button
                onClick={() => keep.mutate()}
                disabled={picturePending || keep.isPending}
                title={replaces ? 'Replaces the title and the text in the post' : undefined}
                data-testid="ai-post-use"
              >
                {picturePending || keep.isPending ? <Loader2 className="animate-spin" /> : <Check />}
                {replaces ? 'Replace the post' : 'Use this'}
              </Button>
            </div>
          ) : (
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => close(false)}>
                Cancel
              </Button>
              <Button onClick={() => write.mutate()} disabled={!canWrite} data-testid="ai-post-write">
                {write.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />}
                {write.isPending ? 'Writing...' : 'Write the post'}
                {!write.isPending && <Kbd className="ml-1 max-sm:hidden">⌘↵</Kbd>}
              </Button>
            </div>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The composer's "Write with AI" button and its dialog: shown with the assistant's model on the server and this
 * month's AI allowance left.
 */
export function WriteWithAi(props: { maxChars: number; replaces: boolean; onUse: (r: AiPostResult) => void }) {
  const status = useAiStatus()
  const [open, setOpen] = useState(false)

  if (allowanceUsedUp(status.data) || !status.data?.operations.some((o) => o.id === 'assistant')) {
    return null
  }

  return (
    <>
      <Button
        type="button"
        variant="outline"
        size="xs"
        className="ml-auto border-brand/40 text-brand hover:bg-brand-soft hover:text-brand"
        onClick={() => setOpen(true)}
        data-testid="ai-post-open"
      >
        <Sparkles /> Write with AI
      </Button>
      <AiPostDialog open={open} onOpenChange={setOpen} {...props} />
    </>
  )
}
