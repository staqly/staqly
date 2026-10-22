'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, UserRound } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { PageContainer } from '@/components/common/page-header'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type MyWorkspace } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { qk, useRole, useSession, useSwitchWorkspace } from '@/lib/queries'
import { ROLE_LABELS } from '@/lib/roles'

import { NewWorkspaceDialog } from './new-workspace-dialog'
import { NotificationPrefsCard } from './notification-prefs'
import { SettingsCard as Card } from './settings-card'
import { SettingsNav } from './settings-nav'
import { SignInMethodsCard } from './sign-in-methods'

export function AccountPage() {
  const { accountsEnabled } = useRole()
  const session = useSession()
  const person = accountsEnabled && !!session.data?.user?.id

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        {person && <ProfileCard />}
        <SignInMethodsCard />
        {accountsEnabled && <NotificationPrefsCard />}
        {person && <WorkspacesCard />}
      </div>
    </PageContainer>
  )
}

/** The person's name: how members, the activity log and emails show them. */
function ProfileCard() {
  const qc = useQueryClient()
  const session = useSession()
  const user = session.data?.user
  const [name, setName] = useState<string | null>(null)
  const rename = useMutation({
    mutationFn: (n: string) => api.renameMe(n),
    onSuccess: (u) => {
      qc.setQueryData(qk.session, (old: typeof session.data) => (old ? { ...old, user: u } : old))
      setName(null)
      toast.success('Name saved')
    },
    onError: (e) => toastApiError(e),
  })

  if (!user) {
    return <Skeleton className="h-32" />
  }

  const value = name ?? user.name

  return (
    <Card title="Profile" icon={<UserRound />} description={`You sign in as ${user.email}.`}>
      <form
        className="grid gap-3"
        onSubmit={(e) => {
          e.preventDefault()

          if (value.trim()) {
            rename.mutate(value.trim())
          }
        }}
      >
        <Field>
          <FieldLabel htmlFor="profile-name" required>
            Name
          </FieldLabel>
          <div className="flex gap-2">
            <Input
              id="profile-name"
              className="min-w-0 sm:max-w-sm"
              maxLength={100}
              aria-required
              value={value}
              onChange={(e) => setName(e.target.value)}
            />
            <Button type="submit" disabled={rename.isPending || !value.trim() || value === user.name}>
              Save
            </Button>
          </div>
        </Field>
      </form>
    </Card>
  )
}

/** Every workspace the person belongs to, with their role; owners add new ones. */
function WorkspacesCard() {
  const { isOwner } = useRole()
  const session = useSession()
  const [creating, setCreating] = useState(false)
  const switchTo = useSwitchWorkspace()
  const workspaces = session.data?.workspaces ?? NO_WORKSPACES

  return (
    <>
      <Card
        title="Workspaces"
        description="Each workspace has its own posts, channels, members and plan."
        action={
          isOwner && (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus /> New workspace
            </Button>
          )
        }
      >
        <ul className="grid divide-y" data-testid="account-workspaces">
          {workspaces.map((w) => (
            <li key={w.id} className="flex flex-wrap items-center gap-3 py-3">
              <div className="min-w-0 flex-1">
                <p className="flex min-w-0 items-center gap-2 text-sm font-medium">
                  <span className="truncate">{w.name}</span>
                  {w.current && (
                    <span className="shrink-0 rounded bg-brand-soft px-1.5 py-0.5 text-[11px] font-medium text-brand">
                      Current
                    </span>
                  )}
                </p>
                <p className="text-xs text-muted-foreground">{ROLE_LABELS[w.role]}</p>
              </div>
              {!w.current && (
                <Button variant="outline" size="sm" disabled={switchTo.isPending} onClick={() => switchTo.mutate(w.id)}>
                  Open
                </Button>
              )}
            </li>
          ))}
        </ul>
      </Card>
      {creating && <NewWorkspaceDialog open={creating} onOpenChange={setCreating} />}
    </>
  )
}

const NO_WORKSPACES: MyWorkspace[] = []
