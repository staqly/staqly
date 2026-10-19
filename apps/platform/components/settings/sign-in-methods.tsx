'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Fingerprint, KeyRound, Mail, Plus } from 'lucide-react'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useState, useSyncExternalStore } from 'react'
import { toast } from 'sonner'

import { EmailAddressForm, EmailCodeForm } from '@/components/auth/email-sign-in'
import { GoogleMark } from '@/components/auth/google-mark'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { ErrorState } from '@/components/common/error-state'
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
import { Skeleton } from '@/components/ui/skeleton'
import { api, ApiError, errorMessage, type EmailCodeSent, type SignInMethod } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { createPasskey, deviceName, passkeyCancelled, passkeysSupported } from '@/lib/passkeys'
import { useSetupStatus } from '@/lib/queries'

import { SettingsCard as Card } from './settings-card'

const KEY = ['sign-in-methods'] as const

const emptySubscribe = () => () => {}

const PROVIDER_LABEL: Record<string, string> = { google: 'Google', email: 'Email code', passkey: 'Passkey' }

/**
 * How the signed-in person signs in: Google, one email address (changed with a code to the old and to the new
 * address, never a second one added) and passkeys; any of them removed while another remains.
 */
export function SignInMethodsCard() {
  const qc = useQueryClient()
  const status = useSetupStatus()
  const methods = useQuery({ queryKey: KEY, queryFn: async () => (await api.signInMethods()).data })
  const [adding, setAdding] = useState(false)
  const [changing, setChanging] = useState<string | null>(null)
  const [passkey, setPasskey] = useState<{ id?: string; name: string } | null>(null)
  const [removing, setRemoving] = useState<SignInMethod | null>(null)
  // The browser's WebAuthn support is only known on the client.
  const canPasskey = useSyncExternalStore(emptySubscribe, passkeysSupported, () => false)

  useLinkOutcome()
  const google = useMutation({
    mutationFn: api.startGoogleLink,
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => toastApiError(e),
  })
  const remove = useMutation({
    mutationFn: (m: SignInMethod) => api.removeSignInMethod(m),
    onSuccess: (res) => {
      qc.setQueryData(KEY, res.data)
      toast.success('Sign-in method removed')
    },
    onError: (e) => toastApiError(e),
  })

  const list = methods.data ?? []
  const only = list.length <= 1
  const email = list.find((m) => m.provider === 'email')

  return (
    <Card
      title="Sign-in methods"
      icon={<KeyRound />}
      description="The ways you sign in to Staqly. Keep at least one."
      action={
        <div className="flex flex-wrap gap-2">
          {status.data?.google_sign_in && !list.some((m) => m.provider === 'google') && (
            <Button
              size="sm"
              variant="outline"

              disabled={google.isPending}
              onClick={() => google.mutate()}
            >
              <GoogleMark /> Add Google
            </Button>
          )}
          {status.data?.passkey_sign_in && canPasskey && (
            <Button
              size="sm"
              variant="outline"

              onClick={() => setPasskey({ name: deviceName() })}
              data-testid="add-passkey"
            >
              <Fingerprint /> Add passkey
            </Button>
          )}
          {status.data?.email_sign_in &&
            methods.isSuccess &&
            (email ? (
              <Button
                size="sm"
                variant="outline"

                onClick={() => setChanging(email.subject)}
                data-testid="change-email"
              >
                <Mail /> Change email address
              </Button>
            ) : (
              <Button size="sm" onClick={() => setAdding(true)}>
                <Plus /> Add email address
              </Button>
            ))}
        </div>
      }
    >
      {methods.isPending ? (
        <Skeleton className="h-16" />
      ) : methods.isError ? (
        <ErrorState error={methods.error} onRetry={() => void methods.refetch()} />
      ) : (
        <ul className="grid divide-y" data-testid="sign-in-methods">
          {list.map((m) => (
            <li key={`${m.provider}:${m.subject}`} className="flex flex-wrap items-center gap-3 py-3">
              <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border bg-background [&_svg]:size-4">
                {m.provider === 'google' ? (
                  <GoogleMark />
                ) : m.provider === 'passkey' ? (
                  <Fingerprint className="text-muted-foreground" />
                ) : (
                  <Mail className="text-muted-foreground" />
                )}
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">
                  {PROVIDER_LABEL[m.provider] ?? m.provider}
                  <span className="font-normal text-muted-foreground">
                    {' '}
                    · {m.provider === 'passkey' ? m.name : m.email}
                  </span>
                </p>
                <p className="text-xs text-muted-foreground">
                  {m.last_login_at
                    ? `Last used ${formatDateTime(m.last_login_at)}`
                    : `Added ${formatDateTime(m.created_at)}`}
                </p>
              </div>
              {m.provider === 'passkey' && (
                <Button
                  variant="ghost"
                  size="sm"

                  onClick={() => setPasskey({ id: m.subject, name: m.name })}
                >
                  Rename
                </Button>
              )}
              <Button
                variant="ghost"
                size="sm"

                disabled={only || remove.isPending}
                title={only ? 'Add another way to sign in first' : undefined}
                onClick={() => setRemoving(m)}
              >
                Remove
              </Button>
            </li>
          ))}
        </ul>
      )}
      <AddEmailSheet
        open={adding}
        onOpenChange={setAdding}
        onAdded={(data) => {
          qc.setQueryData(KEY, data)
          setAdding(false)
          toast.success('Email address added')
        }}
      />
      <ChangeEmailSheet
        current={changing}
        onOpenChange={(o) => !o && setChanging(null)}
        onChanged={(data) => {
          qc.setQueryData(KEY, data)
          void qc.invalidateQueries({ queryKey: ['session'] })
          setChanging(null)
          toast.success('Email address changed')
        }}
      />
      <PasskeySheet
        passkey={passkey}
        onOpenChange={(o) => !o && setPasskey(null)}
        onSaved={(data, added) => {
          qc.setQueryData(KEY, data)
          setPasskey(null)
          toast.success(added ? 'Passkey added' : 'Passkey renamed')
        }}
      />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title="Remove this sign-in method?"
        description={removing ? removeDescription(removing) : ''}
        confirmLabel="Remove"
        variant="destructive"
        onConfirm={() => removing && remove.mutate(removing)}
      />
    </Card>
  )
}

function removeDescription(m: SignInMethod) {
  if (m.provider === 'passkey') {
    return `You will no longer sign in with the passkey ${m.name}. Remove it from the device too if you no longer need it.`
  }

  return `You will no longer sign in with ${m.provider === 'google' ? 'the Google account' : 'a code sent to'} ${m.email}.`
}

/** Google sends the browser back here with ?linked=google or ?error=<code>: say so once, then clean the address. */
function useLinkOutcome() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const linked = params.get('linked')
  const error = params.get('error')

  useEffect(() => {
    if (!linked && !error) {
      return
    }

    if (linked) {
      toast.success('Google account added')
    } else if (error === 'identity_taken') {
      toast.error('This Google account already signs in to another Staqly person.')
    } else {
      toast.error('The Google account could not be added. Try again.')
    }

    router.replace(pathname)
  }, [linked, error, router, pathname])
}

/** A modal: these forms are a field or two (an address, a code, a passkey name). Footer default: Cancel. */
function MethodDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  testId,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  title: string
  description: string
  children: React.ReactNode
  footer?: React.ReactNode
  testId?: string
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" data-testid={testId}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-5">{children}</div>
        <DialogFooter>
          {footer ?? (
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AddEmailSheet({
  open,
  onOpenChange,
  onAdded,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onAdded: (data: SignInMethod[]) => void
}) {
  const [codeFor, setCodeFor] = useState<{ email: string; sent: EmailCodeSent } | null>(null)

  return (
    <MethodDialog
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o)

        if (!o) {
          setCodeFor(null)
        }
      }}
      title="Add email address"
      description="We send a code to the address to check it is yours."
    >
      {codeFor ? (
        <EmailCodeForm
          compact
          email={codeFor.email}
          sent={codeFor.sent}
          submitLabel="Add address"
          verify={async (code) => (await api.verifyEmailLink(codeFor.email, code)).data}
          resend={() => api.startEmailLink(codeFor.email)}
          onDone={(data) => {
            setCodeFor(null)
            onAdded(data as SignInMethod[])
          }}
          onBack={() => setCodeFor(null)}
        />
      ) : (
        <EmailAddressForm
          compact
          send={api.startEmailLink}
          submitLabel="Send code"
          onSent={(email, sent) => setCodeFor({ email, sent })}
        />
      )}
    </MethodDialog>
  )
}

type ChangeStep =
  | { step: 'start' }
  | { step: 'current-code'; sent: EmailCodeSent }
  | { step: 'new-address' }
  | { step: 'new-code'; email: string; sent: EmailCodeSent }

/**
 * Changing the email sign-in address in two steps: a code to the current address proves it is still yours, then a
 * code to the new one proves that one. Only then does the address change.
 */
function ChangeEmailSheet({
  current,
  onOpenChange,
  onChanged,
}: {
  current: string | null
  onOpenChange: (o: boolean) => void
  onChanged: (data: SignInMethod[]) => void
}) {
  const [state, setState] = useState<ChangeStep>({ step: 'start' })
  const [error, setError] = useState('')
  const start = useMutation({
    mutationFn: () => api.startEmailChange(current ?? ''),
    onSuccess: (sent) => {
      setError('')
      setState({ step: 'current-code', sent })
    },
    onError: (e) => setError(changeErrorMessage(e)),
  })
  const close = (o: boolean) => {
    onOpenChange(o)

    if (!o) {
      setState({ step: 'start' })
      setError('')
    }
  }
  const first = state.step === 'start' || state.step === 'current-code'

  return (
    <MethodDialog
      open={!!current}
      onOpenChange={close}
      title="Change email address"
      description="You will sign in with the new address from now on. Your workspaces and other sign-in methods stay as they are."
      testId="change-email-sheet"
    >
      <ol className="grid gap-1 text-sm" aria-label="Steps">
        <li className={first ? 'font-medium' : 'text-muted-foreground'} aria-current={first ? 'step' : undefined}>
          1. Confirm your current address
        </li>
        <li className={first ? 'text-muted-foreground' : 'font-medium'} aria-current={first ? undefined : 'step'}>
          2. Confirm the new address
        </li>
      </ol>
      {state.step === 'start' && (
        <div className="grid gap-3">
          <p className="text-sm text-muted-foreground">
            First we send a code to <strong className="break-all text-foreground">{current}</strong> to check it is you.
          </p>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <Button className="justify-self-start" disabled={start.isPending} onClick={() => start.mutate()}>
            <Mail /> {start.isPending ? 'Sending code...' : 'Send code'}
          </Button>
        </div>
      )}
      {state.step === 'current-code' && current && (
        <EmailCodeForm
          compact
          email={current}
          sent={state.sent}
          submitLabel="Confirm"
          backLabel="Back"
          verify={(code) => api.verifyEmailChangeCurrent(current, code)}
          resend={() => api.startEmailChange(current)}
          onDone={() => setState({ step: 'new-address' })}
          onBack={() => setState({ step: 'start' })}
        />
      )}
      {state.step === 'new-address' && current && (
        <EmailAddressForm
          compact
          label="New email address"
          submitLabel="Send code"
          send={(email) => api.startEmailChangeNew(current, email)}
          onSent={(email, sent) => setState({ step: 'new-code', email, sent })}
        />
      )}
      {state.step === 'new-code' && current && (
        <EmailCodeForm
          compact
          email={state.email}
          sent={state.sent}
          submitLabel="Change address"
          verify={async (code) => (await api.verifyEmailChangeNew(current, state.email, code)).data}
          resend={() => api.startEmailChangeNew(current, state.email)}
          onDone={(data) => {
            setState({ step: 'start' })
            onChanged(data as SignInMethod[])
          }}
          onBack={() => setState({ step: 'new-address' })}
        />
      )}
    </MethodDialog>
  )
}

function changeErrorMessage(e: unknown) {
  if (e instanceof ApiError && e.code === 'code_cooldown') {
    return 'A code was just sent. Wait a few seconds before asking for another.'
  }

  return errorMessage(e)
}

/** Adding a passkey (the browser asks for the fingerprint, face or device PIN), or renaming one. */
function PasskeySheet({
  passkey,
  onOpenChange,
  onSaved,
}: {
  passkey: { id?: string; name: string } | null
  onOpenChange: (o: boolean) => void
  onSaved: (data: SignInMethod[], added: boolean) => void
}) {
  const adding = !passkey?.id

  return (
    <MethodDialog
      open={!!passkey}
      onOpenChange={onOpenChange}
      title={adding ? 'Add passkey' : 'Rename passkey'}
      description={
        adding
          ? 'Sign in with your fingerprint, face or device PIN instead of a code. Your browser or password manager keeps the passkey.'
          : 'The name helps you tell your passkeys apart.'
      }
      footer={
        <>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="passkey-form" data-testid="save-passkey">
            {adding ? 'Create passkey' : 'Save changes'}
          </Button>
        </>
      }
      testId="passkey-sheet"
    >
      {passkey && <PasskeyForm key={passkey.id ?? 'new'} passkey={passkey} onSaved={onSaved} />}
    </MethodDialog>
  )
}

function PasskeyForm({
  passkey,
  onSaved,
}: {
  passkey: { id?: string; name: string }
  onSaved: (data: SignInMethod[], added: boolean) => void
}) {
  const [name, setName] = useState(passkey.name)
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: async () => {
      if (passkey.id) {
        return (await api.renamePasskey(passkey.id, name.trim())).data
      }

      const challenge = await api.startPasskeyRegistration()
      const response = await createPasskey(challenge.options)

      return (
        await api.finishPasskeyRegistration({ challenge_id: challenge.challenge_id, name: name.trim(), response })
      ).data
    },
    onSuccess: (data) => onSaved(data, !passkey.id),
    onError: (e) =>
      setError(
        passkeyCancelled(e)
          ? 'No passkey was created. Try again when you are ready.'
          : e instanceof ApiError && e.code === 'passkey_exists'
            ? 'This device already has a passkey for your account.'
            : e instanceof DOMException && e.name === 'InvalidStateError'
              ? 'This device already has a passkey for your account.'
              : errorMessage(e),
      ),
  })

  return (
    <form
      id="passkey-form"
      noValidate
      onSubmit={(e) => {
        e.preventDefault()

        if (!name.trim()) {
          setError('Give the passkey a name, such as MacBook.')

          return
        }

        if (!save.isPending) {
          setError('')
          save.mutate()
        }
      }}
    >
      <Field data-invalid={!!error}>
        <FieldLabel htmlFor="passkey-name" required>
          Name
        </FieldLabel>
        <Input
          id="passkey-name"
          maxLength={100}
          placeholder="MacBook"
          value={name}
          aria-invalid={!!error}
          onChange={(e) => setName(e.target.value)}
        />
        <FieldDescription>
          {save.isPending && !passkey.id
            ? 'Follow your browser to create the passkey.'
            : 'Such as the device it is on.'}
        </FieldDescription>
        <FieldError>{error}</FieldError>
      </Field>
    </form>
  )
}
