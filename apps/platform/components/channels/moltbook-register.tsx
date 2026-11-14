'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, PlugZap } from 'lucide-react'
import { useState } from 'react'

import { CopyButton } from '@/components/common/copy-button'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { apiFieldErrors } from '@/lib/form-errors'
import { namedToast, toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'

/**
 * Moltbook without an agent yet: the server registers one and connects it at once (Moltbook shows its key only once),
 * then its owner claims it on Moltbook, which keeps an unclaimed agent from posting.
 */
export function MoltbookRegister({ onDone, onOwnKey }: { onDone: () => void; onOwnKey: () => void }) {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const register = useMutation({
    mutationFn: () => api.registerMoltbookAgent({ name: name.trim(), description: description.trim() || undefined }),
    onSuccess: (r) => {
      void qc.invalidateQueries({ queryKey: qk.channels })
      namedToast(r.channel.name, 'is connected', r.channel.handle || undefined)
    },
    onError: (e) => {
      const { errors: fields, complete } = apiFieldErrors<string>(e, (k) => k)

      setErrors(fields)

      if (!complete) {
        toastApiError(e)
      }
    },
  })
  const claim = register.data && safeHttpUrl(register.data.claim_url)

  if (register.data) {
    return (
      <div className="grid gap-4" data-testid="moltbook-claim">
        <p className="text-sm">
          <span className="font-medium">{register.data.channel.handle}</span> is registered and connected. Claim it on
          Moltbook so it can post: Moltbook asks for your email and a post on X.
        </p>
        <div className="flex items-center gap-2 rounded-lg border bg-muted/40 py-1.5 pr-1.5 pl-3">
          <code className="min-w-0 flex-1 truncate font-mono text-sm" title={register.data.claim_url}>
            {register.data.claim_url}
          </code>
          <CopyButton value={register.data.claim_url} label="Copy the claim link" />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2">
          {claim && (
            <Button asChild variant="outline">
              <a href={claim} target="_blank" rel="noopener noreferrer">
                <ExternalLink /> Open claim page
              </a>
            </Button>
          )}
          <Button className="ml-auto" onClick={onDone}>
            Done
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

        if (!name.trim()) {
          setErrors({ name: 'Agent name is required' })

          return
        }

        register.mutate()
      }}
    >
      <Field data-invalid={!!errors.name}>
        <FieldLabel htmlFor="moltbook-name" required>
          Agent name
        </FieldLabel>
        <Input
          id="moltbook-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="my-agent"
          maxLength={100}
          aria-required
          aria-invalid={!!errors.name}
        />
        <FieldDescription className="text-xs">Its name on Moltbook, as in u/my-agent.</FieldDescription>
        <FieldError>{errors.name}</FieldError>
      </Field>
      <Field data-invalid={!!errors.description}>
        <FieldLabel htmlFor="moltbook-description">
          Description <span className="font-normal text-muted-foreground">(optional)</span>
        </FieldLabel>
        <Input
          id="moltbook-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What the agent posts about"
          maxLength={500}
          aria-invalid={!!errors.description}
        />
        <FieldError>{errors.description}</FieldError>
      </Field>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <button
          type="button"
          onClick={onOwnKey}
          className="min-w-0 flex-1 basis-40 text-left text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground pointer-coarse:-my-3 pointer-coarse:py-3"
        >
          Have an agent already? Paste its API key
        </button>
        <Button type="submit" disabled={register.isPending} data-testid="moltbook-register" className="ml-auto">
          <PlugZap /> {register.isPending ? 'Registering...' : 'Register agent'}
        </Button>
      </div>
    </form>
  )
}
