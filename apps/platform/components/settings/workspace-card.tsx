'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Settings as SettingsIcon } from 'lucide-react'
import { Controller, useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { IdChip } from '@/components/common/id-chip'
import { TimeZoneSelect } from '@/components/common/time-zone-select'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { fieldsOf, setFormApiError } from '@/lib/form-errors'
import { formatDateTime } from '@/lib/format'
import { qk, useSession } from '@/lib/queries'
import { zodResolver } from '@/lib/zod'

import { SettingsCard as Card } from './settings-card'

const schema = z.object({
  name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters'),
  timezone: z.string().min(1, 'Choose a time zone'),
})

/** The workspace name and time zone form; loaded with next/dynamic so zod and react-hook-form stay out of /settings. */
export default function WorkspaceCard() {
  'use no memo' // React Hook Form register()/reset() must not be memoised by the React Compiler

  const session = useSession()
  const qc = useQueryClient()
  const ws = session.data?.workspace
  const form = useForm<{ name: string; timezone: string }>({
    resolver: zodResolver(schema),
    values: { name: ws?.name ?? '', timezone: ws?.timezone ?? 'UTC' },
  })
  const save = useMutation({
    mutationFn: (v: { name: string; timezone: string }) => api.updateWorkspace(v),
    onSuccess: (w) => {
      qc.setQueryData(qk.session, (old: typeof session.data) => (old ? { ...old, workspace: w } : old))
      toast.success('Workspace saved')
    },
    onError: (e) => setFormApiError(e, form.setError, fieldsOf('name', 'timezone')),
  })

  return (
    <Card title="Workspace" icon={<SettingsIcon />}>
      <form
        className="grid gap-3"
        onSubmit={form.handleSubmit((v) => save.mutate({ name: v.name.trim(), timezone: v.timezone }))}
        noValidate
      >
        <Field data-invalid={!!form.formState.errors.name}>
          <FieldLabel htmlFor="ws-name" required>
            Name
          </FieldLabel>
          <Input
            id="ws-name"
            {...form.register('name')}
            aria-invalid={!!form.formState.errors.name}
            aria-required
            className="min-w-0 sm:max-w-sm"
          />
          <FieldError errors={[form.formState.errors.name]} />
        </Field>
        <Field data-invalid={!!form.formState.errors.timezone}>
          <FieldLabel htmlFor="ws-timezone">Time zone</FieldLabel>
          <Controller
            control={form.control}
            name="timezone"
            render={({ field }) => (
              <TimeZoneSelect
                id="ws-timezone"
                value={field.value}
                onChange={(v) => field.onChange(v)}
                invalid={!!form.formState.errors.timezone}
                className="w-full sm:max-w-sm"
              />
            )}
          />
          <FieldDescription>
            Repeating posts keep their time of day in this zone, and new schedules start in it.
          </FieldDescription>
          <FieldError errors={[form.formState.errors.timezone]} />
        </Field>
        <div>
          <Button type="submit" disabled={!form.formState.isDirty || save.isPending}>
            {save.isPending ? 'Saving...' : 'Save'}
          </Button>
        </div>
        {ws && (
          <div className="text-xs text-muted-foreground">
            <IdChip id={ws.id} label="workspace" /> · created {formatDateTime(ws.created_at)}
          </div>
        )}
      </form>
    </Card>
  )
}
