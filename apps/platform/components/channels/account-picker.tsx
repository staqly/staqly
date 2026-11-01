'use client'

import { PlugZap } from 'lucide-react'
import { useState } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { PlatformAccount } from '@/lib/api'

/**
 * After signing in at a platform whose sign-in may publish as several accounts (Facebook Pages, LinkedIn Pages,
 * YouTube channels, Google Business locations...), the person picks which ones to connect; each becomes its own
 * channel. Nothing is ticked for them. Shown as a dialog over the page the person came from.
 */
export function AccountPicker({
  provider,
  name,
  accounts,
  busy,
  error,
  onConnect,
  onCancel,
}: {
  provider: string
  /** The platform's name (Facebook, YouTube...). */
  name: string
  accounts: PlatformAccount[]
  busy: boolean
  error?: string
  onConnect: (ids: string[]) => void
  onCancel?: () => void
}) {
  const [chosen, setChosen] = useState<string[]>([])
  const toggle = (id: string, on: boolean) => setChosen((c) => (on ? [...c, id] : c.filter((x) => x !== id)))

  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o && !busy) {
          onCancel?.()
        }
      }}
    >
      <DialogContent className="sm:max-w-lg" showCloseButton={!!onCancel} data-testid="account-picker">
        <DialogHeader>
          <DialogTitle>Pick the {name} accounts to connect</DialogTitle>
          <DialogDescription>
            Your sign-in can post as each of these. Every account you pick becomes its own channel.
          </DialogDescription>
        </DialogHeader>
        <form
          noValidate
          className="grid min-w-0 gap-4 text-left"
          onSubmit={(e) => {
            e.preventDefault()

            if (chosen.length > 0) {
              onConnect(chosen)
            }
          }}
        >
          {accounts.length === 0 && (
            <p className="text-sm text-muted-foreground">
              This sign-in has no {name} accounts to post as. Sign in with another one, or check that it manages at
              least one.
            </p>
          )}
          <fieldset className="-mx-2 grid max-h-[50vh] gap-1 overflow-y-auto" aria-label={`${name} accounts`}>
            {accounts.map((a) => {
              const id = `account-${a.id}`

              return (
                <label
                  key={a.id}
                  htmlFor={id}
                  className="flex min-h-11 cursor-pointer items-center gap-3 rounded-xl px-2 py-2 hover:bg-muted/60"
                >
                  <Checkbox
                    id={id}
                    checked={chosen.includes(a.id)}
                    onCheckedChange={(v) => toggle(a.id, v === true)}
                    data-testid="account-option"
                  />
                  <ChannelAvatar
                    channel={{ id: a.id, provider, name: a.name, avatar_url: a.avatar_url }}
                    className="size-8"
                  />
                  <span className="grid min-w-0">
                    <span className="truncate text-sm font-medium">{a.name}</span>
                    {a.handle && <span className="truncate text-xs text-muted-foreground">{a.handle}</span>}
                  </span>
                </label>
              )
            })}
          </fieldset>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex flex-wrap justify-end gap-2">
            {onCancel && (
              <Button type="button" variant="outline" onClick={onCancel} disabled={busy}>
                Cancel
              </Button>
            )}
            <Button type="submit" disabled={busy || chosen.length === 0} data-testid="accounts-connect">
              <PlugZap />{' '}
              {busy ? 'Connecting...' : chosen.length > 1 ? `Connect ${chosen.length} accounts` : 'Connect account'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
