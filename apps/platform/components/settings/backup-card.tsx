'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { DatabaseBackup, Download, Plus } from 'lucide-react'
import Link from 'next/link'

import { CodeBlock } from '@/components/common/code-block'
import { DocsLink } from '@/components/common/docs-link'
import { PageContainer } from '@/components/common/page-header'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api, errorMessage, exportDownloadUrl, type WorkspaceExport } from '@/lib/api'
import { formatBytes, formatDateTime } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useBackupAccess, useRole } from '@/lib/queries'

import { SettingsCard as Card } from './settings-card'
import { SettingsNav } from './settings-nav'

// The single staqly container: no repository needed.
const DOCKER_BACKUP = `umask 077
docker stop -t 30 staqly
docker run --rm --volumes-from staqly:ro alpine sh -ec \\
  '[ ! -s /data/workspace.db-wal ] || { echo "Nonempty WAL: start and stop staqly cleanly first" >&2; exit 1; }' && \\
  docker cp staqly:/data/workspace.db ./staqly-backup.db
chmod 600 ./staqly-backup.db`
const DOCKER_UPLOADS = `docker run --rm --volumes-from staqly:ro -v "$PWD":/out \\
  -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" alpine sh -ec '
    umask 077
    cd /data
    files="uploads"
    for key in secret.key notify_secret.key access_token; do
      if [ -f "$key" ]; then files="$files $key"; fi
    done
    tar czf /out/staqly-uploads.tar.gz --exclude="uploads/workspaces/*/thumbs" $files
    chown "$HOST_UID:$HOST_GID" /out/staqly-uploads.tar.gz
  '
docker start staqly`

/**
 * Backups. Local: the commands from the backup guide (binary or Docker). Cloud: the workspace's backups (ZIP
 * exports of everything) with Download, on plans with data export only; Starter gets a pointer to its data-only export in
 * Data & privacy and to the plans.
 */
export function BackupCard() {
  const access = useBackupAccess()
  const { canManagePeople } = useRole()

  if (access === 'local') {
    return (
      <Card
        title="Backup & restore"
        icon={<DatabaseBackup />}
        description="A complete backup includes the database, uploaded media and keys. Stop staqly until all of them have been copied."
      >
        <Tabs defaultValue="binary" className="grid gap-3 text-sm">
          <TabsList>
            <TabsTrigger value="binary">staqly serve</TabsTrigger>
            <TabsTrigger value="docker">Docker</TabsTrigger>
          </TabsList>
          <TabsContent value="binary" className="grid gap-2">
            <p className="text-muted-foreground">A consistent copy of the database:</p>
            <CodeBlock code="staqly serve backup ~/staqly-backup.db" language="bash" />
            <p className="text-muted-foreground">
              Pass the same <code>--data</code> you run staqly with. Media files are in the uploads folder of that
              directory; archive it and the keys before starting staqly again. The database-only command also works
              while staqly runs, but that does not keep a later media archive consistent with it.
            </p>
          </TabsContent>
          <TabsContent value="docker" className="grid gap-2">
            <p className="text-muted-foreground">Stop the container, then copy its cleanly closed database:</p>
            <CodeBlock code={DOCKER_BACKUP} language="bash" />
            <p className="text-muted-foreground">
              Keep every container using this data volume stopped. After the database copy succeeds, archive media and
              keys, then start staqly again:
            </p>
            <CodeBlock code={DOCKER_UPLOADS} language="bash" />
          </TabsContent>
          <p className="text-muted-foreground">
            Restoring and scheduling backups:{' '}
            <DocsLink path="/self-hosting/backup-and-restore">the backup guide</DocsLink>.
          </p>
        </Tabs>
      </Card>
    )
  }

  // Backups hold the whole workspace: its owners and admins make them (the server asks for the admin scope).
  if (!canManagePeople) {
    return (
      <Card
        title="Backups"
        icon={<DatabaseBackup />}
        description="A backup is a ZIP of the whole workspace: collections, posts, channels, schedules, events and media."
      >
        <p className="text-sm text-muted-foreground" data-testid="backups-admins-only">
          Owners and admins of this workspace make and download its backups. Ask one of them if you need a copy.
        </p>
      </Card>
    )
  }

  if (access === 'data-only') {
    return (
      <Card
        title="Backups"
        icon={<DatabaseBackup />}
        description="Backups of the whole workspace, media files included, come with Plus and above."
      >
        <div className="grid gap-3 text-sm" data-testid="free-backups">
          <p className="text-muted-foreground">
            On Starter you can export your data without the media files in <strong>Data &amp; privacy</strong>.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button asChild size="sm">
              <Link href="/settings/data">Export your data</Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link href="/plans">See plans</Link>
            </Button>
          </div>
        </div>
      </Card>
    )
  }

  // Wait for the plan before showing anything.
  return access === 'paid' ? <CloudBackups /> : null
}

function CloudBackups() {
  const qc = useQueryClient()
  const list = useQuery({
    queryKey: qk.workspaceExports,
    queryFn: () => api.workspaceExports(),
    // While one is being built, check back until it is ready.
    refetchInterval: (q) =>
      q.state.data?.data.some((x) => x.status === 'pending' || x.status === 'running') ? 3000 : false,
  })
  const { mutate: create, isPending } = useMutation({
    mutationFn: () => api.requestWorkspaceExport(),
    // Pending until the list shows the new backup, so the button can't start a second one in between.
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.workspaceExports }),
    onError: (e) => toastApiError(e),
  })
  const rows = list.data?.data ?? []
  const building = rows.some((x) => x.status === 'pending' || x.status === 'running')

  return (
    <Card
      title="Backups"
      icon={<DatabaseBackup />}
      description="A backup is a ZIP of the whole workspace: collections, posts, channels, schedules, events and media. Each one can be downloaded for 24 hours."
    >
      <div className="grid gap-3 text-sm" data-testid="cloud-backups">
        <div>
          <Button size="sm" onClick={() => create()} disabled={isPending || building}>
            <Plus /> {building ? 'Creating backup...' : 'Create backup'}
          </Button>
        </div>
        {list.isError && rows.length === 0 ? (
          <p role="alert" className="text-destructive">
            Couldn&apos;t load the backups: {errorMessage(list.error)}
          </p>
        ) : rows.length === 0 ? (
          <p className="text-muted-foreground">{list.isLoading ? 'Loading...' : 'No backups yet.'}</p>
        ) : (
          <ul className="divide-y rounded-lg border">
            {rows.map((x) => (
              <BackupRow key={x.id} x={x} />
            ))}
          </ul>
        )}
      </div>
    </Card>
  )
}

const STATUS_TEXT: Record<string, string> = {
  pending: 'Waiting...',
  running: 'Creating...',
  failed: 'Failed',
  expired: 'Expired',
}

function BackupRow({ x }: { x: WorkspaceExport }) {
  return (
    <li className="flex items-center gap-3 px-3 py-2">
      <div className="min-w-0 flex-1">
        <div>{formatDateTime(x.created_at)}</div>
        <div className="text-xs text-muted-foreground">
          {x.status === 'ready'
            ? `${formatBytes(x.size_bytes)} · until ${formatDateTime(x.expires_at)}`
            : (STATUS_TEXT[x.status] ?? x.status)}
          {x.status === 'failed' && x.error ? `: ${x.error}` : ''}
        </div>
      </div>
      {x.status === 'ready' && (
        <Button asChild variant="outline" size="sm">
          <a href={exportDownloadUrl(x)} download>
            <Download /> Download
          </a>
        </Button>
      )}
    </li>
  )
}

/** Settings -> Backups: backups on a local install and on Plus and above; on Starter a pointer to the data export. */
export function BackupsPage() {
  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        <BackupCard />
      </div>
    </PageContainer>
  )
}
