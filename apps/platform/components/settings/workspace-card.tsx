'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Settings as SettingsIcon } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { IdChip } from '@/components/common/id-chip'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { fieldsOf, setFormApiError } from '@/lib/form-errors'
import { formatDateTime } from '@/lib/format'
import { qk, useSession } from '@/lib/queries'
import { zodResolver } from '@/lib/zod'

import { SettingsCard as Card } from './settings-card'

const nameSchema = z.object({ name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters') })

/** The workspace name form; loaded with next/dynamic so zod and react-hook-form stay out of /settings. */
export default function WorkspaceCard() {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const session = useSession()
  const qc = useQueryClient()
  const ws = session.data?.workspace
  const form = useForm<{ name: string }>({
    resolver: zodResolver(nameSchema),
    values: { name: ws?.name ?? '' },
  })
  const rename = useMutation({
    mutationFn: (name: string) => api.renameWorkspace(name),
    onSuccess: (w) => {
      qc.setQueryData(qk.session, (old: typeof session.data) => (old ? { ...old, workspace: w } : old))
      toast.success('Workspace renamed')
    },
    onError: (e) => setFormApiError(e, form.setError, fieldsOf('name')),
  })

  return (
    <Card title="Workspace" icon={<SettingsIcon />}>
      <form className="grid gap-3" onSubmit={form.handleSubmit((v) => rename.mutate(v.name.trim()))} noValidate>
        <Field data-invalid={!!form.formState.errors.name}>
          <FieldLabel htmlFor="ws-name" required>
            Name
          </FieldLabel>
          <div className="flex gap-2">
            <Input
              id="ws-name"
              {...form.register('name')}
              aria-invalid={!!form.formState.errors.name}
              aria-required
              className="min-w-0 sm:max-w-sm"
            />
            <Button type="submit" disabled={!form.formState.isDirty || rename.isPending}>
              Save
            </Button>
          </div>
          <FieldError errors={[form.formState.errors.name]} />
        </Field>
        {ws && (
          <div className="text-xs text-muted-foreground">
            <IdChip id={ws.id} label="workspace" /> · created {formatDateTime(ws.created_at)}
          </div>
        )}
      </form>
    </Card>
  )
}
