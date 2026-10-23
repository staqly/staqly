'use client'

import { PlugZap } from 'lucide-react'
import { useState } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import type { PlatformAccount } from '@/lib/api'

/**
 * After signing in at a platform whose sign-in may publish as several accounts (Facebook Pages, LinkedIn Pages,
 * YouTube channels, Google Business locations...), the person picks which ones to connect; each becomes its own
 * channel. Nothing is ticked for them.
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
    <form
      noValidate
      className="mx-auto grid w-full max-w-lg gap-4 rounded-2xl border bg-card p-5 text-left sm:p-6"
      onSubmit={(e) => {
        e.preventDefault()

        if (chosen.length > 0) {
          onConnect(chosen)
        }
      }}
    >
      <div>
        <h1 className="text-lg font-semibold">Pick the {name} accounts to connect</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Your sign-in can post as each of these. Every account you pick becomes its own channel.
        </p>
      </div>
      <fieldset className="grid gap-1" aria-label={`${name} accounts`}>
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
  )
}
