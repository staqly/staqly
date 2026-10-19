'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, KeyRound, Sparkles, X } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { NativeSelect } from '@/components/common/native-select'
import { PageContainer } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
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
import { api, type AIStatus } from '@/lib/api'
import { fieldsOf, formApiError, useFormErrors } from '@/lib/form-errors'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'

import { IntegrationsHeader } from './integrations-header'

/** What each image model can do for the media editor, in plain words. */
export const CAPABILITIES: [keyof AIStatus['capabilities'], string][] = [
  ['generate', 'Make a picture from a description'],
  ['edit', 'Change a picture by a description'],
  ['inpaint', 'Change or remove a marked area'],
  ['expand', 'Extend a picture to a new shape'],
]

const MODEL_LABELS: Record<string, string> = {
  'gpt-image-1': 'GPT Image 1 (recommended)',
  'gpt-image-1-mini': 'GPT Image 1 mini',
  'dall-e-3': 'DALL-E 3 (pictures from a description only)',
  'dall-e-2': 'DALL-E 2',
}

/**
 * Integrations › AI: the workspace's own OpenAI key for the media editor's AI tools. Everyone sees whether AI
 * works and what it can do; admins add, replace or remove the key (a short form, so a modal).
 */
export function AiSettings() {
  const qc = useQueryClient()
  const status = useQuery({ queryKey: qk.ai, queryFn: api.ai })
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const remove = useMutation({
    mutationFn: api.deleteAI,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.ai })
      toast.success('AI key removed')
    },
    onError: (e) => toastApiError(e),
  })
  const st = status.data

  return (
    <PageContainer>
      <IntegrationsHeader active="ai" />
      <p className="mt-4 text-sm text-muted-foreground">
        The media editor&apos;s AI tools use your own OpenAI account: OpenAI bills the key&apos;s owner, staqly
        doesn&apos;t. A picture goes to OpenAI only when someone clicks an AI action.
      </p>
      <Card className="mt-6 max-w-2xl" data-testid="ai-card">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Sparkles className="size-4" aria-hidden /> OpenAI
          </CardTitle>
          <CardDescription>
            {st ? (st.configured ? `Key ${st.key_hint} · ${MODEL_LABELS[st.model] ?? st.model}` : 'No key yet') : null}
          </CardDescription>
          {st?.can_manage && (
            <CardAction className="flex gap-2">
              {st.configured && (
                <Button variant="ghost" onClick={() => setRemoving(true)} disabled={remove.isPending}>
                  Remove
                </Button>
              )}
              <Button variant={st.configured ? 'outline' : 'default'} onClick={() => setEditing(true)}>
                <KeyRound aria-hidden /> {st.configured ? 'Replace key' : 'Add key'}
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="flex flex-col gap-3 text-sm">
          {status.isPending ? (
            <Skeleton className="h-24" />
          ) : st ? (
            <>
              {st.reason && (
                <Callout tone={st.configured ? 'warning' : 'info'}>
                  {st.can_manage || st.configured
                    ? st.reason
                    : 'An admin of this workspace can add an OpenAI key here.'}
                </Callout>
              )}
              <ul className="flex flex-col gap-1.5" aria-label="What the AI tools can do">
                {CAPABILITIES.map(([k, label]) => {
                  const on = st.configured && st.capabilities[k]

                  return (
                    <li key={k} className="flex items-center gap-2">
                      {on ? (
                        <Check className="size-4 text-brand" aria-hidden />
                      ) : (
                        <X className="size-4 text-muted-foreground" aria-hidden />
                      )}
                      <span className={on ? '' : 'text-muted-foreground'}>
                        <span className="sr-only">{on ? 'Available: ' : 'Not available: '}</span>
                        {label}
                      </span>
                    </li>
                  )
                })}
              </ul>
              <p className="text-xs text-muted-foreground">
                Removing backgrounds and objects, selecting and upscaling also work without a key: they run in your
                browser.
              </p>
            </>
          ) : null}
        </CardContent>
      </Card>

      {st && <AiKeyDialog open={editing} onOpenChange={setEditing} status={st} />}
      <ConfirmDialog
        open={removing}
        onOpenChange={setRemoving}
        title="Remove the OpenAI key?"
        description="The media editor's AI tools stop working for everyone in this workspace until a key is added again."
        confirmLabel="Remove"
        variant="destructive"
        onConfirm={() => remove.mutate()}
      />
    </PageContainer>
  )
}

function AiKeyDialog({
  open,
  onOpenChange,
  status,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  status: AIStatus
}) {
  const qc = useQueryClient()
  const [key, setKey] = useState('')
  const [model, setModel] = useState(status.configured ? status.model : 'gpt-image-1')
  const form = useFormErrors<'api_key' | 'model'>({
    api_key: key.trim() || status.configured ? undefined : 'Paste the key from your OpenAI account',
  })
  const save = useMutation({
    mutationFn: () => api.setAI({ model, ...(key.trim() ? { api_key: key.trim() } : {}) }),
    onSuccess: (st) => {
      qc.setQueryData(qk.ai, st)
      toast.success(status.configured ? 'AI settings saved' : 'AI key added', {
        description: 'OpenAI accepted the key.',
      })
      setKey('')
      onOpenChange(false)
    },
    onError: (e) => form.setServerErrors(formApiError(e, fieldsOf('api_key', 'model'))),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form
          onSubmit={(e) => {
            e.preventDefault()

            if (form.submit()) {
              save.mutate()
            }
          }}
          className="flex flex-col gap-4"
        >
          <DialogHeader>
            <DialogTitle>{status.configured ? 'Replace the OpenAI key' : 'Add an OpenAI key'}</DialogTitle>
            <DialogDescription>
              Staqly checks the key with OpenAI (free), stores it encrypted and shows only its last four characters.
            </DialogDescription>
          </DialogHeader>
          <Field data-invalid={!!form.errors.api_key}>
            <FieldLabel htmlFor="ai-key">API key</FieldLabel>
            <Input
              id="ai-key"
              type="password"
              autoComplete="off"
              spellCheck={false}
              placeholder={status.configured ? `Keep ${status.key_hint}` : 'sk-proj-...'}
              value={key}
              onChange={(e) => {
                setKey(e.target.value)
                form.touch('api_key')
              }}
              aria-invalid={!!form.errors.api_key}
            />
            <FieldDescription>
              Create one at platform.openai.com under API keys. GPT Image needs a verified organization.
            </FieldDescription>
            <FieldError>{form.errors.api_key}</FieldError>
          </Field>
          <Field data-invalid={!!form.errors.model}>
            <FieldLabel htmlFor="ai-model">Image model</FieldLabel>
            <NativeSelect id="ai-model" value={model} onChange={(e) => setModel(e.target.value)}>
              {status.models.map((m) => (
                <option key={m} value={m}>
                  {MODEL_LABELS[m] ?? m}
                </option>
              ))}
            </NativeSelect>
            <FieldError>{form.errors.model}</FieldError>
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={save.isPending} data-testid="ai-save">
              {save.isPending ? 'Checking...' : status.configured ? 'Save' : 'Add key'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
