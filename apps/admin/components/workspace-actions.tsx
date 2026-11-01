'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { adminApi, errorText } from '@/lib/api'

const PLANS = ['starter', 'plus', 'pro', 'business']

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">{children}</CardContent>
    </Card>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function CopyLink({ value, label }: { value: string; label: string }) {
  return (
    <div className="flex gap-2">
      <Input readOnly value={value} aria-label={label} onFocus={(e) => e.currentTarget.select()} />
      <Button
        type="button"
        variant="outline"
        onClick={() =>
          navigator.clipboard.writeText(value).then(
            () => toast.success('Copied'),
            () => toast.error('Could not copy'),
          )
        }
      >
        Copy
      </Button>
    </div>
  )
}

export function WorkspaceActions({ id, name, plan }: { id: string; name: string; plan: string }) {
  const qc = useQueryClient()
  const router = useRouter()
  const base = `/v1/admin/workspaces/${encodeURIComponent(id)}`
  const [newPlan, setNewPlan] = useState(plan)
  const [until, setUntil] = useState('')
  const [attachEmail, setAttachEmail] = useState('')
  const [attachLink, setAttachLink] = useState('')
  const [linkEmail, setLinkEmail] = useState('')
  const [mode, setMode] = useState('person')
  const [role, setRole] = useState('viewer')
  const [hours, setHours] = useState('24')
  const [url, setUrl] = useState('')

  const act = useMutation({
    mutationFn: async ({ run }: { run: () => Promise<string | void> }) => run(),
    onSuccess: (msg) => {
      toast.success(msg || 'Done')
      void qc.invalidateQueries({ queryKey: ['admin-workspace', id] })
      void qc.invalidateQueries({ queryKey: ['admin-workspaces'] })
    },
    onError: (e) => toast.error(errorText(e)),
  })
  const go = (run: () => Promise<string | void>) => act.mutate({ run })
  const post = (path: string, body?: unknown) => adminApi.send<Record<string, unknown>>('POST', `${base}${path}`, body)

  const withEmail = mode !== 'device'

  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <Section title="Change plan">
        <Field label="Plan">
          <Select value={newPlan} onValueChange={setNewPlan}>
            <SelectTrigger aria-label="Plan" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PLANS.map((p) => (
                <SelectItem key={p} value={p}>
                  {p}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <div className="space-y-1.5">
          <Label htmlFor="plan-until">Until (optional)</Label>
          <Input id="plan-until" type="date" value={until} onChange={(e) => setUntil(e.target.value)} />
        </div>
        <Button
          disabled={act.isPending}
          onClick={() =>
            go(async () => {
              await post('/plan', { plan: newPlan, ...(until ? { until: new Date(until).toISOString() } : {}) })

              return `Plan set to ${newPlan}`
            })
          }
        >
          Change plan
        </Button>
        <Button
          variant="outline"
          disabled={act.isPending}
          onClick={() =>
            go(async () => {
              await post('/trial-review/clear')

              return 'Trial review cleared'
            })
          }
        >
          Clear trial review
        </Button>
      </Section>

      <Section title="Attach to email">
        <div className="space-y-1.5">
          <Label htmlFor="attach-email">Email</Label>
          <Input id="attach-email" type="email" value={attachEmail} onChange={(e) => setAttachEmail(e.target.value)} />
        </div>
        <Button
          disabled={act.isPending || !attachEmail}
          onClick={() =>
            go(async () => {
              const r = await post('/attach', { email: attachEmail })

              setAttachLink(String(r.link ?? ''))

              return r.emailed ? 'Link emailed' : 'Link created'
            })
          }
        >
          Attach
        </Button>
        {attachLink && <CopyLink value={attachLink} label="Attach link" />}
      </Section>

      <Section title="Sign-in link">
        <Field label="Mode">
          <Select value={mode} onValueChange={setMode}>
            <SelectTrigger aria-label="Mode" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="person">One-time, as this person</SelectItem>
              <SelectItem value="guest">Guest link</SelectItem>
              <SelectItem value="device">Device link (no email)</SelectItem>
            </SelectContent>
          </Select>
        </Field>
        {withEmail && (
          <div className="space-y-1.5">
            <Label htmlFor="link-email">Email</Label>
            <Input id="link-email" type="email" value={linkEmail} onChange={(e) => setLinkEmail(e.target.value)} />
          </div>
        )}
        {mode === 'guest' && (
          <>
            <Field label="Role">
              <Select value={role} onValueChange={setRole}>
                <SelectTrigger aria-label="Role" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="editor">editor</SelectItem>
                  <SelectItem value="viewer">viewer</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <div className="space-y-1.5">
              <Label htmlFor="link-hours">Valid for (hours)</Label>
              <Input
                id="link-hours"
                type="number"
                min={1}
                max={720}
                value={hours}
                onChange={(e) => setHours(e.target.value)}
              />
            </div>
          </>
        )}
        <Button
          disabled={act.isPending || (withEmail && !linkEmail)}
          onClick={() =>
            go(async () => {
              const body =
                mode === 'device'
                  ? {}
                  : mode === 'person'
                    ? { email: linkEmail }
                    : { email: linkEmail, role, hours: Number(hours) || 24 }
              const r = await post('/sign-in-link', body)

              setUrl(String(r.url ?? ''))

              return 'Link created'
            })
          }
        >
          Create link
        </Button>
        {url && <CopyLink value={url} label="Sign-in link" />}
      </Section>

      <Section title="Sessions and billing">
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={act.isPending}
            onClick={() =>
              go(async () => {
                const r = await post('/sessions/revoke')

                return `Revoked ${String(r.revoked ?? 0)} sessions`
              })
            }
          >
            Revoke all sessions
          </Button>
          <Button
            variant="outline"
            disabled={act.isPending}
            onClick={() =>
              go(async () => {
                await post('/billing/sync')

                return 'Billing synced'
              })
            }
          >
            Sync billing
          </Button>
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button variant="outline">Cancel subscription</Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Cancel the subscription?</AlertDialogTitle>
                <AlertDialogDescription>The Stripe subscription of {name} is cancelled now.</AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Keep it</AlertDialogCancel>
                <AlertDialogAction
                  variant="destructive"
                  onClick={() =>
                    go(async () => {
                      await post('/billing/cancel')

                      return 'Subscription cancelled'
                    })
                  }
                >
                  Cancel subscription
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      </Section>

      <Section title="Delete workspace">
        <p className="text-sm text-muted-foreground">
          Cancels the Stripe subscription and removes the workspace, its data and media. This cannot be undone.
        </p>
        <ConfirmDialog
          title="Delete workspace?"
          description={`${name} and all its data are removed for good.`}
          expected={name}
          actionLabel="Delete workspace"
          trigger={<Button variant="destructive">Delete workspace</Button>}
          onConfirm={() =>
            act.mutate(
              {
                run: async () => {
                  await adminApi.send('DELETE', base, { confirm: name })

                  return 'Workspace deleted'
                },
              },
              { onSuccess: () => router.push('/workspaces') },
            )
          }
        />
      </Section>
    </div>
  )
}
