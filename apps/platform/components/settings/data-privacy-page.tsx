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
import { ConfirmNameField, confirmNameError } from '@/components/common/confirm-name-field'
import { DocsLink } from '@/components/common/docs-link'
import { PageContainer } from '@/components/common/page-header'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
import { focusFirstInvalid, formApiError, useFormErrors } from '@/lib/form-errors'
import { formatBytes, formatDateTime } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useBackupAccess, useMode, useSession } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { SettingsCard } from './settings-card'
import { SettingsNav } from './settings-nav'

const ACTIVE = new Set<WorkspaceExport['status']>(['pending', 'running'])

function ActivityExportCard() {
  return (
    <SettingsCard
      title="Activity log"
      icon={<ScrollText />}
      description="Every event, oldest first. Choose CSV for a spreadsheet, or JSONL to keep every detail for other tools."
    >
      <div className="grid gap-3">
        <div className="flex flex-wrap gap-2">
          <Button asChild variant="outline">
            <a href={eventsExportUrl({ format: 'ndjson' })} download data-testid="export-all-ndjson">
              <FileDown /> Download JSONL
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
          <Link href="/activity" className="text-brand underline underline-offset-2 hover:no-underline">
            Activity
          </Link>{' '}
          page and use <strong>Export</strong> there.
        </p>
      </div>
    </SettingsCard>
  )
}

/** The workspace export. `dataOnly` (Starter): the API builds the same ZIP without the media files. */
function WorkspaceExportCard({ dataOnly = false }: { dataOnly?: boolean }) {
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
      description={
        dataOnly
          ? 'A ZIP with your data: collections, posts, channels (without their sign-ins), schedules, events and webhooks. Media files are listed but not included.'
          : 'A ZIP with collections, posts, channels (without their sign-ins), schedules, events, media files and webhooks.'
      }
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
                {x.status === 'pending' ? 'Queued...' : 'Building your archive...'}
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
              <p className={TONES.danger.text}>The export failed{x.error ? `: ${x.error}` : '.'}</p>
            )}
            {x.status === 'expired' && <p className="text-muted-foreground">This export expired. Start a new one.</p>}
          </div>
        )}
        {job.error && !gone && <p className={cn('text-sm', TONES.danger.text)}>{errorMessage(job.error)}</p>}
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
        {dataOnly && (
          <p className="text-xs text-muted-foreground" data-testid="export-data-only">
            Media files, backups and the activity log export come with Plus and above.{' '}
            <Link href="/plans" className="text-brand underline underline-offset-2 hover:no-underline">
              See plans
            </Link>
          </p>
        )}
      </div>
    </SettingsCard>
  )
}

function DeleteWorkspaceCard() {
  const session = useSession()
  const [open, setOpen] = useState(false)
  const [confirm, setConfirm] = useState('')
  const name = session.data?.workspace.name ?? ''
  const form = useFormErrors<'confirm'>({ confirm: confirmNameError(confirm, name) })
  const viaSession = session.data?.principal?.type === 'session'
  const del = useMutation({
    mutationFn: () => api.deleteWorkspace(confirm),
    onSuccess: () => {
      // The session is gone. A full page load drops every cache (and any remembered API key) and cannot
      // race the global 401 handler, which would otherwise send us to /login.
      setCsrfToken(null)
      toast.success('Workspace deleted', { description: 'Everything in it is gone.' })
      browser.assign('/login')
    },
    onError: (e) =>
      form.setServerErrors(formApiError(e, (k) => (k === 'confirm' ? k : null), 'Could not delete the workspace')),
  })

  return (
    <SettingsCard
      title="Delete workspace"
      tone="danger"
      icon={<ShieldAlert />}
      description="Permanently deletes this workspace and everything in it. This cannot be undone."
    >
      <div className="grid gap-3 text-sm" data-testid="danger-zone">
        <p className="text-muted-foreground">
          Collections, posts, channels, schedules, media files, events, webhooks, alerts, API keys, sessions and exports
          are removed. Every device is signed out. Earlier backups still contain the data.
        </p>
        {!viaSession && (
          <p className="text-xs text-muted-foreground">Only a signed-in dashboard session can delete a workspace.</p>
        )}
        <div>
          <Button
            variant="destructive"
            onClick={() => {
              setConfirm('')
              form.reset()
              setOpen(true)
            }}
            disabled={!viaSession}
          >
            <Trash2 /> Delete workspace...
          </Button>
        </div>
      </div>
      <Dialog open={open} onOpenChange={(o) => !del.isPending && setOpen(o)}>
        <DialogContent className="sm:max-w-md">
          <form
            className="grid gap-4"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()

              if (!form.submit()) {
                focusFirstInvalid(e.currentTarget)

                return
              }

              del.mutate()
            }}
          >
            <DialogHeader>
              <DialogTitle>Delete “{name}” forever?</DialogTitle>
              <DialogDescription>
                All data in this workspace is deleted immediately and you are signed out. There is no undo — export
                first if you want a copy.
              </DialogDescription>
            </DialogHeader>
            <ConfirmNameField
              id="confirm-ws-name"
              label="Type the workspace name to confirm"
              expected={name}
              value={confirm}
              onChange={(v) => {
                setConfirm(v)
                form.touch('confirm')
              }}
              error={form.errors.confirm}
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={del.isPending}>
                Cancel
              </Button>
              <Button type="submit" variant="destructive" disabled={del.isPending}>
                {del.isPending ? 'Deleting...' : 'Delete workspace'}
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
      tone="danger"
      title="Delete all data"
      icon={<Trash2 />}
      description="This install has a single workspace, so there is no delete button: remove the data instead."
    >
      <Tabs defaultValue="binary" className="grid gap-3 text-sm" data-testid="local-delete">
        <TabsList>
          <TabsTrigger value="binary">staqly serve</TabsTrigger>
          <TabsTrigger value="docker">Docker</TabsTrigger>
        </TabsList>
        <TabsContent value="binary" className="grid gap-2">
          <p className="text-muted-foreground">
            Stop staqly (and its background service, if installed) and delete its data folder: database, uploads and
            exports.
          </p>
          <CodeBlock code={'staqly service uninstall\nrm -rf ~/.local/share/staqly'} language="bash" />
          <p className="text-muted-foreground">
            Started with <InlineCode>--data</InlineCode>? Delete that folder instead.
          </p>
        </TabsContent>
        <TabsContent value="docker" className="grid gap-2">
          <p className="text-muted-foreground">
            Remove the staqly container and its data volume (database, uploads and exports):
          </p>
          <CodeBlock code={'docker rm -f staqly\ndocker volume rm staqly-data'} language="bash" />
          <p className="text-muted-foreground">
            Use the volume you mounted at <InlineCode>/data</InlineCode> (<InlineCode>docker volume ls</InlineCode>{' '}
            lists them); with <InlineCode>compose.yml</InlineCode> it is <InlineCode>staqly_staqly-data</InlineCode>.
          </p>
        </TabsContent>
      </Tabs>
    </SettingsCard>
  )
}

export function DataPrivacyPage() {
  const { isCloud } = useMode()
  const exportAccess = useBackupAccess()

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        {/* Every plan exports its data. On Starter the workspace ZIP has no media files and the activity log
            export needs Plus or above (the API answers 402); local installs always have everything. */}
        {exportAccess === 'data-only' ? (
          <WorkspaceExportCard dataOnly />
        ) : exportAccess ? (
          <>
            <ActivityExportCard />
            <WorkspaceExportCard />
          </>
        ) : null}
        <SettingsCard title="Retention & single posts" icon={<Eraser />}>
          <div className="grid gap-2 text-sm text-muted-foreground">
            <p>
              To erase one post, open it and choose <strong>Delete</strong> (the bin icon at the top of the post). What
              was published stays on the platform.
            </p>
            <p>
              The server can prune old events, deliveries and finished jobs automatically: see{' '}
              <DocsLink path="/self-hosting/data-export">Data export &amp; retention</DocsLink>.
            </p>
          </div>
        </SettingsCard>
        <h2 className={cn('mt-4 text-sm font-semibold', TONES.danger.text)}>Danger zone</h2>
        {isCloud ? <DeleteWorkspaceCard /> : <LocalDeleteCard />}
      </div>
    </PageContainer>
  )
}
