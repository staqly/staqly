'use client'

import { LogoMark } from '@staqly/brand/logo'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Settings as SettingsIcon } from 'lucide-react'
import { Controller, useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { IdChip } from '@/components/common/id-chip'
import { ImagePicker } from '@/components/common/image-picker'
import { TimeZoneSelect } from '@/components/common/time-zone-select'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { api, type Workspace } from '@/lib/api'
import { fieldsOf, setFormApiError } from '@/lib/form-errors'
import { formatDateTime } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
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
  // The workspace menu shows the name and the logo too.
  const put = (w: Workspace) =>
    qc.setQueryData(qk.session, (old: typeof session.data) =>
      old
        ? {
            ...old,
            workspace: w,
            workspaces: old.workspaces?.map((x) => (x.id === w.id ? { ...x, name: w.name } : x)),
          }
        : old,
    )
  const save = useMutation({
    mutationFn: (v: { name: string; timezone: string }) => api.updateWorkspace(v),
    onSuccess: (w) => {
      put(w)
      toast.success('Workspace saved')
    },
    onError: (e) => setFormApiError(e, form.setError, fieldsOf('name', 'timezone')),
  })
  // The logo saves on its own, as soon as it is picked or removed.
  const logo = useMutation({
    mutationFn: (dataUrl: string) => api.updateWorkspace({ logo: dataUrl }),
    onSuccess: (w) => {
      put(w)
      toast.success(w.logo ? 'Logo saved' : 'Logo removed')
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Card title="Workspace" icon={<SettingsIcon />}>
      <form
        className="grid gap-4"
        onSubmit={form.handleSubmit((v) => save.mutate({ name: v.name.trim(), timezone: v.timezone }))}
        noValidate
      >
        {ws && (
          <div className="text-xs text-muted-foreground">
            <IdChip id={ws.id} label="workspace" /> created {formatDateTime(ws.created_at)}
          </div>
        )}
        <div role="group" aria-labelledby="ws-logo-label" className="mb-2 grid gap-2">
          <span id="ws-logo-label" className="text-sm font-medium">
            Logo
          </span>
          <ImagePicker
            label="logo"
            value={ws?.logo ?? ''}
            fallback={<LogoMark className="size-8" />}
            pending={logo.isPending}
            onChange={(v) => logo.mutate(v)}
            hint="Shown in the workspace menu. A square image works best; without one, staqly's logo."
          />
        </div>
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
      </form>
    </Card>
  )
}
