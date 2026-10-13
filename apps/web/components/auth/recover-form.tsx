'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, LifeBuoy } from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, errorMessage, setCsrfToken } from '@/lib/api'
import { useSetupStatus } from '@/lib/queries'
import { safeNextPath } from '@/lib/safe'
import { rememberApiKey } from '@/lib/session-key'

import { AuthLayout } from './auth-layout'

const schema = z.object({
  recovery_key: z.string().trim().min(1, 'Paste your recovery key'),
})

type Values = z.infer<typeof schema>

export function RecoverForm() {
  const router = useRouter()
  const params = useSearchParams()
  const qc = useQueryClient()
  const status = useSetupStatus()
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { recovery_key: '' } })
  const { register, handleSubmit, formState, setError } = form

  // Local installs have no sign-in. In cloud mode there are many workspaces, so
  // "initialized" means nothing here: always offer sign-in plus "create one".
  useEffect(() => {
    if (status.data?.mode === 'local') {
      router.replace('/inbox')
    }
  }, [status.data, router])
  const canSignUp = status.data?.signup_open !== false || !!status.data?.setup_token_required

  const recover = useMutation({
    mutationFn: (v: Values) => api.recover(v.recovery_key.trim()),
    onSuccess: (res) => {
      setCsrfToken(res.csrf_token)
      // Possibly another workspace: nothing cached for the previous session may leak into it.
      rememberApiKey(null)
      qc.clear()
      router.replace(safeNextPath(params.get('next'), window.location.origin))
    },
    onError: (err) => {
      setError('recovery_key', {
        message:
          err instanceof ApiError && err.status === 401
            ? 'That recovery key is not valid for this workspace.'
            : err instanceof ApiError && err.isRateLimited
              ? 'Too many attempts. Wait a minute and try again.'
              : errorMessage(err),
      })
    },
  })

  return (
    <AuthLayout>
      <form onSubmit={handleSubmit((v) => recover.mutate(v))} className="grid gap-6" noValidate>
        <div>
          <div className="mb-4 flex size-10 items-center justify-center rounded-xl border bg-card shadow-xs">
            <LifeBuoy className="size-5 text-brand" />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight">Sign in to your workspace</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Each workspace has its own recovery key. Paste the key of the workspace you want to open — the one you saved
            when you created it.
          </p>
        </div>
        <FieldGroup>
          <Field data-invalid={!!formState.errors.recovery_key}>
            <FieldLabel htmlFor="recovery-key">Recovery key</FieldLabel>
            <Textarea
              id="recovery-key"
              rows={3}
              spellCheck={false}
              autoComplete="off"
              placeholder="staqly-recovery-XXXXX-XXXXX-…"
              className="font-mono text-base sm:text-sm"
              aria-invalid={!!formState.errors.recovery_key}
              {...register('recovery_key')}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  void handleSubmit((v) => recover.mutate(v))()
                }
              }}
            />
            <FieldDescription>
              Your recovery key keeps working after you use it. Rotate it in Settings if you think it leaked.
            </FieldDescription>
            <FieldError errors={[formState.errors.recovery_key]} />
          </Field>
        </FieldGroup>
        <Button type="submit" size="lg" className="h-11" disabled={recover.isPending}>
          {recover.isPending ? 'Checking…' : 'Sign in'} <ArrowRight />
        </Button>
        {canSignUp && (
          <p className="text-center text-sm text-muted-foreground">
            No workspace yet?{' '}
            <Link href="/setup" className="font-medium text-foreground underline-offset-4 hover:underline">
              Create one free
            </Link>{' '}
            — no card, no email.
          </p>
        )}
      </form>
    </AuthLayout>
  )
}
