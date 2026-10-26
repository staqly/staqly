'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Sparkles } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { NativeSelect } from '@/components/common/native-select'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'

/**
 * AI pictures in the design editor: they run on the workspace's own OpenAI key, so adding a key turns
 * them on and removing it turns them off. Admins manage the key; everyone else sees whether AI is on.
 */
export function AiKeyCard() {
  const qc = useQueryClient()
  const status = useQuery({ queryKey: qk.ai, queryFn: api.ai })
  const [key, setKey] = useState('')
  const [model, setModel] = useState('')
  const [changing, setChanging] = useState(false)
  const [removing, setRemoving] = useState(false)
  const st = status.data
  const save = useMutation({
    mutationFn: (body: { api_key?: string; model?: string }) => api.setAI(body),
    onSuccess: (s) => {
      qc.setQueryData(qk.ai, s)
      setKey('')
      setChanging(false)
      toast.success('AI is on', { description: `The editors make pictures with ${s.model}.` })
    },
    onError: (e) => toastApiError(e, 'Could not save the key'),
  })
  const remove = useMutation({
    mutationFn: api.deleteAI,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.ai })
      toast.success('AI is off', { description: 'The key is forgotten.' })
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <section id="ai" className="grid scroll-mt-20 gap-3 rounded-xl border bg-card p-4" data-testid="ai-key-card">
      <div className="flex flex-wrap items-start gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-brand-soft text-brand">
          <Sparkles className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">
            AI pictures{' '}
            {st && (
              <span className={st.configured ? 'text-brand' : 'text-muted-foreground'}>
                {st.configured ? '· On' : '· Off'}
              </span>
            )}
          </h2>
          <p className="text-sm text-muted-foreground">
            Make a picture from a description, or change a photo by telling what to do, in the design editor. It runs on
            your own OpenAI key and OpenAI bills you for it.
          </p>
        </div>
        {st?.configured && st.can_manage && !changing && (
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => setChanging(true)}>
              Change key
            </Button>
            <Button size="sm" variant="destructive" onClick={() => setRemoving(true)} disabled={remove.isPending}>
              Turn off
            </Button>
          </div>
        )}
      </div>

      {status.isPending && <Skeleton className="h-8 w-full" />}

      {st && !st.can_manage && !st.configured && (
        <p className="text-sm text-muted-foreground">An admin of this workspace can add a key.</p>
      )}

      {st?.configured && !changing && (
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <span className="text-muted-foreground">Key {st.key_hint}</span>
          <label className="flex items-center gap-2">
            <span className="text-muted-foreground">Model</span>
            <NativeSelect
              aria-label="Model"
              className="w-44"
              value={st.model}
              disabled={!st.can_manage || save.isPending}
              onChange={(e) => save.mutate({ model: e.target.value })}
            >
              {st.models.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </NativeSelect>
          </label>
          {st.reason && <span className="text-amber-700 dark:text-amber-400">{st.reason}</span>}
        </div>
      )}

      {st?.can_manage && (!st.configured || changing) && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate({ api_key: key.trim(), model: model || undefined })
          }}
        >
          <Input
            type="password"
            autoComplete="off"
            aria-label="OpenAI API key"
            placeholder="sk-..."
            value={key}
            onChange={(e) => setKey(e.target.value)}
            className="min-w-0 flex-1 basis-64"
          />
          <NativeSelect
            aria-label="Model"
            className="w-44"
            value={model || st.model}
            onChange={(e) => setModel(e.target.value)}
          >
            {st.models.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </NativeSelect>
          <Button type="submit" disabled={!key.trim() || save.isPending}>
            {save.isPending ? 'Checking...' : 'Save key'}
          </Button>
          {changing && (
            <Button type="button" variant="ghost" onClick={() => setChanging(false)}>
              Cancel
            </Button>
          )}
          <p className="w-full text-xs text-muted-foreground">
            staqly checks the key with OpenAI (free), then stores it encrypted and never shows it again.
          </p>
        </form>
      )}

      <ConfirmDialog
        open={removing}
        onOpenChange={setRemoving}
        title="Turn off AI pictures?"
        description="The key is forgotten and the AI tools leave the editors. Pictures made so far stay in Media."
        confirmLabel="Turn off"
        variant="destructive"
        onConfirm={() => remove.mutate()}
      />
    </section>
  )
}
