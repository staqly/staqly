'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, ArrowRight, Download, KeyRound, LifeBuoy } from 'lucide-react'
import { useRouter, useSearchParams } from 'next/navigation'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { z } from 'zod'

import { Callout } from '@/components/common/callout'
import { AckCheckbox, useAckGate } from '@/components/common/confirm-name-field'
import { SecretField } from '@/components/common/secret-field'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { api, ApiError, errorMessage, setCsrfToken, type RecoverResult } from '@/lib/api'
import { downloadText } from '@/lib/format'
import { safeNextPath } from '@/lib/safe'
import { rememberApiKey } from '@/lib/session-key'

const schema = z.object({
  recovery_key: z.string().trim().min(1, 'Paste your recovery key'),
})

type Values = z.infer<typeof schema>

/**
 * "Lost your key?": the recovery key signs this browser in and is replaced by a new one, together with a new admin
 * API key. Both are shown once before the dashboard opens.
 */
export function RecoverForm({ onBack }: { onBack?: () => void }) {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const qc = useQueryClient()
  const [result, setResult] = useState<RecoverResult | null>(null)
  const form = useForm<Values>({ resolver: zodResolver(schema), defaultValues: { recovery_key: '' } })
  const { register, handleSubmit, formState, setError } = form

  const recover = useMutation({
    mutationFn: (v: Values) => api.recover(v.recovery_key.trim()),
    onSuccess: (res) => {
      setCsrfToken(res.csrf_token)
      // Possibly another workspace: nothing cached for the previous session may leak into it.
      rememberApiKey(null)
      qc.clear()
      setResult(res)
    },
    onError: (err) => {
      setError('recovery_key', {
        message:
          err instanceof ApiError && err.status === 401
            ? 'That recovery key is not valid. Each recovery key works only once.'
            : err instanceof ApiError && err.isRateLimited
              ? 'Too many attempts. Wait a minute and try again.'
              : errorMessage(err),
      })
    },
  })

  if (result) {
    return <RecoveredKeys result={result} />
  }

  return (
    <form
      onSubmit={(e) => {
        if (recover.isPending) {
          e.preventDefault()

          return
        }

        void handleSubmit((v) => recover.mutate(v))(e)
      }}
      className="grid gap-6"
      noValidate
    >
      <div>
        <div className="mb-4 flex size-10 items-center justify-center rounded-xl border bg-card shadow-xs">
          <LifeBuoy className="size-5 text-brand" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Recover your workspace</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Lost your API key? Paste the recovery key you saved when you created the workspace. You get a new admin API
          key and a new recovery key.
        </p>
      </div>
      <FieldGroup>
        <Field data-invalid={!!formState.errors.recovery_key}>
          <FieldLabel htmlFor="recovery-key" required>
            Recovery key
          </FieldLabel>
          <Textarea
            id="recovery-key"
            rows={3}
            spellCheck={false}
            autoComplete="off"
            placeholder="staqly-recovery-XXXXX-XXXXX-..."
            className="font-mono text-base sm:text-sm"
            aria-invalid={!!formState.errors.recovery_key}
            aria-required
            {...register('recovery_key')}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                // Through the form, like the button: nothing is sent twice while a check is running.
                e.currentTarget.form?.requestSubmit()
              }
            }}
          />
          <FieldDescription>
            A recovery key works once: using it replaces it with a new one and signs out every other browser. You get a
            new admin key; on the Free plan it replaces your old one.
          </FieldDescription>
          <FieldError errors={[formState.errors.recovery_key]} />
        </Field>
      </FieldGroup>
      <Button type="submit" size="lg" className="h-11" disabled={recover.isPending}>
        {recover.isPending ? 'Checking...' : 'Recover access'} <ArrowRight />
      </Button>
      {onBack && (
        <Button type="button" variant="ghost" className="h-11" onClick={onBack}>
          <ArrowLeft /> Sign in with your API key
        </Button>
      )}
    </form>
  )
}

function RecoveredKeys({ result }: { result: RecoverResult }) {
  const router = useRouter()
  const params = useSearchParams()
  const ack = useAckGate('ack-recovered', 'Confirm that you saved your new keys')
  const apiKey = result.api_key.key ?? ''

  function download() {
    downloadText(
      'staqly-recovered-keys.txt',
      [
        `staqly keys issued by recovery on ${new Date().toISOString()}`,
        '',
        `API key (${result.api_key.name}; scopes: ${result.api_key.scopes.join(', ')}). Signs you in to the dashboard:`,
        apiKey,
        '',
        'Recovery key. Works once, for when you lose the API key; replaces the one you just used:',
        result.recovery_key,
        '',
        'Store this file in a password manager and delete it from your downloads folder.',
      ].join('\n'),
    )
  }

  return (
    <div className="grid gap-6" data-testid="recovered-secrets">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Save your new keys</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          You are signed in. These keys are shown <strong>only once</strong>.
        </p>
      </div>

      <div className="grid gap-2">
        <Label className="flex flex-wrap items-center gap-1.5">
          <KeyRound className="size-3.5" /> API key
          <span className="font-normal text-muted-foreground">(admin, signs you in)</span>
        </Label>
        <SecretField value={apiKey} label="API key" />
      </div>

      <div className="grid gap-2">
        <Label className="flex flex-wrap items-center gap-1.5">
          <LifeBuoy className="size-3.5" /> New recovery key
          <span className="font-normal text-muted-foreground">(for you, keep offline)</span>
        </Label>
        <SecretField value={result.recovery_key} label="Recovery key" />
      </div>

      <Callout role="alert">
        The recovery key you just used <strong>no longer works</strong>. Every other browser was signed out; your other
        API keys still work, so revoke any that may have leaked in Settings.
      </Callout>

      <Button type="button" variant="outline" size="lg" className="h-11" onClick={download}>
        <Download /> Download keys as .txt
      </Button>

      <AckCheckbox {...ack.checkbox} className="min-h-11 rounded-lg border p-3">
        I have saved my new API key and recovery key
      </AckCheckbox>

      <Button
        size="lg"
        className="h-11"
        onClick={() => ack.confirm(() => router.replace(safeNextPath(params.get('next'), window.location.origin)))}
      >
        Open dashboard <ArrowRight />
      </Button>
    </div>
  )
}
