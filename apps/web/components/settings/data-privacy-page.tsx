'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Archive,
  Download,
  Eraser,
  FileDown,
  HardDriveDownload,
  Loader2,
  ScrollText,
  ShieldAlert,
  Trash2,
} from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'
import { toast } from 'sonner'

import { CodeBlock, InlineCode } from '@/components/common/code-block'
import { PageContainer, PageHeader } from '@/components/common/page-header'
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
import {
  ApiError,
  api,
  errorMessage,
  eventsExportUrl,
  exportDownloadUrl,
  setCsrfToken,
  type WorkspaceExport,
} from '@/lib/api'
import { browser } from '@/lib/browser'
import { formatBytes, formatDateTime } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useMode, useSession } from '@/lib/queries'

import { SettingsCard } from './settings-card'
import { SettingsNav } from './settings-nav'

const ACTIVE = new Set<WorkspaceExport['status']>(['pending', 'running'])

function ActivityExportCard() {
  return (
    <SettingsCard
      title="Activity log"
      icon={<ScrollText />}
      description="Every event, oldest first. NDJSON keeps the full event shape; CSV opens in a spreadsheet."
    >
      <div className="grid gap-3">
        <div className="flex flex-wrap gap-2">
          <Button asChild variant="outline">
            <a href={eventsExportUrl({ format: 'ndjson' })} download data-testid="export-all-ndjson">
              <FileDown /> Download NDJSON
            </a>
          </Button>
          <Button asChild variant="outline">
            <a href={eventsExportUrl({ format: 'csv' })} download data-testid="export-all-csv">
              <FileDown /> Download CSV
            </a>
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          Need a slice? Filter the{' '}
          <Link href="/activity" className="text-brand hover:underline">
            Activity
          </Link>{' '}
          page and use <strong>Export</strong> there.
        </p>
      </div>
    </SettingsCard>
  )
}

function WorkspaceExportCard() {
  const qc = useQueryClient()
  const [exportId, setExportId] = useState<string | null>(null)
  const job = useQuery({
    queryKey: qk.workspaceExport(exportId ?? ''),
    queryFn: () => api.workspaceExport(exportId!),
    enabled: !!exportId,
    staleTime: 0,
    refetchInterval: (q) => (q.state.data && ACTIVE.has(q.state.data.status) ? 2000 : false),
  })
  const start = useMutation({
    mutationFn: () => api.requestWorkspaceExport(),
    onSuccess: (x) => {
      qc.setQueryData(qk.workspaceExport(x.id), x)
      setExportId(x.id)
    },
    onError: (e) => {
      const running = e instanceof ApiError && e.isConflict ? e.details?.export_id : undefined

      if (typeof running === 'string' && running) {
        setExportId(running)
        toast.info('An export is already running', { description: 'Showing its progress below.' })

        return
      }

      toastApiError(e, 'Could not start the export')
    },
  })

  const x = job.data
  const gone = job.error instanceof ApiError && job.error.status === 404
  const busy = start.isPending || (!!x && ACTIVE.has(x.status))

  return (
    <SettingsCard
      title="Export workspace"
      icon={<Archive />}
      description="A ZIP with collections, items (with every version), events, media files, webhooks, rules and watchers."
    >
      <div className="grid gap-3" data-testid="workspace-export">
        {x && !gone && (
          <div
            className="rounded-lg border bg-muted/40 p-3 text-sm"
            aria-live="polite"
            data-testid="export-status"
            data-status={x.status}
          >
            {ACTIVE.has(x.status) && (
              <p className="flex items-center gap-2">
                <Loader2 className="size-4 animate-spin text-brand" aria-hidden />
                {x.status === 'pending' ? 'Queued…' : 'Building your archive…'}
                <span className="text-xs text-muted-foreground">
                  started {formatDateTime(x.started_at ?? x.created_at)}
                </span>
              </p>
            )}
            {x.status === 'ready' && (
              <div className="flex flex-wrap items-center gap-3">
                <Button asChild>
                  <a href={exportDownloadUrl(x)} download>
                    <Download /> Download ZIP
                  </a>
                </Button>
                <span className="text-xs text-muted-foreground">
                  {formatBytes(x.size_bytes)} · available until {formatDateTime(x.expires_at)} (24 h)
                </span>
              </div>
            )}
            {x.status === 'failed' && (
              <p className="text-rose-600 dark:text-rose-400">The export failed{x.error ? `: ${x.error}` : '.'}</p>
            )}
            {x.status === 'expired' && <p className="text-muted-foreground">This export expired. Start a new one.</p>}
          </div>
        )}
        {job.error && !gone && <p className="text-sm text-rose-600">{errorMessage(job.error)}</p>}
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant={x?.status === 'ready' ? 'outline' : 'default'}
            onClick={() => start.mutate()}
            disabled={busy}
          >
            <HardDriveDownload /> {x && !gone ? 'Export again' : 'Export workspace'}
          </Button>
          <span className="text-xs text-muted-foreground">
            API keys, sessions and the recovery key are never included. Webhook secrets are redacted.
          </span>
        </div>
      </div>
    </SettingsCard>
  )
}

function DeleteWorkspaceCard() {
  const session = useSession()
  const [open, setOpen] = useState(false)
  const [confirm, setConfirm] = useState('')
  const name = session.data?.workspace.name ?? ''
  const viaSession = session.data?.principal?.type === 'session'
  const del = useMutation({
    mutationFn: () => api.deleteWorkspace(confirm),
    onSuccess: () => {
      // The session is gone. A full page load drops every cache (and any remembered API key) and cannot
      // race the global 401 handler, which would otherwise send us to /login.
      setCsrfToken(null)
      toast.success('Workspace deleted', { description: 'Everything in it is gone.' })
      browser.assign('/setup')
    },
    onError: (e) => toastApiError(e, 'Could not delete the workspace'),
  })
  const matches = confirm === name && name !== ''

  return (
    <SettingsCard
      title="Delete workspace"
      tone="danger"
      icon={<ShieldAlert />}
      description="Permanently deletes this workspace and everything in it. This cannot be undone."
    >
      <div className="grid gap-3 text-sm" data-testid="danger-zone">
        <p className="text-muted-foreground">
          Collections, items, versions, media files, events, webhooks, rules, watchers, notification channels, API keys,
          sessions and exports are removed. Every device is signed out. Earlier backups still contain the data.
        </p>
        {!viaSession && (
          <p className="text-xs text-muted-foreground">Only a signed-in dashboard session can delete a workspace.</p>
        )}
        <div>
          <Button
            variant="destructive"
            onClick={() => {
              setConfirm('')
              setOpen(true)
            }}
            disabled={!viaSession}
          >
            <Trash2 /> Delete workspace…
          </Button>
        </div>
      </div>
      <Dialog open={open} onOpenChange={(o) => !del.isPending && setOpen(o)}>
        <DialogContent className="sm:max-w-md">
          <form
            className="grid gap-4"
            onSubmit={(e) => {
              e.preventDefault()

              if (matches) {
                del.mutate()
              }
            }}
          >
            <DialogHeader>
              <DialogTitle>Delete “{name}” forever?</DialogTitle>
              <DialogDescription>
                All data in this workspace is deleted immediately and you are signed out. There is no undo — export
                first if you want a copy.
              </DialogDescription>
            </DialogHeader>
            <Field data-invalid={del.error instanceof ApiError && del.error.status === 422}>
              <FieldLabel htmlFor="confirm-ws-name">
                Type <strong className="font-semibold">{name}</strong> to confirm
              </FieldLabel>
              <Input
                id="confirm-ws-name"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                autoComplete="off"
                spellCheck={false}
                autoCapitalize="off"
              />
              {del.error instanceof ApiError && del.error.status === 422 && (
                <FieldError>The name does not match.</FieldError>
              )}
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={del.isPending}>
                Cancel
              </Button>
              <Button type="submit" variant="destructive" disabled={!matches || del.isPending}>
                {del.isPending ? 'Deleting…' : 'Delete workspace'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </SettingsCard>
  )
}

function LocalDeleteCard() {
  return (
    <SettingsCard
      title="Delete all data"
      icon={<Trash2 />}
      description="This install has a single workspace, so there is no delete button — remove the data instead."
    >
      <div className="grid gap-3 text-sm" data-testid="local-delete">
        <p className="text-muted-foreground">Stop staqly and delete its data volume (database, uploads and exports):</p>
        <CodeBlock code="docker compose down -v" language="bash" />
        <p className="text-muted-foreground">
          Running without Docker? Stop the server and delete its <InlineCode>DATA_DIR</InlineCode> folder (default{' '}
          <InlineCode>./data</InlineCode>).
        </p>
      </div>
    </SettingsCard>
  )
}

export function DataPrivacyPage() {
  const { isCloud } = useMode()

  return (
    <PageContainer className="max-w-3xl">
      <PageHeader title="Settings" description="Workspace, notifications, data and system status." />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        <ActivityExportCard />
        <WorkspaceExportCard />
        <SettingsCard title="Retention & single items" icon={<Eraser />}>
          <div className="grid gap-2 text-sm text-muted-foreground">
            <p>
              To erase one item including its history, open it and choose <strong>Purge permanently</strong> in the ⋯
              menu.
            </p>
            <p>
              The server can prune old events, deliveries and archived items automatically — see{' '}
              <InlineCode>docs/data-export.md</InlineCode>.
            </p>
          </div>
        </SettingsCard>
        <h2 className="mt-4 text-sm font-semibold text-rose-600 dark:text-rose-400">Danger zone</h2>
        {isCloud ? <DeleteWorkspaceCard /> : <LocalDeleteCard />}
      </div>
    </PageContainer>
  )
}
