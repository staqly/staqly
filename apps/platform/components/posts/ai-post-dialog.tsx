'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  AlignLeft,
  BadgePercent,
  BookOpen,
  Briefcase,
  CalendarDays,
  Check,
  Film,
  GraduationCap,
  ImageIcon,
  Lightbulb,
  ListTree,
  Loader2,
  MessageCircleQuestion,
  Rocket,
  RotateCcw,
  Sparkles,
  Trophy,
  X,
  Zap,
} from 'lucide-react'
import { useState } from 'react'

import { Callout } from '@/components/common/callout'
import { insertMedia } from '@/components/media/media-cache'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Kbd } from '@/components/ui/kbd'
import { Label } from '@/components/ui/label'
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from '@/components/ui/sheet'
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
  { label: 'Announce a feature', text: 'Announce our newest feature: ', icon: Rocket },
  { label: 'Share a tip', text: 'Share a practical tip about ', icon: Lightbulb },
  { label: 'Invite to an event', text: 'Invite people to our event on ', icon: CalendarDays },
  { label: 'Tell a story', text: 'Tell the story behind ', icon: BookOpen },
  { label: 'Celebrate a milestone', text: 'Celebrate our milestone: ', icon: Trophy },
  { label: 'Ask your audience', text: 'Ask our audience what they think about ', icon: MessageCircleQuestion },
  { label: 'Promote an offer', text: 'Promote our offer: ', icon: BadgePercent },
  { label: 'Share a lesson', text: 'Share a lesson we learned from ', icon: GraduationCap },
  { label: "We're hiring", text: 'Announce an open role: ', icon: Briefcase },
]

type MediaKind = 'none' | 'image' | 'video'
type Shape = '1:1' | '9:16' | '16:9'

const SHAPES: { id: Shape; label: string }[] = [
  { id: '1:1', label: 'Square' },
  { id: '9:16', label: 'Portrait' },
  { id: '16:9', label: 'Landscape' },
]

// What the video models make (Kling on fal, Higgsfield): one clip of 5 or 10 seconds.
const LENGTHS: { id: '5' | '10'; label: string }[] = [
  { id: '5', label: '5 seconds' },
  { id: '10', label: '10 seconds' },
]

// A picture's size per shape (1344 px on the long side, like the design editor's pictures).
const PICTURE_SIZE: Record<Shape, { width: number; height: number }> = {
  '1:1': { width: 1024, height: 1024 },
  '9:16': { width: 768, height: 1344 },
  '16:9': { width: 1344, height: 768 },
}

/** What the dialog hands the composer: the draft and, if one was made, its picture or video (in the library). */
export interface AiPostResult {
  draft: AIPostDraft
  picture?: Media
}

function Chips<T extends string>({
  label,
  required,
  options,
  value,
  onChange,
}: {
  label: string
  required?: boolean
  options: readonly { id: T; label: string }[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div className="grid gap-2">
      <Label required={required}>{label}</Label>
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
 * Write with AI, a second drawer over the composer: a brief and how the post should read (format, voice, tone, a picture or not) become a draft to
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
  const canVideo = !!status.data?.operations.some((o) => o.id === 'text_to_video')
  const [brief, setBrief] = useState('')
  const [format, setFormat] = useState<Format>('short')
  const [voice, setVoice] = useState<Voice>('personal')
  const [tone, setTone] = useState<Tone>('friendly')
  const [mediaKind, setMediaKind] = useState<MediaKind>('none')
  const [shape, setShape] = useState<Shape>('1:1')
  const [length, setLength] = useState<'5' | '10'>('5')
  const [jobId, setJobId] = useState<string | null>(null)
  const [pictureError, setPictureError] = useState('')
  const job = useAiJob(jobId)
  const j = job.data
  // What the server can make; a choice it no longer offers counts as none.
  const media = (mediaKind === 'image' && canPicture) || (mediaKind === 'video' && canVideo) ? mediaKind : 'none'
  const mediaWord = media === 'video' ? 'video' : 'picture'

  const write = useMutation({
    mutationFn: () =>
      api.aiPost({
        brief: brief.trim(),
        format,
        voice,
        tone,
        max_chars: maxChars,
        media: media === 'none' ? undefined : media,
      }),
    onSuccess: (draft) => {
      setPictureError('')

      if (media !== 'none' && draft.media_prompt) {
        const body =
          media === 'video'
            ? { operation: 'text_to_video' as const, fields: { duration: Number(length), aspect_ratio: shape } }
            : { operation: 'generate' as const, fields: PICTURE_SIZE[shape] }

        api
          .startAiJob({ ...body, prompt: draft.media_prompt }, requestKey())
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
  // Like every form here: the button stays on, an empty brief says so under the field once tried.
  const [tried, setTried] = useState(false)
  const briefError = tried && !brief.trim() ? 'Say what the post is about.' : undefined
  const submit = () => {
    setTried(true)

    if (brief.trim() && !write.isPending) {
      write.mutate()
    }
  }
  const note = allowanceNote(status.data)

  return (
    <Sheet open={open} onOpenChange={close}>
      <SheetContent
        // A second drawer over the composer, a little narrower so the composer's edge shows (like the preview).
        className="gap-0 overflow-hidden p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-[calc(42rem-1.5rem)] data-[side=right]:lg:max-w-[calc(56rem-3rem)]"
        data-testid="ai-post-dialog"
        // ⌘/Ctrl+Enter writes from anywhere in the form.
        onKeyDown={(e) => {
          if (!draft && e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault()
            submit()
          }
        }}
      >
        <SheetHeader className="relative gap-1 border-b bg-gradient-to-br from-brand-soft/70 via-transparent to-transparent px-6 pb-4">
          <div className="mb-2 flex items-center gap-2">
            <span className="flex size-10 items-center justify-center rounded-xl bg-brand text-brand-foreground shadow-sm">
              <Sparkles className="size-5" />
            </span>
          </div>
          <SheetTitle className="text-lg">Write with AI</SheetTitle>
          <SheetDescription>
            {draft
              ? 'Look it over. Use it and change anything in the post before it goes out.'
              : 'Say what the post is about. AI drafts the title and the text; nothing is published.'}
          </SheetDescription>
        </SheetHeader>

        <div className="grid min-h-0 flex-1 content-start gap-5 overflow-y-auto overscroll-contain px-6 py-5 max-sm:px-5">
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
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{mediaWord}</p>
                  {j?.status === 'succeeded' ? (
                    j.kind === 'video' ? (
                      <video
                        src={aiResultUrl(j.id)}
                        controls
                        playsInline
                        preload="metadata"
                        className="max-h-64 w-auto self-start rounded-lg border bg-black"
                      >
                        <track kind="captions" />
                      </video>
                    ) : (
                      <img
                        src={aiResultUrl(j.id)}
                        alt={draft.media_prompt ?? ''}
                        className="max-h-64 w-auto self-start rounded-lg border"
                      />
                    )
                  ) : pictureFailed ? (
                    <p className="text-sm text-destructive">
                      The {mediaWord} could not be made. Use the post without it.
                    </p>
                  ) : (
                    <div className="flex h-40 animate-pulse items-center justify-center rounded-lg bg-muted text-sm text-muted-foreground">
                      <Loader2 className="mr-2 size-4 animate-spin" />
                      {media === 'video' ? 'Making the video, a few minutes...' : 'Making the picture...'}
                    </div>
                  )}
                </div>
              )}
              {pictureError && (
                <Callout tone="danger" size="xs" role="alert">
                  {media === 'video' ? 'Video' : 'Picture'}: {pictureError}
                </Callout>
              )}
            </div>
          ) : (
            <>
              <div className="grid gap-2">
                <Label htmlFor="ai-post-brief" required>
                  What is it about?
                </Label>
                <Textarea
                  id="ai-post-brief"
                  value={brief}
                  onChange={(e) => setBrief(e.target.value)}
                  placeholder="A few words or notes: what happened, what you want people to know or do, links..."
                  maxLength={4000}
                  rows={4}
                  className="min-h-24 resize-y"
                  aria-required
                  aria-invalid={!!briefError}
                  data-testid="ai-post-brief"
                />
                <FieldError>{briefError}</FieldError>
                {!brief && (
                  <div className="flex flex-wrap items-center gap-1.5" aria-label="Start from an idea" role="group">
                    <span className="mr-0.5 text-xs text-muted-foreground">Ideas:</span>
                    {STARTERS.map((s) => (
                      <button
                        key={s.label}
                        type="button"
                        onClick={() => {
                          setBrief(s.text)
                          document.getElementById('ai-post-brief')?.focus()
                        }}
                        className="inline-flex h-7 items-center gap-1.5 rounded-full border bg-card px-2.5 text-xs transition-colors outline-none hover:border-brand/40 hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:h-9"
                      >
                        <s.icon className="size-3.5 shrink-0 text-brand" />
                        {s.label}
                      </button>
                    ))}
                  </div>
                )}
              </div>

              <div className="grid gap-2">
                <Label required>Format</Label>
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

              <Chips label="Voice" required options={VOICES} value={voice} onChange={setVoice} />
              <Chips label="Tone" required options={TONES} value={tone} onChange={setTone} />

              {(canPicture || canVideo) && (
                <div className="grid gap-2">
                  <Label>Media</Label>
                  <div role="radiogroup" aria-label="Media" className="grid grid-cols-3 gap-2">
                    {(
                      [
                        { id: 'none', label: 'Text only', icon: X, show: true },
                        { id: 'image', label: 'Picture', icon: ImageIcon, show: canPicture },
                        { id: 'video', label: 'Video', icon: Film, show: canVideo },
                      ] as const
                    )
                      .filter((m) => m.show)
                      .map((m) => (
                        <button
                          key={m.id}
                          type="button"
                          role="radio"
                          aria-checked={media === m.id}
                          onClick={() => setMediaKind(m.id)}
                          className={cn(
                            'flex min-h-10 items-center justify-center gap-2 rounded-xl border bg-card px-3 py-2.5 text-sm font-medium transition-colors outline-none hover:border-brand/40 hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring',
                            media === m.id && 'border-brand bg-brand-soft/60 text-brand hover:bg-brand-soft/60',
                          )}
                        >
                          <m.icon className="size-4 shrink-0" /> {m.label}
                        </button>
                      ))}
                  </div>
                  {media !== 'none' && (
                    <>
                      <Chips label="Shape" options={SHAPES} value={shape} onChange={setShape} />
                      {media === 'video' && (
                        <Chips label="Length" options={LENGTHS} value={length} onChange={setLength} />
                      )}
                      <p className="text-xs text-muted-foreground">
                        {media === 'video'
                          ? `AI makes a ${length} second clip to go with the post; it takes a few minutes.`
                          : 'AI makes a picture to go with the post.'}{' '}
                        It lands in your library too.
                      </p>
                    </>
                  )}
                </div>
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

        <SheetFooter className="mt-0 flex-row flex-wrap items-center justify-between border-t bg-muted/30 px-6 py-4 max-sm:px-5">
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
              <Button onClick={submit} disabled={write.isPending} data-testid="ai-post-write">
                {write.isPending ? <Loader2 className="animate-spin" /> : <Sparkles />}
                {write.isPending ? 'Writing...' : 'Write the post'}
                {!write.isPending && <Kbd className="ml-1 max-sm:hidden">⌘↵</Kbd>}
              </Button>
            </div>
          )}
        </SheetFooter>
      </SheetContent>
    </Sheet>
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
        size="sm"
        className="border-brand/40 text-brand hover:bg-brand-soft hover:text-brand"
        onClick={() => setOpen(true)}
        data-testid="ai-post-open"
      >
        <Sparkles /> Write with AI
      </Button>
      <AiPostDialog open={open} onOpenChange={setOpen} {...props} />
    </>
  )
}
