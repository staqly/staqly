'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MailPlus, Users } from 'lucide-react'
import Link from 'next/link'
import { useSearchParams } from 'next/navigation'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { CopyButton } from '@/components/common/copy-button'
import { ErrorState } from '@/components/common/error-state'
import { NativeSelect } from '@/components/common/native-select'
import { PageContainer } from '@/components/common/page-header'
import { money } from '@/components/plans/plan-cards'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
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
import { api, ApiError, errorMessage, type Invitation, type Member, type Role } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { LIVE_SUBSCRIPTION } from '@/lib/plans'
import { qk, useBilling, useBillingDetails, useMode, useRole, useUsage } from '@/lib/queries'
import { ROLE_LABELS } from '@/lib/roles'

import { SettingsCard as Card } from './settings-card'
import { SettingsNav } from './settings-nav'
import { UserAvatar } from './user-avatar'

const ROLE_HELP: Record<Role, string> = {
  viewer: 'Sees posts, channels and activity. Changes nothing.',
  editor: 'Writes, schedules and publishes posts. No channels, keys or people.',
  admin: 'Everything in this workspace, including channels, API keys and people.',
  owner: 'Like an admin, and can also make or remove owners.',
}

/** Roles this person may hand out: owners any, admins up to admin. */
function assignable(myRole: string): Role[] {
  return myRole === 'owner' ? ['viewer', 'editor', 'admin', 'owner'] : ['viewer', 'editor', 'admin']
}

export function MembersPage() {
  const { canManagePeople, accountsEnabled } = useRole()

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        {!accountsEnabled ? (
          <p className="text-sm text-muted-foreground">Members and roles are part of the cloud version.</p>
        ) : !canManagePeople ? (
          <p className="text-sm text-muted-foreground">Only admins and owners see and manage the people here.</p>
        ) : (
          <>
            <MembersCard />
            <InvitationsCard />
          </>
        )}
      </div>
    </PageContainer>
  )
}

/**
 * Per user pricing in the team screens: how many seats are billed (every member), and what one more person
 * costs while the workspace pays ("Each person you add is billed €29/month on Plus (prorated).").
 */
function useSeats() {
  const { isLocal } = useMode()
  const billing = useBilling({ enabled: !isLocal })
  const on = !isLocal && !!billing.data?.enabled
  // Charged now (not during the Starter trial, when seats are free until it ends).
  const paying = on && ['active', 'past_due'].includes(billing.data?.subscription_status ?? '')
  const details = useBillingDetails({ enabled: paying })
  const usage = useUsage()
  const sub = details.data?.subscription
  const plan = usage.data?.plan
  const each =
    sub && sub.amount > 0
      ? `${money(sub.amount / 100, sub.currency)}/${sub.interval || 'month'}`
      : plan
        ? `${money(plan.price_month, plan.currency)}/month`
        : ''

  return {
    count: on && LIVE_SUBSCRIPTION.includes(billing.data?.subscription_status ?? '') ? billing.data?.seats : undefined,
    note: paying && each ? `Each person you add is billed ${each} on ${plan?.name ?? 'your plan'} (prorated).` : '',
  }
}

function MembersCard() {
  const qc = useQueryClient()
  const { role: myRole } = useRole()
  const members = useQuery({ queryKey: qk.members, queryFn: api.members })
  const seats = useSeats()
  const [removing, setRemoving] = useState<Member | null>(null)
  const done = () => {
    void qc.invalidateQueries({ queryKey: qk.members })
    void qc.invalidateQueries({ queryKey: qk.session })
  }
  const update = useMutation({
    mutationFn: (v: { m: Member; role: Role }) => api.updateMember(v.m.user_id, v.role),
    onSuccess: (m) => {
      toast.success(`${m.name} is now ${ROLE_LABELS[m.role].toLowerCase()}`)
      done()
    },
    onError: (e) => toastApiError(e),
  })
  const remove = useMutation({
    mutationFn: (m: Member) => api.removeMember(m.user_id),
    onSuccess: (_, m) => {
      toast.success(m.you ? 'You left the workspace' : `${m.name} was removed`)

      if (m.you) {
        window.location.assign('/schedule')
      }

      done()
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Card
      title="Members"
      icon={<Users />}
      description={`People who can open this workspace, and what they may do.${
        seats.count ? ` ${seats.count === 1 ? '1 seat' : `${seats.count} seats`} billed, one per member.` : ''
      }`}
    >
      {members.isPending ? (
        <Skeleton className="h-24" />
      ) : members.isError ? (
        <ErrorState error={members.error} onRetry={() => void members.refetch()} />
      ) : (
        <ul className="grid divide-y" data-testid="members">
          {members.data.data.map((m, _, all) => {
            // Admins never touch owners. The only owner keeps the role and stays (the server refuses too): with
            // others here they make someone else an owner first, alone they delete the workspace instead.
            const soleOwner = m.you && m.role === 'owner' && all.filter((x) => x.role === 'owner').length === 1
            const locked = (m.role === 'owner' && myRole !== 'owner') || soleOwner
            const roles = assignable(myRole)

            return (
              <li key={m.user_id} className="flex flex-wrap items-center gap-3 py-3">
                <UserAvatar name={m.name} src={m.avatar_url} />
                <div className="min-w-0 flex-1 basis-40">
                  <p className="truncate text-sm font-medium">
                    {m.name}
                    {m.you && <span className="text-muted-foreground"> (you)</span>}
                  </p>
                  {/* An email-code sign-in has the address as the name: show it once. */}
                  {m.email !== m.name && <p className="truncate text-xs text-muted-foreground">{m.email}</p>}
                </div>
                {locked ? (
                  <span className="text-sm text-muted-foreground">{ROLE_LABELS[m.role]}</span>
                ) : (
                  <NativeSelect
                    aria-label={`Role of ${m.name}`}
                    className="w-32"
                    value={m.role}
                    disabled={update.isPending}
                    onChange={(e) => update.mutate({ m, role: e.target.value as Role })}
                  >
                    {roles.map((r) => (
                      <option key={r} value={r}>
                        {ROLE_LABELS[r]}
                      </option>
                    ))}
                  </NativeSelect>
                )}
                {soleOwner ? (
                  all.length === 1 ? (
                    <Button variant="destructive" size="sm" asChild>
                      <Link href="/settings/data">Delete workspace</Link>
                    </Button>
                  ) : (
                    <span className="text-xs text-muted-foreground" data-testid="sole-owner-note">
                      Make someone else an owner to leave
                    </span>
                  )
                ) : (
                  (m.you || !locked) && (
                    <Button variant="ghost" size="sm" onClick={() => setRemoving(m)}>
                      {m.you ? 'Leave' : 'Remove'}
                    </Button>
                  )
                )}
              </li>
            )
          })}
        </ul>
      )}
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={removing?.you ? 'Leave this workspace?' : `Remove ${removing?.name}?`}
        description={
          removing?.you
            ? 'You lose access at once. An admin can invite you again.'
            : 'They lose access at once and are signed out of this workspace. Their API keys stop working.'
        }
        confirmLabel={removing?.you ? 'Leave' : 'Remove'}
        variant="destructive"
        onConfirm={() => removing && remove.mutate(removing)}
      />
    </Card>
  )
}

function InvitationsCard() {
  const qc = useQueryClient()
  const invitations = useQuery({ queryKey: qk.invitations, queryFn: api.invitations })
  // ?invite=1 (the getting-started checklist) opens the invitation form at once.
  const [open, setOpen] = useState(useSearchParams().get('invite') === '1')
  const [sent, setSent] = useState<Invitation | null>(null)
  const [revoking, setRevoking] = useState<Invitation | null>(null)
  const refresh = () => void qc.invalidateQueries({ queryKey: qk.invitations })
  const resend = useMutation({
    mutationFn: (inv: Invitation) => api.resendInvitation(inv.id),
    onSuccess: (inv) => {
      setSent(inv)
      setOpen(true)
      refresh()
    },
    onError: (e) => toastApiError(e),
  })
  const revoke = useMutation({
    mutationFn: (inv: Invitation) => api.revokeInvitation(inv.id),
    onSuccess: (_, inv) => {
      toast.success(`Invitation for ${inv.email} revoked`)
      refresh()
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Card
      title="Invitations"
      icon={<MailPlus />}
      description="Each link works once, for the address it was sent to, for 7 days."
      action={
        <Button
          size="sm"

          onClick={() => {
            setSent(null)
            setOpen(true)
          }}
        >
          Invite people
        </Button>
      }
    >
      {invitations.isPending ? (
        <Skeleton className="h-12" />
      ) : invitations.isError ? (
        <ErrorState error={invitations.error} onRetry={() => void invitations.refetch()} />
      ) : invitations.data.data.length === 0 ? (
        <p className="text-sm text-muted-foreground">No open invitations.</p>
      ) : (
        <ul className="grid divide-y" data-testid="invitations">
          {invitations.data.data.map((inv) => (
            <li key={inv.id} className="flex flex-wrap items-center gap-3 py-3">
              <div className="min-w-0 flex-1 basis-48">
                <p className="truncate text-sm font-medium">{inv.email}</p>
                <p className="text-xs text-muted-foreground">
                  {ROLE_LABELS[inv.role]} · {inv.expired ? 'expired' : `expires ${formatDateTime(inv.expires_at)}`}
                </p>
              </div>
              <Button
                variant="outline"
                size="sm"

                disabled={resend.isPending}
                onClick={() => resend.mutate(inv)}
              >
                Resend
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setRevoking(inv)}>
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
      <InviteSheet
        open={open}
        onOpenChange={setOpen}
        sent={sent}
        onSent={(inv) => {
          setSent(inv)
          refresh()
        }}
      />
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={`Revoke the invitation for ${revoking?.email}?`}
        description="The link stops working. You can invite them again later."
        confirmLabel="Revoke"
        variant="destructive"
        onConfirm={() => revoking && revoke.mutate(revoking)}
      />
    </Card>
  )
}

function InviteSheet({
  open,
  onOpenChange,
  sent,
  onSent,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  sent: Invitation | null
  onSent: (inv: Invitation) => void
}) {
  const { role: myRole } = useRole()
  const seats = useSeats()
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<Role>('editor')
  const [error, setError] = useState('')
  const invite = useMutation({
    mutationFn: () => api.invite(email.trim(), role),
    onSuccess: (inv) => {
      setEmail('')
      setError('')
      onSent(inv)
    },
    onError: (e) =>
      setError(
        e instanceof ApiError && e.status === 422 ? 'Enter an email address such as ana@example.com.' : errorMessage(e),
      ),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{sent ? 'Invitation ready' : 'Invite people'}</DialogTitle>
          <DialogDescription>
            {sent
              ? `${sent.email} joins as ${ROLE_LABELS[sent.role].toLowerCase()} after signing in as that address.`
              : 'They sign in as this address and join with the role you pick.'}
          </DialogDescription>
        </DialogHeader>
        {sent ? (
          <>
            <div className="grid gap-3">
              <p className="text-sm">
                {sent.emailed
                  ? 'We emailed the link. You can also send it yourself:'
                  : 'Send this link yourself. It is shown only now:'}
              </p>
              <div className="flex items-center gap-2">
                <Input readOnly value={sent.link ?? ''} aria-label="Invitation link" className="font-mono text-xs" />
                <CopyButton value={sent.link ?? ''} label="Copy link" variant="outline" size="sm" showLabel />
              </div>
            </div>
            <DialogFooter>
              <Button onClick={() => onOpenChange(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form
            noValidate
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault()

              if (!email.trim()) {
                setError('Enter an email address such as ana@example.com.')

                return
              }

              invite.mutate()
            }}
          >
            <div className="grid gap-5">
              <Field data-invalid={!!error}>
                <FieldLabel htmlFor="invite-email" required>
                  Email
                </FieldLabel>
                <Input
                  id="invite-email"
                  type="email"
                  autoComplete="off"
                  placeholder="ana@example.com"
                  value={email}
                  aria-invalid={!!error}
                  onChange={(e) => setEmail(e.target.value)}
                />
                <FieldError>{error}</FieldError>
              </Field>
              <Field>
                <FieldLabel htmlFor="invite-role">Role</FieldLabel>
                <NativeSelect id="invite-role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
                  {assignable(myRole).map((r) => (
                    <option key={r} value={r}>
                      {ROLE_LABELS[r]}
                    </option>
                  ))}
                </NativeSelect>
                <FieldDescription>{ROLE_HELP[role]}</FieldDescription>
              </Field>
              {seats.note && (
                <p className="text-sm text-muted-foreground" data-testid="invite-seat-price">
                  {seats.note}
                </p>
              )}
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={invite.isPending}>
                {invite.isPending ? 'Sending...' : 'Send invitation'}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
