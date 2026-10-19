'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Building2, Plus } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useRole, useSwitchWorkspace } from '@/lib/queries'
import { ROLE_LABELS } from '@/lib/roles'

import { NewWorkspaceDialog } from './new-workspace-dialog'
import { NotificationPrefsCard } from './notification-prefs'
import { SettingsCard as Card } from './settings-card'
import { SettingsNav } from './settings-nav'
import { SignInMethodsCard } from './sign-in-methods'

export function AccountPage() {
  const { accountOwner, accountsEnabled } = useRole()

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        <SignInMethodsCard />
        {accountsEnabled && <NotificationPrefsCard />}
        {accountOwner && <AccountCard />}
      </div>
    </PageContainer>
  )
}

function AccountCard() {
  const qc = useQueryClient()
  const account = useQuery({ queryKey: qk.account, queryFn: api.account })
  const [name, setName] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const switchTo = useSwitchWorkspace()
  const rename = useMutation({
    mutationFn: (n: string) => api.renameAccount(n),
    onSuccess: (a) => {
      qc.setQueryData(qk.account, a)
      setName(null)
      toast.success('Account renamed')
    },
    onError: (e) => toastApiError(e),
  })

  if (account.isPending) {
    return <Skeleton className="h-40" />
  }

  if (account.isError) {
    return <ErrorState error={account.error} onRetry={() => void account.refetch()} />
  }

  const a = account.data
  const value = name ?? a.name

  return (
    <>
      <Card title="Account" icon={<Building2 />} description={`Owned by ${a.owner.name} (${a.owner.email}).`}>
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
            <FieldLabel htmlFor="account-name" required>
              Name
            </FieldLabel>
            <div className="flex gap-2">
              <Input
                id="account-name"
                className="min-w-0 sm:max-w-sm"
                maxLength={100}
                value={value}
                onChange={(e) => setName(e.target.value)}
              />
              <Button type="submit" disabled={rename.isPending || !value.trim() || value === a.name}>
                Save
              </Button>
            </div>
          </Field>
          <p className="text-xs text-muted-foreground">Created {formatDateTime(a.created_at)}</p>
        </form>
      </Card>
      <Card
        title="Workspaces"
        description="Each workspace has its own posts, channels, members and plan."
        action={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus /> New workspace
          </Button>
        }
      >
        <ul className="grid divide-y" data-testid="account-workspaces">
          {a.workspaces.map((w) => (
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
                <Button
                  variant="outline"
                  size="sm"

                  disabled={switchTo.isPending}
                  onClick={() => switchTo.mutate(w.id)}
                >
                  Open
                </Button>
              )}
            </li>
          ))}
        </ul>
      </Card>
      <NewWorkspaceDialog open={creating} onOpenChange={setCreating} />
    </>
  )
}
