'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, KeyRound } from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api, ApiError, errorMessage, setCsrfToken } from '@/lib/api'
import { useSetupStatus } from '@/lib/queries'
import { safeNextPath } from '@/lib/safe'
import { rememberApiKey } from '@/lib/session-key'

import { AuthLayout } from './auth-layout'
import { RecoverForm } from './recover-form'

const schema = z.object({
  api_key: z.string().trim().min(1, 'Paste your API key'),
})

type Values = z.infer<typeof schema>

/**
 * The cloud sign-in page: an admin API key opens the workspace; "Lost your key?" switches to recovery with the
 * recovery key. Local installs have no sign-in.
 */
export function SignIn() {
  const router = useRouter()
  const status = useSetupStatus()
  const [recovering, setRecovering] = useState(false)

  useEffect(() => {
    if (status.data?.mode === 'local') {
      router.replace('/inbox')
    }
  }, [status.data, router])

  // In cloud mode there are many workspaces, so "initialized" means nothing here: always offer "create one".
  const canSignUp = status.data?.signup_open !== false || !!status.data?.setup_token_required

  return (
    <AuthLayout>
      {recovering ? (
        <RecoverForm onBack={() => setRecovering(false)} />
      ) : (
        <ApiKeyForm onLostKey={() => setRecovering(true)} canSignUp={canSignUp} />
      )}
    </AuthLayout>
  )
}

function ApiKeyForm({ onLostKey, canSignUp }: { onLostKey: () => void; canSignUp: boolean }) {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const router = useRouter()
  const params = useSearchParams()
  const qc = useQueryClient()
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { api_key: '' } })
  const { register, handleSubmit, formState, setError } = form

  const signIn = useMutation({
    mutationFn: (v: Values) => api.signInWithApiKey(v.api_key.trim()),
    onSuccess: (res) => {
      setCsrfToken(res.csrf_token)
      // Possibly another workspace: nothing cached for the previous session may leak into it.
      rememberApiKey(null)
      qc.clear()
      router.replace(safeNextPath(params.get('next'), window.location.origin))
    },
    onError: (err) => {
      setError('api_key', {
        message:
          err instanceof ApiError && err.status === 401
            ? 'That API key is not valid. It may have been revoked or expired.'
            : err instanceof ApiError && err.status === 403
              ? 'This API key cannot open the dashboard: only keys with the admin scope sign in. Agent keys stay API-only.'
              : err instanceof ApiError && err.isRateLimited
                ? 'Too many attempts. Wait a minute and try again.'
                : errorMessage(err),
      })
    },
  })

  return (
    <form
      onSubmit={(e) => {
        if (signIn.isPending) {
          e.preventDefault()

          return
        }

        void handleSubmit((v) => signIn.mutate(v))(e)
      }}
      className="grid gap-6"
      noValidate
    >
      <div>
        <div className="mb-4 flex size-10 items-center justify-center rounded-xl border bg-card shadow-xs">
          <KeyRound className="size-5 text-brand" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Sign in with your API key</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Paste the admin API key of the workspace you want to open: the one you saved when you created it, or any other
          key with the admin scope.
        </p>
      </div>
      <FieldGroup>
        <Field data-invalid={!!formState.errors.api_key}>
          <FieldLabel htmlFor="api-key" required>
            API key
          </FieldLabel>
          <Input
            id="api-key"
            type="password"
            spellCheck={false}
            autoComplete="off"
            placeholder="stk_..."
            className="h-11 font-mono text-base sm:text-sm"
            aria-invalid={!!formState.errors.api_key}
            aria-required
            {...register('api_key')}
          />
          <FieldDescription>Agent keys without the admin scope cannot open the dashboard.</FieldDescription>
          <FieldError errors={[formState.errors.api_key]} />
        </Field>
      </FieldGroup>
      <Button type="submit" size="lg" className="h-11" disabled={signIn.isPending}>
        {signIn.isPending ? 'Checking...' : 'Sign in'} <ArrowRight />
      </Button>
      <p className="text-center text-sm text-muted-foreground">
        <button
          type="button"
          onClick={onLostKey}
          className="inline-flex min-h-11 items-center font-medium text-foreground underline-offset-4 hover:underline"
        >
          Lost your key?
        </button>{' '}
        Use your recovery key.
      </p>
      {canSignUp && (
        <p className="text-center text-sm text-muted-foreground">
          No workspace yet?{' '}
          <Link href="/setup" className="font-medium text-foreground underline-offset-4 hover:underline">
            Create one free
          </Link>{' '}
          (no card, no email).
        </p>
      )}
    </form>
  )
}
