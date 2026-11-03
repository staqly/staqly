'use client'

import { useMutation } from '@tanstack/react-query'
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
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { errorMessage, request } from '@/lib/api'

export function FeedbackDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [kind, setKind] = useState('problem')
  const [description, setDescription] = useState('')
  const [screenshot, setScreenshot] = useState<File | null>(null)
  const [error, setError] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)
  const sending = useRef(false)
  const { mutate, isPending } = useMutation({
    mutationFn: () => {
      const form = new FormData()

      form.set('kind', kind)
      form.set('description', description.trim())
      form.set('page_path', window.location.pathname)

      if (screenshot) {
        form.set('screenshot', screenshot)
      }

      return request<{ id: string }>('/v1/feedback', { method: 'POST', form })
    },
    onSuccess: () => {
      setDescription('')
      setScreenshot(null)
      setError('')
      setKind('problem')

      if (fileInput.current) {
        fileInput.current.value = ''
      }

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
      <DialogContent className="sm:max-w-lg" showCloseButton={!isPending}>
        <DialogHeader>
          <DialogTitle>Feedback</DialogTitle>
          <DialogDescription>Report a problem or suggest a feature to the staqly team.</DialogDescription>
        </DialogHeader>
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
              screenshot &&
              (screenshot.size > 5 * 1024 * 1024 ||
                !['image/png', 'image/jpeg', 'image/webp'].includes(screenshot.type))
            ) {
              setError('Choose a PNG, JPEG or WebP screenshot up to 5 MB.')

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
              rows={5}
              value={description}
              disabled={isPending}
              onChange={(e) => setDescription(e.target.value)}
              aria-describedby="feedback-privacy"
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="feedback-screenshot">Screenshot (optional)</FieldLabel>
            <Input
              id="feedback-screenshot"
              type="file"
              accept="image/png,image/jpeg,image/webp"
              ref={fileInput}
              disabled={isPending}
              onChange={(e) => setScreenshot(e.target.files?.[0] ?? null)}
              aria-describedby="feedback-file-help"
            />
            <FieldDescription id="feedback-file-help">
              PNG, JPEG or WebP, up to 5 MB. Remove any private information before attaching.
            </FieldDescription>
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
