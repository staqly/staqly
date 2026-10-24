'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link2, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { CopyButton } from '@/components/common/copy-button'
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
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api, type ChannelLink } from '@/lib/api'
import { apiFieldErrors } from '@/lib/form-errors'
import { formatDateTime, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { ProviderIcon } from '@/lib/providers'
import { useChannelGroups, useProviders } from '@/lib/queries'

const LINKS = ['channel-links'] as const

const EXPIRY: [string, string][] = [
  ['24', '24 hours'],
  ['72', '3 days'],
  ['168', '7 days'],
  ['720', '30 days'],
]

const USES: [string, string][] = [
  ['1', 'One channel'],
  ['3', 'Up to 3 channels'],
  ['5', 'Up to 5 channels'],
  ['10', 'Up to 10 channels'],
  ['50', 'Up to 50 channels'],
]

/**
 * "Invite someone to connect a channel": a link for a client or a colleague without access to Staqly. They open
 * it, pick a platform and sign in there; the channel lands in this workspace (in the chosen group). A short form,
 * so a modal; the open links are listed under it with Revoke.
 */
export function ConnectLinkButton() {
  const [open, setOpen] = useState(false)

  return (
    <>
      <Button
        variant="outline"
        size="icon"
        aria-label="Invite someone to connect a channel"
        onClick={() => setOpen(true)}
        data-testid="connect-link"
      >
        <Link2 />
      </Button>
      {open && <ConnectLinkDialog open={open} onOpenChange={setOpen} />}
    </>
  )
}

function ConnectLinkDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const qc = useQueryClient()
  const providers = useProviders()
  const groups = useChannelGroups()
  const links = useQuery({ queryKey: LINKS, queryFn: async () => (await api.channelLinks()).data ?? [] })
  const [note, setNote] = useState('')
  const [platforms, setPlatforms] = useState<string[]>([])
  const [group, setGroup] = useState('')
  const [hours, setHours] = useState('168')
  const [uses, setUses] = useState('1')
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const [made, setMade] = useState<ChannelLink | null>(null)
  const create = useMutation({
    mutationFn: () =>
      api.createChannelLink({
        note: note.trim() || undefined,
        providers: platforms.length ? platforms : undefined,
        group_id: group || undefined,
        expires_in_hours: Number(hours),
        max_uses: Number(uses),
      }),
    onSuccess: (l) => {
      setMade(l)
      setErrors({})
      void qc.invalidateQueries({ queryKey: LINKS })
    },
    onError: (e) => {
      const { errors: fields, complete } = apiFieldErrors(e, (f) =>
        f === 'note' || f === 'providers' || f === 'group_id' ? f : null,
      )

      setErrors(fields)

      if (!complete || Object.keys(fields).length === 0) {
        toastApiError(e)
      }
    },
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeChannelLink(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: LINKS }),
    onError: (e) => toastApiError(e),
  })
  // Your own automation is set up by the team, never through a link.
  const offered = (providers.data ?? []).filter((p) => p.status === 'available' && p.id !== 'custom')
  const openLinks = (links.data ?? []).filter((l) => l.status === 'open')
  const platformName = (id: string) => offered.find((p) => p.id === id)?.name ?? id

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" data-testid="connect-link-dialog">
        <DialogHeader>
          <DialogTitle>{made ? 'Send this link' : 'Invite someone to connect a channel'}</DialogTitle>
          <DialogDescription>
            {made
              ? 'Whoever opens it can connect an account to this workspace, and nothing else. It is shown only now.'
              : 'For a client or a colleague without access to Staqly: they open the link, sign in at the platform and the channel appears here.'}
          </DialogDescription>
        </DialogHeader>

        {made ? (
          <div className="grid gap-3">
            <div className="relative">
              <Input
                readOnly
                value={made.url ?? ''}
                aria-label="Connect link"
                className="pr-11 font-mono text-xs"
                data-testid="connect-link-url"
              />
              <CopyButton
                value={made.url ?? ''}
                label="Copy link"
                className="absolute top-1/2 right-1 -translate-y-1/2"
              />
            </div>
            <p className="text-xs text-muted-foreground">
              Works until {formatDateTime(made.expires_at)} for{' '}
              {made.max_uses === 1 ? 'one channel' : `up to ${made.max_uses} channels`}.
            </p>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setMade(null)}>
                Make another
              </Button>
              <Button onClick={() => onOpenChange(false)}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <form
            noValidate
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault()
              create.mutate()
            }}
          >
            <Field data-invalid={!!errors.note}>
              <FieldLabel htmlFor="connect-link-note">Note for them</FieldLabel>
              <Input
                id="connect-link-note"
                maxLength={300}
                placeholder="For ACME's Instagram"
                value={note}
                onChange={(e) => setNote(e.target.value)}
              />
              <FieldError>{errors.note}</FieldError>
            </Field>
            <Field data-invalid={!!errors.providers}>
              <FieldLabel htmlFor="connect-link-platforms">Platforms</FieldLabel>
              <MultiSelect
                id="connect-link-platforms"
                label="Platforms"
                anyLabel="Any platform"
                className="w-full"
                value={platforms}
                onChange={setPlatforms}
                options={offered.map((p) => ({
                  value: p.id,
                  label: p.name,
                  icon: <ProviderIcon id={p.id} name={p.name} />,
                }))}
              />
              <FieldDescription>Only these show on the link&apos;s page.</FieldDescription>
              <FieldError>{errors.providers}</FieldError>
            </Field>
            <div className="grid gap-4 sm:grid-cols-3">
              <Field data-invalid={!!errors.group_id}>
                <FieldLabel htmlFor="connect-link-group">Group</FieldLabel>
                <Select value={group || 'default'} onValueChange={(v) => setGroup(v === 'default' ? '' : v)}>
                  <SelectTrigger id="connect-link-group" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="default">Default</SelectItem>
                    {(groups.data ?? [])
                      .filter((g) => !g.default)
                      .map((g) => (
                        <SelectItem key={g.id} value={g.id}>
                          {g.name}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
                <FieldError>{errors.group_id}</FieldError>
              </Field>
              <Field>
                <FieldLabel htmlFor="connect-link-expiry">Works for</FieldLabel>
                <Select value={hours} onValueChange={setHours}>
                  <SelectTrigger id="connect-link-expiry" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {EXPIRY.map(([v, label]) => (
                      <SelectItem key={v} value={v}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field>
                <FieldLabel htmlFor="connect-link-uses">Connects</FieldLabel>
                <Select value={uses} onValueChange={setUses}>
                  <SelectTrigger id="connect-link-uses" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {USES.map(([v, label]) => (
                      <SelectItem key={v} value={v}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? 'Making the link...' : 'Make link'}
              </Button>
            </DialogFooter>
          </form>
        )}

        {openLinks.length > 0 && (
          <section aria-labelledby="open-connect-links" className="grid gap-2 border-t pt-4">
            <h3 id="open-connect-links" className="text-sm font-medium">
              Open links
            </h3>
            <ul className="grid gap-1.5" data-testid="connect-links">
              {openLinks.map((l) => (
                <li key={l.id} className="flex items-center gap-3 rounded-lg border px-3 py-2 text-sm">
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-medium" title={l.note || undefined}>
                      {l.note || (l.providers.length ? l.providers.map(platformName).join(', ') : 'Any platform')}
                    </p>
                    <p className="truncate text-xs text-muted-foreground" title={formatDateTime(l.expires_at)}>
                      {l.uses} of {l.max_uses} used · ends {timeAgo(l.expires_at)}
                      {l.created_by ? ` · by ${l.created_by}` : ''}
                    </p>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-destructive hover:text-destructive"
                    aria-label={`Revoke the link${l.note ? ` "${l.note}"` : ''}`}
                    tooltip="Revoke"
                    disabled={revoke.isPending}
                    onClick={() => revoke.mutate(l.id)}
                  >
                    <Trash2 />
                  </Button>
                </li>
              ))}
            </ul>
          </section>
        )}
      </DialogContent>
    </Dialog>
  )
}
