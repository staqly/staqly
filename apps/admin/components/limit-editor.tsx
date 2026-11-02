'use client'

import { useMutation } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { errorText } from '@/lib/api'
import { fromInput, isSwitch, toInput, unitOf, UNLIMITED } from '@/lib/limits'

/**
 * Edits one limit's value: a number in its unit, or Unlimited (data_export: on or off). onClear, when given, offers
 * to take the value away (a plan's or a workspace's own value); withNote asks why (a workspace's own value).
 */
export function LimitEditor({
  limitKey,
  title,
  description,
  value,
  withNote,
  note: initialNote = '',
  onSave,
  onClear,
  clearLabel,
  onClose,
}: {
  limitKey: string
  title: string
  description: string
  value: number
  withNote?: boolean
  note?: string
  onSave: (value: number, note: string) => Promise<unknown>
  onClear?: () => Promise<unknown>
  clearLabel?: string
  onClose: () => void
}) {
  const [unlimited, setUnlimited] = useState(value === UNLIMITED)
  const [text, setText] = useState(value === UNLIMITED ? '' : toInput(limitKey, value))
  const [on, setOn] = useState(value !== 0)
  const [note, setNote] = useState(initialNote)
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: (v: number) => onSave(v, note.trim()),
    onSuccess: () => {
      toast.success('Limit saved')
      onClose()
    },
    onError: (e) => toast.error(errorText(e)),
  })
  const clear = useMutation({
    mutationFn: () => onClear!(),
    onSuccess: () => {
      toast.success('Limit removed')
      onClose()
    },
    onError: (e) => toast.error(errorText(e)),
  })
  const unit = unitOf(limitKey)

  function submit(e: FormEvent) {
    e.preventDefault()

    if (isSwitch(limitKey)) {
      save.mutate(on ? 1 : 0)

      return
    }

    if (unlimited) {
      save.mutate(UNLIMITED)

      return
    }

    const v = fromInput(limitKey, text)

    if (v === null) {
      setError('A number, 0 or more')

      return
    }

    save.mutate(v)
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          {isSwitch(limitKey) ? (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={on} onChange={(e) => setOn(e.target.checked)} className="size-4" />
              On
            </label>
          ) : (
            <>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={unlimited}
                  onChange={(e) => setUnlimited(e.target.checked)}
                  className="size-4"
                />
                Unlimited
              </label>
              {!unlimited && (
                <div className="space-y-2">
                  <Label htmlFor="limit-value">
                    Value{unit ? ` (${unit.label === '$' ? 'US dollars' : unit.label})` : ''}
                  </Label>
                  <Input
                    id="limit-value"
                    inputMode="decimal"
                    value={text}
                    aria-invalid={!!error || undefined}
                    onChange={(e) => {
                      setText(e.target.value)
                      setError('')
                    }}
                  />
                  {error && <p className="text-sm text-destructive">{error}</p>}
                </div>
              )}
            </>
          )}
          {withNote && (
            <div className="space-y-2">
              <Label htmlFor="limit-note">Why (shown here only)</Label>
              <Input
                id="limit-note"
                value={note}
                placeholder="A deal, a test account..."
                onChange={(e) => setNote(e.target.value)}
              />
            </div>
          )}
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={save.isPending}>
              Save
            </Button>
            {onClear && (
              <Button type="button" variant="outline" disabled={clear.isPending} onClick={() => clear.mutate()}>
                {clearLabel}
              </Button>
            )}
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
