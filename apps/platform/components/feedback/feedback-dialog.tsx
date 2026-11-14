'use client'

import { useMutation } from '@tanstack/react-query'
import { ArrowRight, XIcon } from 'lucide-react'
import { useRef, useState } from 'react'
import { toast } from 'sonner'

import { AssistantIcon } from '@/components/assistant/assistant-icon'
import { DOCS_URL } from '@/components/common/docs-link'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { errorMessage, request } from '@/lib/api'

const HELP_CARD =
  'group flex w-full items-center gap-3 rounded-lg border bg-muted/40 px-3 py-2.5 text-left text-sm transition-colors outline-none hover:bg-muted focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50'

/** Help & Feedback: for a question, the way to the AI Assistant (the docs without one); below, a report to the staqly team. */
export function FeedbackDialog({
  open,
  onOpenChange,
  onAskAssistant,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Opens the AI Assistant; absent when the server has no AI for it. */
  onAskAssistant?: () => void
}) {
  const [kind, setKind] = useState('problem')
  const [description, setDescription] = useState('')
  const [screenshots, setScreenshots] = useState<{ id: number; file: File }[]>([])
  const [error, setError] = useState('')
  const sending = useRef(false)
  const nextFileId = useRef(0)
  const { mutate, isPending } = useMutation({
    mutationFn: () => {
      const form = new FormData()

      form.set('kind', kind)
      form.set('description', description.trim())
      form.set('page_path', window.location.pathname)

      for (const { file: screenshot } of screenshots) {
        form.append('screenshot', screenshot)
      }

      return request<{ id: string }>('/v1/feedback', { method: 'POST', form })
    },
    onSuccess: () => {
      setDescription('')
      setScreenshots([])
      setError('')
      setKind('problem')

      onOpenChange(false)
      toast.success('Feedback sent. Thank you!')
    },
    onError: (e) => setError(errorMessage(e)),
    onSettled: () => {
      sending.current = false
    },
  })

  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!sending.current) {
          onOpenChange(value)
        }
      }}
    >
      <DialogContent className="sm:max-w-2xl" showCloseButton={!isPending}>
        <DialogHeader>
          <DialogTitle>Help & Feedback</DialogTitle>
          <DialogDescription>Report a problem or suggest a feature to the staqly team.</DialogDescription>
        </DialogHeader>
        {onAskAssistant ? (
          <button
            type="button"
            className={HELP_CARD}
            disabled={isPending}
            onClick={() => {
              onOpenChange(false)
              onAskAssistant()
            }}
          >
            <AssistantIcon className="size-5 shrink-0" />
            <span className="grid min-w-0 flex-1">
              <span className="font-medium">Need help?</span>
              <span className="text-xs text-muted-foreground">Ask the AI Assistant how to do anything in staqly.</span>
            </span>
            <span className="flex shrink-0 items-center gap-1 font-medium">
              Ask <ArrowRight className="size-3.5" />
            </span>
          </button>
        ) : (
          <a href={DOCS_URL} target="_blank" rel="noreferrer" className={HELP_CARD}>
            <span className="grid min-w-0 flex-1">
              <span className="font-medium">Need help?</span>
              <span className="text-xs text-muted-foreground">Step-by-step guides are at docs.staqly.ai.</span>
            </span>
            <span className="flex shrink-0 items-center gap-1 font-medium">
              Open the docs <ArrowRight className="size-3.5" />
            </span>
          </a>
        )}
        <form
          className="grid gap-4"
          aria-busy={isPending}
          onSubmit={(event) => {
            event.preventDefault()

            if (sending.current) {
              return
            }

            if (!description.trim() || Array.from(description.trim()).length > 5000) {
              setError('Describe your feedback in 1 to 5000 characters.')

              return
            }

            if (
              screenshots.some(
                ({ file: screenshot }) =>
                  screenshot.size === 0 ||
                  screenshot.size > 5 * 1024 * 1024 ||
                  !['image/png', 'image/jpeg', 'image/webp'].includes(screenshot.type),
              )
            ) {
              setError('Choose PNG, JPEG or WebP screenshots up to 5 MB each.')

              return
            }

            setError('')
            sending.current = true
            mutate()
          }}
        >
          <Field>
            <FieldLabel htmlFor="feedback-kind">Type</FieldLabel>
            <Select value={kind} onValueChange={setKind} disabled={isPending}>
              <SelectTrigger id="feedback-kind" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="problem">Report a problem</SelectItem>
                <SelectItem value="feature">Suggest a feature</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="feedback-description" required>
              Description
            </FieldLabel>
            <Textarea
              id="feedback-description"
              required
              maxLength={5000}
              rows={8}
              className="min-h-48 sm:min-h-56"
              aria-required="true"
              value={description}
              disabled={isPending}
              onChange={(e) => setDescription(e.target.value)}
              aria-describedby="feedback-privacy"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="feedback-screenshot">Screenshots (optional)</FieldLabel>
            <Input
              id="feedback-screenshot"
              type="file"
              multiple
              accept="image/png,image/jpeg,image/webp"
              disabled={isPending}
              onChange={(e) => {
                const selected = Array.from(e.target.files ?? [], (file) => ({ file, id: nextFileId.current++ }))

                e.target.value = ''

                if (screenshots.length + selected.length > 10) {
                  setError('Attach at most 10 screenshots.')

                  return
                }

                setScreenshots([...screenshots, ...selected])
                setError('')
              }}
              aria-describedby="feedback-file-help"
            />
            <FieldDescription id="feedback-file-help">
              Up to 10 PNG, JPEG or WebP images, 5 MB each. Remove any private information before attaching.
            </FieldDescription>
            {screenshots.length > 0 && (
              <ul className="grid gap-1" aria-label="Selected screenshots">
                {screenshots.map(({ file, id }, index) => (
                  <li key={id} className="flex min-w-0 items-center gap-2">
                    <span className="min-w-0 flex-1 wrap-anywhere text-sm">{file.name}</span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Remove screenshot ${index + 1}: ${file.name}`}
                      disabled={isPending}
                      onClick={() => setScreenshots(screenshots.filter((_, i) => i !== index))}
                    >
                      <XIcon />
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </Field>
          <p id="feedback-privacy" className="text-xs text-muted-foreground">
            We include your account, workspace and page path. We do not capture your screen, browser storage or logs.
          </p>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" disabled={isPending} onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isPending}>
              {isPending ? 'Sending...' : 'Send feedback'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
