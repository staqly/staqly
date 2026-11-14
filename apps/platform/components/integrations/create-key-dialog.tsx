'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CalendarDays } from 'lucide-react'
import { useState } from 'react'

import { Callout } from '@/components/common/callout'
import { DatePicker } from '@/components/common/date-picker'
import { MultiSelect } from '@/components/common/multi-select'
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
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectSeparator, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useNow } from '@/hooks/use-now'
import { ALL_SCOPES, api, type APIKey, type Scope } from '@/lib/api'
import { fieldsOf, focusFirstInvalid, formApiError, useFormErrors } from '@/lib/form-errors'
import { qk, useRole } from '@/lib/queries'
import { SCOPE_INFO, scopeGroup } from '@/lib/scope-info'
import { SCOPE_PRESETS } from '@/lib/scope-presets'
import { rememberApiKey } from '@/lib/session-key'
import { cn } from '@/lib/utils'

function sameSet(a: string[], b: string[]) {
  return a.length === b.length && a.every((x) => b.includes(x))
}

const HOUR = 3_600_000
const DAY = 24 * HOUR
const YEAR = 365 * DAY

/** How long a new key works: [id, label, ms]. A year is the longest; "On a day you pick" allows a day to a year. */
const KEY_LIFETIMES: [string, string, number][] = [
  ['hour', '1 hour', HOUR],
  ['8h', '8 hours', 8 * HOUR],
  ['day', '1 day', DAY],
  ['week', '1 week', 7 * DAY],
  ['month', '1 month', 30 * DAY],
  ['year', '1 year', YEAR],
]

export function CreateKeyDialog({
  open,
  onOpenChange,
  availableScopes,
  onCreated,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  availableScopes: Scope[]
  onCreated: (k: APIKey) => void
}) {
  const { guest } = useRole()
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<Scope[]>(SCOPE_PRESETS[0].scopes)
  const [lifetime, setLifetime] = useState<string>('month')
  const [expires, setExpires] = useState('')
  const now = useNow(true, 60_000)
  const form = useFormErrors<'name' | 'scopes' | 'expires_at'>({
    name: name.trim() ? undefined : 'Give the key a name so you know where it is used',
    scopes: scopes.length ? undefined : 'Select at least one scope',
    expires_at: lifetime === 'custom' && !expires ? 'Pick the day the key stops working' : undefined,
  })
  // Every key expires: a preset from an hour to a year (the longest), or a day of your own within a year.
  const expiresAt = () => {
    const preset = KEY_LIFETIMES.find(([id]) => id === lifetime)

    return preset ? new Date(Date.now() + preset[2]).toISOString() : expires
  }
  const nameError = form.errors.name
  const scopesError = form.errors.scopes
  const expiresError = form.errors.expires_at

  const create = useMutation({
    mutationFn: () => api.createApiKey({ name: name.trim(), scopes, expires_at: expiresAt() }),
    onSuccess: (k) => {
      void qc.invalidateQueries({ queryKey: qk.apiKeys })

      if (k.key) {
        rememberApiKey({ id: k.id, name: k.name, key: k.key })
      }

      setName('')
      setExpires('')
      setLifetime('month')
      setScopes(SCOPE_PRESETS[0].scopes)
      form.reset()
      onOpenChange(false)
      onCreated(k)
    },
    onError: (e) => form.setServerErrors(formApiError(e, fieldsOf('name', 'scopes', 'expires_at'))),
  })

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()

    if (!form.submit()) {
      focusFirstInvalid(e.currentTarget)

      return
    }

    create.mutate()
  }

  const scopeList = availableScopes.length ? availableScopes : ALL_SCOPES

  // The dialog's own close (Esc, outside, X) and Cancel both clear the errors.
  function handleOpenChange(o: boolean) {
    if (!o) {
      form.reset()
    }

    onOpenChange(o)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <DialogHeader>
            <DialogTitle>Create API key</DialogTitle>
            <DialogDescription>
              Give each agent or automation its own key with only the scopes it needs.
              {guest &&
                ' This key expires with your guest session at the latest and stops working if that session is revoked.'}
            </DialogDescription>
          </DialogHeader>
          <Field data-invalid={!!nameError}>
            <FieldLabel htmlFor="key-name" required>
              Name
            </FieldLabel>
            <Input
              id="key-name"
              value={name}
              placeholder="n8n production"
              onChange={(e) => {
                setName(e.target.value)
                form.touch('name')
              }}
              aria-invalid={!!nameError}
              aria-required
            />
            <FieldError>{nameError}</FieldError>
          </Field>
          <div className="grid gap-2">
            <Label>Preset</Label>
            <div className="grid gap-2 sm:grid-cols-2">
              {SCOPE_PRESETS.map((p) => {
                const active = sameSet(p.scopes, scopes)

                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => {
                      setScopes(p.scopes.filter((s) => scopeList.includes(s)))
                      form.touch('scopes')
                    }}
                    aria-pressed={active}
                    className={cn(
                      'rounded-lg border p-2.5 text-left transition-colors hover:bg-muted/50',
                      active && 'border-brand bg-brand-soft hover:bg-brand-soft',
                    )}
                  >
                    <div className="text-sm font-medium">{p.label}</div>
                    <div className="text-xs text-muted-foreground">{p.description}</div>
                  </button>
                )
              })}
            </div>
          </div>
          <Field data-invalid={!!scopesError}>
            <FieldLabel htmlFor="key-scopes" required>
              Scopes
            </FieldLabel>
            {/* Grouped by what they touch (Posts, Media, ...): the preset fills them, the list fine-tunes. */}
            <MultiSelect
              id="key-scopes"
              label="Scopes"
              value={scopes}
              onChange={(v) => {
                setScopes(v as Scope[])
                form.touch('scopes')
              }}
              options={scopeList.map((sc) => ({
                value: sc,
                label: sc,
                hint: SCOPE_INFO[sc],
                group: scopeGroup(sc),
              }))}
              placeholder="Choose scopes"
              mono
              invalid={!!scopesError}
            />
            <FieldError id="key-scopes-error">{scopesError}</FieldError>
          </Field>
          {scopes.includes('admin') && (
            <Callout size="xs">
              Admin keys can create and revoke other keys and manage sessions. Only use them for trusted tooling.
            </Callout>
          )}
          <Field data-invalid={!!expiresError}>
            <FieldLabel htmlFor="key-expires">Expires</FieldLabel>
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={lifetime}
                onValueChange={(v) => {
                  setLifetime(v)
                  form.touch('expires_at')
                }}
              >
                <SelectTrigger id="key-expires" className="w-44">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KEY_LIFETIMES.map(([id, label]) => (
                    <SelectItem key={id} value={id}>
                      {label}
                    </SelectItem>
                  ))}
                  <SelectSeparator />
                  <SelectItem value="custom">
                    <CalendarDays /> Pick a day
                  </SelectItem>
                </SelectContent>
              </Select>
              {lifetime === 'custom' && (
                <DatePicker
                  value={expires}
                  onChange={(v) => {
                    setExpires(v)
                    form.touch('expires_at')
                  }}
                  minDate={new Date(now + DAY)}
                  maxDate={new Date(now + YEAR)}
                  placeholder="Pick a day"
                  invalid={!!expiresError}
                  className="w-fit"
                />
              )}
            </div>
            <FieldError>{expiresError}</FieldError>
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? 'Creating...' : 'Create key'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
