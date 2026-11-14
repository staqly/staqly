'use client'

import { useQueryClient } from '@tanstack/react-query'
import { Check, Loader2, RotateCcw, Sparkles } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { insertMedia, refreshMediaCounts } from '@/components/media/media-cache'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { AI_OPS, allowanceNote, madeBy, requestKey } from '@/lib/ai'
import { isLive, useAiJob, useAiStatus } from '@/lib/ai-status'
import { aiResultUrl, api, errorMessage, mediaThumbUrl, mediaUrl, type Media } from '@/lib/api'
import { cn } from '@/lib/utils'

const DURATIONS = [5, 10] as const
const SHAPES = [
  { id: '16:9', label: 'Landscape' },
  { id: '9:16', label: 'Portrait' },
  { id: '1:1', label: 'Square' },
] as const

/** A picture from the library, as the AI service gets it: a PNG at most 1280 px on its long side. */
async function pictureBlob(m: Media) {
  const blob = await (await fetch(mediaUrl(m.id))).blob()
  const { downscale } = await import('@/components/design/raster')

  return downscale(blob, 1280)
}

function Choice<T extends string | number>({
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
    <div className="grid gap-1.5">
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
              'rounded-full border px-3 py-1 text-sm hover:bg-muted pointer-coarse:py-2',
              value === o.id && 'border-brand bg-brand-soft text-brand',
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
 * A video clip made by AI: from a prompt, or from a library picture (animate it). The result previews here (never
 * playing by itself) and becomes a library file only when kept; closing while it runs cancels it.
 */
export function AiVideoDialog({
  open,
  onOpenChange,
  from,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The picture to animate; none: a video from a prompt. */
  from?: Media
}) {
  const qc = useQueryClient()
  const status = useAiStatus()
  const op = from ? 'image_to_video' : 'text_to_video'
  const copy = AI_OPS[op]
  const [prompt, setPrompt] = useState('')
  const [duration, setDuration] = useState<5 | 10>(5)
  const [shape, setShape] = useState<'16:9' | '9:16' | '1:1'>('16:9')
  const [jobId, setJobId] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [keeping, setKeeping] = useState(false)
  const [error, setError] = useState('')
  const [key, setKey] = useState(requestKey)
  const attempt = useRef({ version: 0, jobId: '' })

  useEffect(() => {
    const active = attempt.current

    return () => {
      active.version++

      if (active.jobId) {
        void api.discardAiJob(active.jobId).catch(() => undefined)
      }
    }
  }, [])

  const job = useAiJob(jobId)
  const j = job.data
  const running = starting || (!!jobId && (!j || isLive(j)))

  const reset = () => {
    attempt.current.jobId = ''
    setJobId(null)
    setError('')
    setKey(requestKey())
  }

  const close = (o: boolean) => {
    if (!o) {
      attempt.current.version++

      if (jobId && !j?.media_id) {
        void api.discardAiJob(jobId).catch(() => undefined)
      }

      setStarting(false)
      reset()
    }

    onOpenChange(o)
  }

  const start = (sameKey: boolean) => {
    const k = sameKey ? key : requestKey()
    const version = ++attempt.current.version

    if (jobId && !sameKey) {
      void api.discardAiJob(jobId).catch(() => undefined) // the earlier result, not kept
      attempt.current.jobId = ''
      setJobId(null)
    }

    setKey(k)
    setError('')
    setStarting(true)
    void (from ? pictureBlob(from) : Promise.resolve(undefined))
      .then((image) => {
        if (version !== attempt.current.version) {
          return
        }

        return api.startAiJob(
          {
            operation: op,
            prompt: prompt.trim(),
            image,
            fields: from ? { duration } : { duration, aspect_ratio: shape },
          },
          k,
        )
      })
      .then((started) => {
        if (!started) {
          return
        }

        // Closing can race with POST /ai/jobs; cancel its returned job instead of losing it.
        if (version !== attempt.current.version) {
          return api.discardAiJob(started.id).catch(() => undefined)
        }

        attempt.current.jobId = started.id
        setJobId(started.id)
      })
      .catch((e: unknown) => {
        if (version === attempt.current.version) {
          setError(errorMessage(e))
        }
      })
      .finally(() => {
        if (version === attempt.current.version) {
          setStarting(false)
        }
      })
  }

  const keep = () => {
    if (!jobId) {
      return
    }

    setKeeping(true)
    void api
      .acceptAiJob(jobId)
      .then((m) => {
        insertMedia(qc, [{ ...m, uses: 0 }])
        refreshMediaCounts(qc)
        toast.success('Video added to your library', { description: m.original_name })
        reset()
        onOpenChange(false)
      })
      .catch((e: unknown) => toast.error('Could not keep it', { description: errorMessage(e) }))
      .finally(() => setKeeping(false))
  }

  const done = j && !isLive(j) ? j : undefined
  const note = allowanceNote(status.data)

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-lg" data-testid="ai-video-dialog">
        <DialogHeader>
          <DialogTitle>{from ? 'Animate with AI' : 'New video with AI'}</DialogTitle>
          <DialogDescription>{copy.hint} It takes a few minutes; closing this cancels it.</DialogDescription>
        </DialogHeader>
        {done?.status === 'succeeded' && done.result ? (
          <div className="grid gap-2">
            <video
              src={aiResultUrl(done.id)}
              controls
              playsInline
              preload="metadata"
              className="max-h-80 w-full rounded-md bg-black"
            >
              <track kind="captions" />
            </video>
            <p className="text-xs text-muted-foreground">{madeBy(done)}</p>
          </div>
        ) : (
          <div className="grid gap-4">
            {from && (
              <img
                src={mediaThumbUrl(from.id, 320)}
                alt={from.alt_text || from.original_name}
                className="max-h-40 w-auto self-start rounded-md border"
              />
            )}
            <div className="grid gap-1.5">
              <Label htmlFor="ai-video-prompt" required>
                {from ? 'How should it move?' : 'What should the video show?'}
              </Label>
              <Textarea
                id="ai-video-prompt"
                aria-required
                value={prompt}
                maxLength={4000}
                rows={3}
                disabled={running}
                placeholder={
                  from ? 'Slow zoom in, the leaves move in the wind' : 'Waves rolling onto a beach at sunset'
                }
                onChange={(e) => setPrompt(e.target.value)}
              />
            </div>
            <Choice
              label="Length"
              options={DURATIONS.map((d) => ({ id: d, label: `${d} seconds` }))}
              value={duration}
              onChange={setDuration}
            />
            {!from && <Choice label="Shape" options={SHAPES} value={shape} onChange={setShape} />}
            {running && (
              <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" /> Making the video, this can take a few minutes...
              </p>
            )}
            {(error || (done && done.status !== 'succeeded')) && (
              <Callout tone="danger" role="alert">
                {error || done?.error?.message || 'It did not work.'}
              </Callout>
            )}
            {note && <p className="text-xs text-muted-foreground">{note}</p>}
          </div>
        )}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => close(false)}>
            {running ? 'Cancel' : done?.status === 'succeeded' ? 'Discard' : 'Close'}
          </Button>
          {done?.status === 'succeeded' ? (
            <>
              <Button type="button" variant="outline" onClick={() => start(false)} disabled={keeping}>
                <RotateCcw /> Try again
              </Button>
              <Button type="button" onClick={keep} disabled={keeping}>
                {keeping ? <Loader2 className="animate-spin" /> : <Check />} Keep it
              </Button>
            </>
          ) : (
            <Button type="button" onClick={() => start(!!error)} disabled={running || !prompt.trim()}>
              {running ? <Loader2 className="animate-spin" /> : <Sparkles />} {copy.run}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
