'use client'

import { Sparkles } from 'lucide-react'
import { useState } from 'react'

import { Callout } from '@/components/common/callout'
import { NativeSelect } from '@/components/common/native-select'
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
import { allowanceUsedUp, requestKey } from '@/lib/ai'
import { useAiStatus } from '@/lib/ai-status'
import { api, errorMessage, type AIWriteInput, type Media } from '@/lib/api'

export interface Written {
  content: string
  thread: string[]
  media: Media[]
}

const FORMATS: { value: AIWriteInput['format']; label: string }[] = [
  { value: 'short', label: 'Short post' },
  { value: 'long', label: 'Long post' },
  { value: 'thread_short', label: 'A thread of short posts' },
  { value: 'thread_long', label: 'A thread of long posts' },
]

const VOICES: { value: AIWriteInput['voice']; label: string }[] = [
  { value: 'personal', label: 'Personal ("I am happy to announce")' },
  { value: 'company', label: 'Company ("We are happy to announce")' },
]

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))

/** A picture from its description, kept in the library (the same AI job the design editor runs). */
async function makePicture(prompt: string, onStep: (s: string) => void): Promise<Media> {
  onStep('Making the picture')

  let job = await api.startAiJob({ operation: 'generate', prompt, fields: { width: 1080, height: 1080 } }, requestKey())

  while (job.status === 'queued' || job.status === 'running') {
    await wait(1500)
    job = await api.aiJob(job.id)
  }

  if (job.status !== 'succeeded') {
    throw new Error(job.error?.message || 'The picture could not be made.')
  }

  return api.acceptAiJob(job.id)
}

async function write(input: AIWriteInput, onStep: (s: string) => void): Promise<Written> {
  onStep('Writing')

  const out = await api.writeWithAI(input)
  const [first = '', ...rest] = out.posts
  const media = input.picture && out.picture_prompt ? [await makePicture(out.picture_prompt, onStep)] : []

  return { content: first, thread: rest, media }
}

/** "Write with AI": describe the post, pick its shape and voice; the result opens in the composer to edit. */
export function WriteWithAiDialog({
  open,
  onOpenChange,
  onWritten,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onWritten: (w: Written) => void
}) {
  const status = useAiStatus()
  const [prompt, setPrompt] = useState('')
  const [format, setFormat] = useState<AIWriteInput['format']>('short')
  const [voice, setVoice] = useState<AIWriteInput['voice']>('company')
  const [picture, setPicture] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState('')
  const ops = status.data?.operations ?? []
  const canWrite = ops.some((o) => o.id === 'write')
  const canPicture = ops.some((o) => o.id === 'generate')
  const usedUp = allowanceUsedUp(status.data)

  const go = () => {
    setError('')
    void write({ prompt: prompt.trim(), format, voice, picture: picture && canPicture }, setBusy)
      .then((w) => {
        onOpenChange(false)
        setPrompt('')
        onWritten(w)
      })
      .catch((e: unknown) => setError(errorMessage(e)))
      .finally(() => setBusy(null))
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Write with AI</DialogTitle>
          <DialogDescription>
            Describe the post; it opens in the editor for you to change before it goes out.
          </DialogDescription>
        </DialogHeader>
        {usedUp ? (
          <Callout tone="info">This month&apos;s AI allowance is used up; it starts again on {usedUp.renews}.</Callout>
        ) : (
          !status.isPending &&
          !canWrite && <Callout tone="info">Writing with AI needs AI set up on the server.</Callout>
        )}
        <form
          id="write-with-ai"
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            go()
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="ai-write-prompt">What is it about?</Label>
            <Textarea
              id="ai-write-prompt"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              rows={5}
              maxLength={4000}
              placeholder="e.g. We opened a second cafe in Kraków, on Długa 12; first coffee free this week"
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="ai-write-format">Format</Label>
              <NativeSelect
                id="ai-write-format"
                value={format}
                onChange={(e) => setFormat(e.target.value as AIWriteInput['format'])}
              >
                {FORMATS.map((f) => (
                  <option key={f.value} value={f.value}>
                    {f.label}
                  </option>
                ))}
              </NativeSelect>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="ai-write-voice">Voice</Label>
              <NativeSelect
                id="ai-write-voice"
                value={voice}
                onChange={(e) => setVoice(e.target.value as AIWriteInput['voice'])}
              >
                {VOICES.map((v) => (
                  <option key={v.value} value={v.value}>
                    {v.label}
                  </option>
                ))}
              </NativeSelect>
            </div>
          </div>
          {canPicture && (
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={picture}
                onChange={(e) => setPicture(e.target.checked)}
                className="size-4 accent-[var(--brand)]"
              />
              Add a picture made by AI
            </label>
          )}
          {error && <p className="text-sm text-red-700 dark:text-red-400">{error}</p>}
        </form>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={!!busy}>
            Cancel
          </Button>
          <Button type="submit" form="write-with-ai" disabled={!prompt.trim() || !canWrite || !!usedUp || !!busy}>
            <Sparkles /> {busy ? `${busy}...` : 'Write it'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
