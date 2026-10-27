'use client'

import { Logo } from '@staqly/brand/logo'
import { ProviderTile } from '@staqly/brand/providers'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ArrowLeft, CheckCircle2, Link2Off, PlugZap } from 'lucide-react'
import { useState, useSyncExternalStore } from 'react'

import { AccountPicker } from '@/components/channels/account-picker'
import { fieldValues, ProviderFields } from '@/components/channels/provider-fields'
import { ProviderGrid } from '@/components/channels/provider-grid'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ApiError, api, errorMessage, type Provider } from '@/lib/api'
import { apiFieldErrors } from '@/lib/form-errors'
import { formatDateTime } from '@/lib/format'

/**
 * The page a connect link opens, for someone without access to Staqly: the workspace's name, the team's note and
 * the platforms the link allows. They sign in at the platform (or enter its token here) and the channel lands in
 * that workspace. No session is made; nothing else of the workspace is reachable. The token is in the fragment,
 * so it never reaches a server log; after a platform's sign-in the API sends the browser back here with
 * `?connected=`, `?pending=` (the sign-in may publish as several accounts: they pick which here) or `?error=`, and
 * the token again in the fragment.
 */

function subscribeLocation(onChange: () => void) {
  window.addEventListener('hashchange', onChange)
  window.addEventListener('popstate', onChange)

  return () => {
    window.removeEventListener('hashchange', onChange)
    window.removeEventListener('popstate', onChange)
  }
}

const readToken = () => window.location.hash.replace(/^#/, '')
const readSearch = () => window.location.search

function Card({ children }: { children: React.ReactNode }) {
  return (
    <div className="mx-auto grid w-full max-w-md justify-items-center gap-3 rounded-2xl border bg-card p-8 text-center wrap-anywhere">
      {children}
    </div>
  )
}

function Connected({ handle, workspace }: { handle: string; workspace: string }) {
  return (
    <Card>
      <CheckCircle2 className="size-9 text-brand" aria-hidden />
      <h1 className="text-lg font-semibold" data-testid="connected">
        Connected {handle}
        {workspace ? ` to ${workspace}` : ''}
      </h1>
      <p className="text-sm text-muted-foreground">You can close this page. Nothing else is needed from you.</p>
    </Card>
  )
}

function Unavailable({ message }: { message: string }) {
  return (
    <Card>
      <Link2Off className="size-8 text-muted-foreground" aria-hidden />
      <h1 className="text-lg font-semibold">This link can&apos;t be used</h1>
      <p className="text-sm text-muted-foreground">{message}</p>
    </Card>
  )
}

/**
 * A platform's fields, on this page: a token platform connects with them; one that signs in with many servers
 * (Mastodon) sends them along to its sign-in.
 */
function TokenForm({
  token,
  provider,
  onBack,
  onDone,
}: {
  token: string
  provider: Provider
  onBack: () => void
  onDone: (handle: string) => void
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const [failed, setFailed] = useState('')
  const connect = useMutation({
    mutationFn: async () => {
      const fields = fieldValues(provider.credential_fields ?? [], values)

      if (provider.auth === 'oauth2') {
        const { url } = await api.connectLinkOAuthStart(token, provider.id, fields)

        window.location.assign(url)

        return null
      }

      return api.connectViaLink(token, { provider: provider.id, fields })
    },
    onSuccess: (r) => {
      if (r) {
        onDone(r.handle || r.name)
      }
    },
    onError: (e) => {
      const { errors: fields, complete } = apiFieldErrors(e, (f) => (f.startsWith('fields.') ? f.slice(7) : null))

      setErrors(fields)
      setFailed(complete && Object.keys(fields).length > 0 ? '' : errorMessage(e))
    },
  })

  return (
    <form
      noValidate
      className="grid gap-4 rounded-2xl border bg-card p-5 sm:p-6"
      onSubmit={(e) => {
        e.preventDefault()
        connect.mutate()
      }}
    >
      <div className="flex items-center gap-3">
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Back to the platforms" onClick={onBack}>
          <ArrowLeft />
        </Button>
        <ProviderTile id={provider.id} name={provider.name} className="size-8 rounded-lg" />
        <h2 className="font-semibold">Connect {provider.name}</h2>
      </div>
      <ProviderFields
        idPrefix={`link-${provider.id}`}
        fields={provider.credential_fields ?? []}
        values={values}
        onChange={(k, v) => setValues((vs) => ({ ...vs, [k]: v }))}
        errors={errors}
      />
      {failed && (
        <p role="alert" className="text-sm text-destructive">
          {failed}
        </p>
      )}
      <div className="flex justify-end">
        <Button type="submit" disabled={connect.isPending} data-testid="link-connect">
          <PlugZap /> {connect.isPending ? 'Connecting...' : 'Connect'}
        </Button>
      </div>
    </form>
  )
}

/** Back to the platforms from the account picker: drops ?pending from the address (the token stays in the fragment). */
function leavePicker() {
  window.history.replaceState(null, '', window.location.pathname + window.location.hash)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

export function AddChannelPage() {
  const token = useSyncExternalStore(subscribeLocation, readToken, () => '')
  const search = useSyncExternalStore(subscribeLocation, readSearch, () => '')
  const params = new URLSearchParams(search)
  const [picked, setPicked] = useState<Provider | null>(null)
  const [done, setDone] = useState('')
  const [starting, setStarting] = useState('')
  const [startError, setStartError] = useState('')
  const returned = params.get('connected')
  const pendingID = params.get('pending')
  const [pickError, setPickError] = useState('')
  const returnError = params.get('error') || pickError
  const link = useQuery({
    queryKey: ['connect-link', token],
    queryFn: () => api.openConnectLink(token),
    enabled: !!token && !returned,
    retry: false,
  })
  const l = link.data
  const pending = useQuery({
    queryKey: ['connect-link-pending', token, pendingID],
    queryFn: () => api.pendingConnectionViaLink(token, pendingID!),
    enabled: !!token && !!pendingID && !returned && !pickError,
    retry: false,
  })
  const finish = useMutation({
    mutationFn: (ids: string[]) => api.finishPendingConnectionViaLink(token, pendingID!, ids),
    onSuccess: (r) => setDone(r.data.map((c) => c.handle || c.name).join(', ')),
    onError: (e) => setPickError(errorMessage(e)),
  })
  const pendingError = pending.error ? errorMessage(pending.error) : ''

  const start = async (p: Provider) => {
    setStartError('')

    if (p.auth !== 'oauth2' || p.credential_fields?.length) {
      setPicked(p)

      return
    }

    setStarting(p.id)

    try {
      const { url } = await api.connectLinkOAuthStart(token, p.id)

      window.location.assign(url)
    } catch (e) {
      setStarting('')
      setStartError(errorMessage(e))
    }
  }

  let body: React.ReactNode

  if (returned || done) {
    body = <Connected handle={returned || done} workspace={params.get('workspace') ?? l?.workspace ?? ''} />
  } else if (!token) {
    body = <Unavailable message="The link is incomplete. Open it again from the message it came in." />
  } else if (link.error) {
    body = (
      <Unavailable
        message={
          link.error instanceof ApiError && link.error.status === 429
            ? 'Too many tries from this network. Wait a few minutes and open the link again.'
            : `${errorMessage(link.error)}`
        }
      />
    )
  } else if (link.isLoading || !l) {
    body = <Skeleton className="mx-auto h-72 w-full max-w-2xl rounded-2xl" />
  } else if (pendingID && !pickError && (pending.isLoading || pending.data)) {
    const p = pending.data

    body = p ? (
      <AccountPicker
        provider={p.provider}
        name={l?.providers.find((x) => x.id === p.provider)?.name ?? p.provider}
        accounts={p.accounts}
        busy={finish.isPending}
        onConnect={(ids) => finish.mutate(ids)}
        onCancel={leavePicker}
      />
    ) : (
      <Skeleton className="mx-auto h-72 w-full max-w-lg rounded-2xl" />
    )
  } else if (picked) {
    body = (
      <div className="mx-auto w-full max-w-lg">
        <TokenForm token={token} provider={picked} onBack={() => setPicked(null)} onDone={setDone} />
      </div>
    )
  } else {
    body = (
      <div className="mx-auto grid w-full max-w-2xl gap-6">
        <div className="text-center">
          <h1 className="text-2xl font-semibold tracking-tight text-balance wrap-anywhere">
            Connect an account to <span className="text-brand-strong">{l.workspace}</span>
          </h1>
          {l.note && <p className="mt-2 wrap-anywhere text-muted-foreground">{l.note}</p>}
          <p className="mt-2 text-sm text-muted-foreground">
            Pick the platform, then sign in there. You only allow Staqly to post for {l.workspace}; you won&apos;t get
            access to anything else, and you can close this page when it&apos;s done.
          </p>
        </div>
        {(returnError || startError || pendingError) && (
          <p role="alert" className="rounded-xl border border-destructive/40 bg-destructive/5 px-4 py-3 text-sm">
            The account was not connected: {startError || returnError || pendingError}
          </p>
        )}
        {l.providers.length === 0 ? (
          <Unavailable message="None of the platforms this link offers can be connected right now. Ask whoever sent it." />
        ) : (
          <div aria-busy={!!starting}>
            <ProviderGrid
              providers={l.providers.map((p) => ({ id: p.id, name: p.name, status: 'available', auth: p.auth }))}
              onPick={(c) => {
                const p = l.providers.find((x) => x.id === c.id)

                if (p && !starting) {
                  void start(p)
                }
              }}
            />
          </div>
        )}
        <p className="text-center text-xs text-muted-foreground">
          This link works until {formatDateTime(l.expires_at)}.
        </p>
      </div>
    )
  }

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="border-b bg-card/60 backdrop-blur">
        <div className="mx-auto flex w-full max-w-6xl items-center gap-4 px-4 py-3 sm:px-6">
          <Logo className="text-lg" markClassName="size-7" />
          <span className="text-sm text-muted-foreground">Connect a channel</span>
        </div>
      </header>
      <main className="px-4 py-10 sm:px-6 sm:py-16">{body}</main>
    </div>
  )
}
