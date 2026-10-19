'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, MoreHorizontal, Plus, RotateCw, ShieldOff } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useSearchParams } from 'next/navigation'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SecretField } from '@/components/common/secret-field'
import { IntegrationsHeader } from '@/components/integrations/integrations-header'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useIsMobile } from '@/hooks/use-mobile'
import { api, type APIKey } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useMode } from '@/lib/queries'
import { SCOPE_INFO } from '@/lib/scope-info'
import { SCOPE_PRESETS } from '@/lib/scope-presets'
import { forgetApiKey, rememberApiKey } from '@/lib/session-key'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { CloudModeCard } from './shared'

// Local installs only: out of the cloud pages' first load.
const AccessTokenCard = dynamic(() => import('./access-token').then((m) => m.AccessTokenCard))

export { SCOPE_INFO }

// The create form (scopes multiselect, expiry presets, date picker) loads when it is first opened.
const CreateKeyDialog = dynamic(() => import('./create-key-dialog').then((m) => m.CreateKeyDialog))

export { SCOPE_PRESETS }

/** Tones as in lib/tones.ts: revoked is danger, expired neutral. */
function keyState(k: APIKey): { label: string; cls: string } {
  if (k.revoked_at) {
    return { label: 'Revoked', cls: TONES.danger.badge }
  }

  if (k.expires_at && new Date(k.expires_at).getTime() < Date.now()) {
    return { label: 'Expired', cls: TONES.neutral.badge }
  }

  return { label: 'Active', cls: TONES.success.badge }
}

function NewKeyDialog({
  apiKey,
  onOpenChange,
  title = 'Your new API key',
}: {
  apiKey: APIKey | null
  onOpenChange: (o: boolean) => void
  title?: string
}) {
  return (
    <Dialog open={!!apiKey} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            Copy it now — for your security it is <strong>shown only once</strong>. Staqly stores only a hash.
          </DialogDescription>
        </DialogHeader>
        {apiKey?.key && (
          <div className="grid gap-3">
            <SecretField value={apiKey.key} label="New API key" />
            <div className="flex flex-wrap gap-1">
              {apiKey.scopes.map((s) => (
                <span key={s} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">
                  {s}
                </span>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              The integration pages in this tab are pre-filled with this key until you reload.
            </p>
          </div>
        )}
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>I&apos;ve copied it</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ApiKeysManager() {
  const { isLocal } = useMode()

  if (isLocal) {
    return (
      <PageContainer>
        <PageHeader
          title="API keys"
          icon={<KeyRound className="size-4" />}
          description="A local install has no API keys: agents sign in with its access token."
        />
        <AccessTokenCard className="mt-6" />
        <CloudModeCard className="mt-6" />
      </PageContainer>
    )
  }

  return <CloudApiKeysManager />
}

function ScopeList({ scopes }: { scopes: APIKey['scopes'] }) {
  return (
    <div className="flex flex-wrap gap-1">
      {scopes.map((s) => (
        <span key={s} className="rounded bg-muted px-1 py-0.5 font-mono text-[10.5px]" title={SCOPE_INFO[s]}>
          {s}
        </span>
      ))}
    </div>
  )
}

function KeyMenu({
  apiKey: k,
  onPick,
}: {
  apiKey: APIKey
  onPick: (c: { key: APIKey; kind: 'revoke' | 'rotate' }) => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label={`Options for ${k.name}`}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => onPick({ key: k, kind: 'rotate' })}>
          <RotateCw /> Rotate
        </DropdownMenuItem>
        <DropdownMenuItem variant="destructive" onSelect={() => onPick({ key: k, kind: 'revoke' })}>
          <ShieldOff /> Revoke
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function CloudApiKeysManager() {
  const params = useSearchParams()
  const qc = useQueryClient()
  const keys = useQuery({ queryKey: qk.apiKeys, queryFn: api.apiKeys })
  const [createOpen, setCreateOpen] = useState(params.get('new') === '1')
  const [revealed, setRevealed] = useState<{ key: APIKey; title: string } | null>(null)
  const [confirm, setConfirm] = useState<{ key: APIKey; kind: 'revoke' | 'rotate' } | null>(null)
  const isPhone = useIsMobile(640)

  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeApiKey(id),
    onSuccess: (_res, id) => {
      forgetApiKey(id)
      void qc.invalidateQueries({ queryKey: qk.apiKeys })
      toast.success('API key revoked')
    },
    onError: (e) => toastApiError(e),
  })
  const rotate = useMutation({
    mutationFn: (id: string) => api.rotateApiKey(id),
    onSuccess: (k, id) => {
      void qc.invalidateQueries({ queryKey: qk.apiKeys })
      forgetApiKey(id)

      if (k.key) {
        rememberApiKey({ id: k.id, name: k.name, key: k.key })
      }

      setRevealed({ key: k, title: 'Rotated API key' })
    },
    onError: (e) => toastApiError(e),
  })

  const list = (keys.data?.data ?? []).toSorted(
    (a, b) => Number(!!a.revoked_at) - Number(!!b.revoked_at) || b.created_at.localeCompare(a.created_at),
  )

  return (
    <PageContainer>
      <IntegrationsHeader
        active="keys"
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus /> Create API key
          </Button>
        }
      />
      <p className="mt-4 text-sm text-muted-foreground">
        Keys for agents and automations, each limited to the permissions you pick.
      </p>
      <div className="mt-6">
        {keys.isLoading ? (
          <Skeleton className="h-48" />
        ) : keys.error ? (
          <ErrorState error={keys.error} onRetry={() => keys.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState
            icon={<KeyRound />}
            title="No API keys"
            description="Create a key for each agent or automation."
          />
        ) : // Phones: one card per key, so name, scopes, status and the menu are all in view.
        isPhone ? (
          <ul className="grid gap-2" data-testid="api-key-cards">
            {list.map((k) => {
              const st = keyState(k)

              return (
                <li
                  key={k.id}
                  className={cn('grid gap-2 rounded-xl border bg-card p-3', k.revoked_at && 'opacity-60')}
                  data-testid="api-key-row"
                >
                  <div className="flex items-start gap-2">
                    <div className="min-w-0 flex-1">
                      <div className="font-medium wrap-anywhere">{k.name}</div>
                      <div className="font-mono text-xs text-muted-foreground">{k.prefix}...</div>
                    </div>
                    <span className={cn('shrink-0 rounded-md px-1.5 py-0.5 text-[11px] font-medium', st.cls)}>
                      {st.label}
                    </span>
                    {!k.revoked_at && <KeyMenu apiKey={k} onPick={setConfirm} />}
                  </div>
                  <ScopeList scopes={k.scopes} />
                  <div className="text-xs text-muted-foreground">
                    {k.last_used_at ? `Used ${timeAgo(k.last_used_at)}` : 'Never used'} · created{' '}
                    {timeAgo(k.created_at)}
                    {k.expires_at && <> · expires {formatDateTime(k.expires_at)}</>}
                  </div>
                </li>
              )
            })}
          </ul>
        ) : (
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Name</TableHead>
                  <TableHead>Key</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead className="hidden md:table-cell">Last used</TableHead>
                  <TableHead className="hidden lg:table-cell">Created</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((k) => {
                  const st = keyState(k)

                  return (
                    <TableRow
                      key={k.id}
                      className={cn(k.revoked_at && 'text-muted-foreground')}
                      data-testid="api-key-row"
                    >
                      <TableCell className="max-w-56 truncate font-medium" title={k.name}>
                        {k.name}
                      </TableCell>
                      <TableCell className="font-mono text-xs">{k.prefix}...</TableCell>
                      <TableCell className="max-w-72">
                        <ScopeList scopes={k.scopes} />
                      </TableCell>
                      <TableCell
                        className="hidden text-muted-foreground md:table-cell"
                        title={formatDateTime(k.last_used_at)}
                      >
                        {k.last_used_at ? timeAgo(k.last_used_at) : 'Never'}
                      </TableCell>
                      <TableCell
                        className="hidden text-muted-foreground lg:table-cell"
                        title={formatDateTime(k.created_at)}
                      >
                        {timeAgo(k.created_at)}
                        {k.expires_at && (
                          <span className="block text-[11px]">expires {formatDateTime(k.expires_at)}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <span className={cn('rounded-md px-1.5 py-0.5 text-[11px] font-medium', st.cls)}>
                          {st.label}
                        </span>
                      </TableCell>
                      <TableCell>{!k.revoked_at && <KeyMenu apiKey={k} onPick={setConfirm} />}</TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </div>

      <CreateKeyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        availableScopes={keys.data?.available_scopes ?? []}
        onCreated={(k) => setRevealed({ key: k, title: 'Your new API key' })}
      />
      <NewKeyDialog
        apiKey={revealed?.key ?? null}
        title={revealed?.title}
        onOpenChange={(o) => !o && setRevealed(null)}
      />
      <ConfirmDialog
        open={!!confirm}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={
          <>
            {confirm?.kind === 'rotate' ? 'Rotate' : 'Revoke'} “{confirm?.key.name}”?
          </>
        }
        description={
          confirm?.kind === 'rotate'
            ? 'The current key stops working immediately and a replacement with the same name and scopes is issued. Update every integration that uses it.'
            : 'Any agent or automation using this key loses access immediately. This cannot be undone.'
        }
        confirmLabel={confirm?.kind === 'rotate' ? 'Rotate key' : 'Revoke key'}
        variant="destructive"
        onConfirm={() => {
          if (!confirm) {
            return
          }

          if (confirm.kind === 'rotate') {
            rotate.mutate(confirm.key.id)
          } else {
            revoke.mutate(confirm.key.id)
          }
        }}
      />
    </PageContainer>
  )
}
