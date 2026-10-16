'use client'

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Laptop,
  LifeBuoy,
  LogOut,
  Monitor,
  MonitorSmartphone,
  QrCode,
  Settings as SettingsIcon,
  Smartphone,
  Tablet,
  TerminalSquare,
  Timer,
} from 'lucide-react'
import { useTheme } from 'next-themes'
import { useState, useSyncExternalStore } from 'react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'

import { Callout } from '@/components/common/callout'
import { AckCheckbox, useAckGate } from '@/components/common/confirm-name-field'
import { ErrorState } from '@/components/common/error-state'
import { IdChip } from '@/components/common/id-chip'
import { PageContainer } from '@/components/common/page-header'
import { SecretField } from '@/components/common/secret-field'
import { SettingsHeader } from '@/components/settings/settings-header'
import { useLogout, useShell } from '@/components/shell/app-shell'
import { THEMES } from '@/components/shell/theme-toggle'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/lib/api'
import { fieldsOf, setFormApiError } from '@/lib/form-errors'
import { downloadText, formatDateTime, formatInterval, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useMode, useSession } from '@/lib/queries'

import { SettingsCard as Card } from './settings-card'
import { SettingsNav } from './settings-nav'

const nameSchema = z.object({ name: z.string().trim().min(1, 'Name is required').max(100, 'At most 100 characters') })

function WorkspaceCard() {
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

const TIMEOUTS = [900, 1800, 3600, 7200, 14400, 28800, 43200, 86400]

/** How long an item may stay "processing" before it fails (1 minute to 24 hours via the API). */
function ProcessingTimeoutCard() {
  const session = useSession()
  const qc = useQueryClient()
  const current = session.data?.workspace.processing_timeout_seconds ?? 3600
  const save = useMutation({
    mutationFn: (seconds: number) => api.setProcessingTimeout(seconds),
    onSuccess: (w) => {
      qc.setQueryData(qk.session, (old: typeof session.data) => (old ? { ...old, workspace: w } : old))
      toast.success(`Processing timeout set to ${formatInterval(w.processing_timeout_seconds)}`)
    },
    onError: (e) => toastApiError(e),
  })
  const options = TIMEOUTS.includes(current) ? TIMEOUTS : [...TIMEOUTS, current].toSorted((a, b) => a - b)

  return (
    <Card title="Processing timeout" icon={<Timer />}>
      <Field>
        <FieldLabel htmlFor="processing-timeout">Fail items still processing after</FieldLabel>
        <Select value={String(current)} onValueChange={(v) => save.mutate(Number(v))} disabled={save.isPending}>
          <SelectTrigger id="processing-timeout" className="w-full sm:max-w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {options.map((s) => (
              <SelectItem key={s} value={String(s)}>
                {formatInterval(s)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          After an action like Publish or Regenerate, your automation has this long to report the result. Then the item
          fails with &ldquo;No result reported&rdquo;, so nothing waits forever.
        </p>
      </Field>
    </Card>
  )
}

const emptySubscribe = () => () => {}

function ThemeCard() {
  const { theme, setTheme } = useTheme()
  // next-themes only knows the theme on the client.
  const mounted = useSyncExternalStore(
    emptySubscribe,
    () => true,
    () => false,
  )

  return (
    <Card title="Appearance" icon={<Monitor />}>
      <ToggleGroup
        type="single"
        variant="outline"
        value={mounted ? theme : undefined}
        onValueChange={(v) => v && setTheme(v)}
        aria-label="Theme"
        className="w-full sm:w-auto"
      >
        {THEMES.map(({ value, label, Icon }) => (
          <ToggleGroupItem key={value} value={value} className="flex-1 px-3 sm:flex-none">
            <Icon /> {label}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
    </Card>
  )
}

function deviceIcon(ua: string) {
  if (/iPad|Tablet/i.test(ua)) {
    return Tablet
  }

  if (/iPhone|Android.*Mobile|Mobile/i.test(ua)) {
    return Smartphone
  }

  if (/curl|python|node|Go-http/i.test(ua)) {
    return TerminalSquare
  }

  return Laptop
}

function DevicesCard() {
  const qc = useQueryClient()
  const { openConnectDevice } = useShell()
  const { isLocal } = useMode()
  const sessions = useQuery({ queryKey: qk.sessions, queryFn: async () => (await api.sessions()).data ?? [] })
  const [revoking, setRevoking] = useState<string | null>(null)
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeSession(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.sessions })
      toast.success('Device signed out')
    },
    onError: (e) => toastApiError(e),
  })
  const list = sessions.data ?? []

  return (
    <Card
      title="Devices"
      description={
        isLocal
          ? 'Every browser signed in to this install — this computer included. Sign in more with a QR code or staqly serve connect-link.'
          : 'Browsers and phones signed in to this workspace.'
      }
      icon={<MonitorSmartphone />}
      action={
        <Button size="sm" variant="outline" onClick={openConnectDevice} className="pointer-coarse:h-10">
          <QrCode /> Connect a device
        </Button>
      }
    >
      {sessions.isLoading ? (
        <Skeleton className="h-20" />
      ) : sessions.error ? (
        <ErrorState error={sessions.error} onRetry={() => sessions.refetch()} />
      ) : list.length === 0 ? (
        <div
          className="flex flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground"
          data-testid="devices-empty"
        >
          <Smartphone className="size-5" />
          No signed-in browsers yet. Connect your phone to review on the go.
        </div>
      ) : (
        <ul className="divide-y" data-testid="sessions-list">
          {list.map((s) => {
            const Icon = deviceIcon(s.user_agent)

            return (
              <li key={s.id} className="flex items-center gap-3 py-3 text-sm">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                  <Icon className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1" title={s.user_agent}>
                    <span className="font-medium">{describeAgent(s.user_agent)}</span>
                    {s.current && (
                      <span className="rounded bg-brand-soft px-1.5 py-0.5 text-[11px] font-medium text-brand">
                        This device
                      </span>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    Last seen {timeAgo(s.last_seen_at)}
                    <span className="hidden sm:inline">
                      {' '}
                      · signed in {formatDateTime(s.created_at)} · expires {formatDateTime(s.expires_at)}
                    </span>
                  </div>
                </div>
                {!s.current && (
                  <Button
                    size="sm"
                    variant="outline"
                    className="pointer-coarse:h-10"
                    onClick={() => setRevoking(s.id)}
                    aria-label={`Revoke ${describeAgent(s.user_agent)}`}
                  >
                    Revoke
                  </Button>
                )}
              </li>
            )
          })}
        </ul>
      )}
      <AlertDialog open={!!revoking} onOpenChange={(o) => !o && setRevoking(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Sign out this device?</AlertDialogTitle>
            <AlertDialogDescription>It loses access immediately and has to be connected again.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => revoking && revoke.mutate(revoking)}>
              Revoke access
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

function describeAgent(ua: string) {
  if (!ua) {
    return 'Unknown browser'
  }

  const browser = /Edg(e|A|iOS)?\//.test(ua)
    ? 'Edge'
    : /Firefox\/|FxiOS/.test(ua)
      ? 'Firefox'
      : /Chrome\/|CriOS/.test(ua)
        ? 'Chrome'
        : /Safari\//.test(ua)
          ? 'Safari'
          : /curl/i.test(ua)
            ? 'curl'
            : ua.split(' ')[0]
  const os = /iPhone/.test(ua)
    ? 'iPhone'
    : /iPad/.test(ua)
      ? 'iPad'
      : /Android/.test(ua)
        ? 'Android'
        : /Mac OS X/.test(ua)
          ? 'macOS'
          : /Windows/.test(ua)
            ? 'Windows'
            : /Linux/.test(ua)
              ? 'Linux'
              : ''

  return os ? `${browser} on ${os}` : browser
}

function RecoveryCard() {
  const session = useSession()
  const [confirm, setConfirm] = useState(false)
  const [newKey, setNewKey] = useState<string | null>(null)
  const ack = useAckGate('ack-new-recovery', 'Confirm that you saved the new recovery key')
  const rotate = useMutation({
    mutationFn: () => api.rotateRecoveryKey(),
    onSuccess: (r) => {
      setNewKey(r.recovery_key ?? null)
      ack.reset()
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Card
      title="Recovery key"
      icon={<LifeBuoy />}
      description="The only way to regain admin access if every browser session is lost."
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="text-sm text-muted-foreground">
          Last rotated {formatDateTime(session.data?.workspace.recovery_rotated_at)}. Rotating invalidates the old key.
        </div>
        <Button variant="outline" onClick={() => setConfirm(true)} disabled={rotate.isPending}>
          Rotate recovery key
        </Button>
      </div>
      <AlertDialog open={confirm} onOpenChange={setConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Rotate the recovery key?</AlertDialogTitle>
            <AlertDialogDescription>
              Your current recovery key stops working immediately. The new key is shown once — be ready to store it.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => rotate.mutate()}>Rotate key</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <Dialog open={!!newKey} onOpenChange={(o) => !o && ack.checked && setNewKey(null)}>
        <DialogContent className="sm:max-w-lg" showCloseButton={false} onInteractOutside={(e) => e.preventDefault()}>
          <DialogHeader>
            <DialogTitle>Your new recovery key</DialogTitle>
            <DialogDescription>Shown only once. Store it somewhere safe and offline.</DialogDescription>
          </DialogHeader>
          {newKey && (
            <div className="grid gap-3">
              <SecretField value={newKey} label="Recovery key" />
              <Callout size="xs">If you lose this key and all sessions, you may be permanently locked out.</Callout>
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  downloadText(
                    'staqly-recovery-key.txt',
                    `staqly recovery key (${new Date().toISOString()}):\n${newKey}\n`,
                  )
                }
              >
                Download as .txt
              </Button>
              <AckCheckbox {...ack.checkbox}>I have saved my recovery key</AckCheckbox>
            </div>
          )}
          <DialogFooter>
            <Button onClick={() => ack.confirm(() => setNewKey(null))}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

export function SettingsPage() {
  const logout = useLogout()
  const { isCloud } = useMode()

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        <WorkspaceCard />
        <ProcessingTimeoutCard />
        <ThemeCard />
        <DevicesCard />
        {isCloud && <RecoveryCard />}
        {isCloud && (
          <Card title="Sign out" icon={<LogOut />} description="End the session in this browser.">
            <Button variant="outline" onClick={() => logout.mutate()} disabled={logout.isPending}>
              <LogOut /> Log out
            </Button>
          </Card>
        )}
      </div>
    </PageContainer>
  )
}
