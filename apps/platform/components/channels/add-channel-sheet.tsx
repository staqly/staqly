'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Loader2, PlugZap } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { SkeletonList } from '@/components/common/skeleton-list'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { api, type Channel, type Provider } from '@/lib/api'
import { apiFieldErrors } from '@/lib/form-errors'
import { namedToast, toastApiError } from '@/lib/notify'
import { ProviderTile } from '@/lib/providers'
import { qk, useProviders } from '@/lib/queries'
import { clearOAuthReturn, setOAuthReturn } from '@/lib/welcome'

import { fieldValues, ProviderFields } from './provider-fields'
import { ProviderGrid } from './provider-grid'

// Only for Telegram through the server's bot: loaded when that panel opens.
const TelegramConnect = dynamic(() => import('./telegram-connect').then((m) => m.TelegramConnect), { ssr: false })

/** Connect one account of a chosen provider: OAuth sends the browser to the platform; token providers take their fields here. */
export function ConnectForm({
  provider,
  reconnect,
  onDone,
  oauthReturnTo,
}: {
  provider: Provider
  reconnect: Channel | null
  onDone: () => void
  /** Where the platform's callback lands instead of Channels (the welcome wizard). */
  oauthReturnTo?: string
}) {
  const qc = useQueryClient()
  const [values, setValues] = useState<Record<string, string>>({})
  const [name, setName] = useState(reconnect?.name ?? '')
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  // Telegram through the server's bot (/connect CODE in the chat), unless the person picks a bot of their own.
  const [ownBot, setOwnBot] = useState(false)
  // Nothing on the other side names your own automation, so the person does.
  const nameRequired = provider.auth === 'none'
  // A working channel opened from Edit connection (not a broken one being reconnected).
  const editing = !!reconnect && reconnect.status !== 'needs_reconnect'
  const start = useMutation({
    mutationFn: () => api.oauthStart(provider.id),
    onSuccess: ({ url }) => {
      // A connection started elsewhere (the welcome wizard) and abandoned on the platform's page must not send this
      // one back there.
      if (oauthReturnTo) {
        setOAuthReturn(oauthReturnTo)
      } else {
        clearOAuthReturn()
      }

      window.location.assign(url)
    },
    onError: (e) => toastApiError(e),
  })
  const connect = useMutation({
    mutationFn: () =>
      reconnect
        ? api.updateChannel(
            reconnect.id,
            editing
              ? { fields: fieldValues(provider.credential_fields ?? [], values) }
              : { fields: fieldValues(provider.credential_fields ?? [], values), status: 'connected' },
          )
        : api.connectChannel({
            provider: provider.id,
            name: name.trim() || undefined,
            fields: fieldValues(provider.credential_fields ?? [], values),
          }),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: qk.channels })
      namedToast(
        c.name,
        editing ? 'was updated' : reconnect ? 'is connected again' : 'is connected',
        c.handle || undefined,
      )
      onDone()
    },
    onError: (e) => {
      const { errors: fields, complete } = apiFieldErrors<string>(e, (k) => k)

      setErrors(fields)

      if (!complete) {
        toastApiError(e)
      }
    },
  })

  if (provider.bot && !ownBot) {
    return (
      <TelegramConnect
        bot={provider.bot}
        botName={provider.bot_name}
        onDone={onDone}
        onOwnBot={() => setOwnBot(true)}
      />
    )
  }

  if (provider.auth === 'oauth2') {
    // Busy from the click until the platform's page loads: after success the browser is still navigating away.
    const opening = start.isPending || start.isSuccess

    return (
      <div className="grid gap-4">
        <p className="text-sm text-muted-foreground">
          {provider.name} opens in this tab to ask which account to connect. You come back here when it is done.
        </p>
        <div>
          <Button onClick={() => start.mutate()} disabled={opening} aria-busy={opening} data-testid="oauth-start">
            {opening ? <Loader2 className="animate-spin" aria-hidden /> : <PlugZap />}{' '}
            {opening ? `Opening ${provider.name}...` : `Connect with ${provider.name}`}
          </Button>
        </div>
      </div>
    )
  }

  return (
    <form
      className="grid gap-4"
      noValidate
      onSubmit={(e) => {
        e.preventDefault()

        // Your own automation has no name of its own on the other side, so a new one needs one here.
        if (nameRequired && !reconnect && !name.trim()) {
          setErrors({ name: 'Name is required' })

          return
        }

        connect.mutate()
      }}
    >
      {/* Only your own automation needs a name here: every platform names its account, and a channel is renamed later
          in its settings. */}
      {!reconnect && nameRequired && (
        <Field data-invalid={!!errors.name}>
          <FieldLabel htmlFor="connect-name" required={nameRequired}>
            Name {!nameRequired && <span className="font-normal text-muted-foreground">(optional)</span>}
          </FieldLabel>
          <Input
            id="connect-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={nameRequired ? 'My n8n publisher' : `${provider.name} account`}
            aria-required={nameRequired}
            aria-invalid={!!errors.name}
          />
          <FieldDescription className="text-xs">
            {nameRequired
              ? 'How this channel shows up in staqly and in the composer.'
              : 'How this channel shows up in staqly. Left empty, staqly uses the name the platform gives.'}
          </FieldDescription>
          <FieldError>{errors.name}</FieldError>
        </Field>
      )}
      {provider.bot && ownBot && (
        <button
          type="button"
          onClick={() => setOwnBot(false)}
          className="justify-self-start text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
        >
          Connect with {provider.bot_name || provider.bot} instead (no token needed)
        </button>
      )}
      <ProviderFields
        idPrefix={`connect-${provider.id}`}
        fields={provider.credential_fields ?? []}
        values={values}
        onChange={(k, v) => setValues((vs) => ({ ...vs, [k]: v }))}
        errors={errors}
      />
      <div className="flex justify-end">
        <Button type="submit" disabled={connect.isPending} data-testid="connect-channel">
          <PlugZap /> {connect.isPending ? 'Saving...' : editing ? 'Save' : reconnect ? 'Reconnect' : 'Connect'}
        </Button>
      </div>
    </form>
  )
}

/**
 * Add channel: the platform grid first, then that platform's way in (OAuth, or its fields). Reconnecting an
 * existing channel skips the grid.
 */
export function AddChannelSheet({
  open,
  onOpenChange,
  reconnect,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  reconnect: Channel | null
}) {
  const providers = useProviders()
  const [picked, setPicked] = useState<string | null>(null)
  // Every opening starts at the platform grid, however it closed last time (a connection done, the page closing it).
  const [wasOpen, setWasOpen] = useState(open)

  if (open !== wasOpen) {
    setWasOpen(open)

    if (open) {
      setPicked(null)
    }
  }

  const providerId = reconnect?.provider ?? picked
  const provider = providers.data?.find((p) => p.id === providerId) ?? null

  return (
    <Sheet
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o)

        if (!o) {
          setPicked(null)
        }
      }}
    >
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle className="flex items-center gap-2">
            {provider && !reconnect && (
              <Button
                size="icon"
                variant="ghost"
                className="-ml-2 size-8"
                aria-label="Back to platforms"
                onClick={() => setPicked(null)}
              >
                <ArrowLeft />
              </Button>
            )}
            {provider ? (
              <>
                <ProviderTile id={provider.id} name={provider.name} className="size-7" />
                {reconnect
                  ? reconnect.status === 'needs_reconnect'
                    ? `Reconnect ${reconnect.name}`
                    : `Edit ${reconnect.name}`
                  : `Connect ${provider.name}`}
              </>
            ) : (
              'Add channel'
            )}
          </SheetTitle>
          <SheetDescription>
            {provider
              ? reconnect
                ? reconnect.status === 'needs_reconnect'
                  ? 'The platform stopped accepting the saved sign-in. Sign in again to keep publishing here.'
                  : 'Saved details are sealed and never shown again: enter every field you want to keep.'
                : 'One account, one channel. You can connect more later.'
              : 'Pick the platform. Greyed ones are on the way.'}
          </SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4">
          {providers.isLoading ? (
            <SkeletonList count={6} className="h-24 rounded-xl" />
          ) : providers.error ? (
            <ErrorState error={providers.error} onRetry={() => providers.refetch()} />
          ) : provider ? (
            <ConnectForm provider={provider} reconnect={reconnect} onDone={() => onOpenChange(false)} />
          ) : (
            <ProviderGrid providers={providers.data ?? []} onPick={(p) => setPicked(p.id)} />
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
