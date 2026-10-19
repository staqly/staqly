'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, ExternalLink, KeyRound, PlugZap } from 'lucide-react'
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
import { qk, useMode, useProviders } from '@/lib/queries'

import { fieldValues, ProviderFields } from './provider-fields'
import { ProviderGrid } from './provider-grid'

/**
 * Local mode and a platform that signs in with OAuth: the person registers their own app at the platform and
 * pastes its client id and secret here, once. Cloud servers bring their own apps, so this never shows there.
 */
function DeveloperAppForm({ provider, onSaved }: { provider: Provider; onSaved: () => void }) {
  const qc = useQueryClient()
  const [clientId, setClientId] = useState('')
  const [secret, setSecret] = useState('')
  const save = useMutation({
    mutationFn: () => api.setDeveloperApp(provider.id, { client_id: clientId.trim(), client_secret: secret.trim() }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.providers })
      namedToast(provider.name, 'app saved', 'You can connect accounts now.')
      onSaved()
    },
    onError: (e) => toastApiError(e),
  })
  const redirect = typeof window !== 'undefined' ? `${window.location.origin}/channels/callback/${provider.id}` : ''

  return (
    <form
      className="grid gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate()
      }}
    >
      <div className="rounded-xl border bg-muted/40 p-3 text-sm">
        <p className="flex items-center gap-2 font-medium">
          <KeyRound className="size-4" /> {provider.name} needs your own app first
        </p>
        <p className="mt-1 text-muted-foreground">
          Register an app at {provider.name}, allow this address as its redirect, then paste its keys. Staqly keeps them
          sealed and uses them only to sign accounts in.
        </p>
        <p className="mt-2 font-mono text-xs break-all select-all">{redirect}</p>
        {provider.docs_url && (
          <a
            href={provider.docs_url}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-2 inline-flex items-center gap-1 text-xs underline underline-offset-2 hover:no-underline"
          >
            Step by step for {provider.name} <ExternalLink className="size-3" aria-hidden />
          </a>
        )}
      </div>
      <Field>
        <FieldLabel htmlFor="app-client-id" required>
          Client ID
        </FieldLabel>
        <Input
          id="app-client-id"
          value={clientId}
          onChange={(e) => setClientId(e.target.value)}
          autoComplete="off"
          aria-required
        />
      </Field>
      <Field>
        <FieldLabel htmlFor="app-client-secret" required>
          Client secret
        </FieldLabel>
        <Input
          id="app-client-secret"
          type="password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          autoComplete="off"
          aria-required
        />
      </Field>
      <div className="flex justify-end">
        <Button type="submit" disabled={save.isPending || !clientId.trim() || !secret.trim()}>
          {save.isPending ? 'Saving...' : 'Save app'}
        </Button>
      </div>
    </form>
  )
}

/** Connect one account of a chosen provider: OAuth sends the browser to the platform; token providers take their fields here. */
function ConnectForm({
  provider,
  reconnect,
  onDone,
}: {
  provider: Provider
  reconnect: Channel | null
  onDone: () => void
}) {
  const qc = useQueryClient()
  const { isLocal } = useMode()
  const [values, setValues] = useState<Record<string, string>>({})
  const [name, setName] = useState(reconnect?.name ?? '')
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const [appSaved, setAppSaved] = useState(false)
  // Nothing on the other side names your own automation, so the person does.
  const nameRequired = provider.auth === 'none'
  // A working channel opened from Edit connection (not a broken one being reconnected).
  const editing = !!reconnect && reconnect.status !== 'needs_reconnect'
  const needsApp = provider.auth === 'oauth2' && isLocal && !provider.app_configured && !appSaved
  const start = useMutation({
    mutationFn: () => api.oauthStart(provider.id),
    onSuccess: ({ url }) => {
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

  if (needsApp) {
    return <DeveloperAppForm provider={provider} onSaved={() => setAppSaved(true)} />
  }

  if (provider.auth === 'oauth2') {
    return (
      <div className="grid gap-4">
        <p className="text-sm text-muted-foreground">
          {provider.name} opens in this tab to ask which account to connect. You come back here when it is done.
        </p>
        <div>
          <Button onClick={() => start.mutate()} disabled={start.isPending} data-testid="oauth-start">
            <PlugZap /> {start.isPending ? 'Opening...' : `Connect with ${provider.name}`}
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
      {!reconnect && (
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
