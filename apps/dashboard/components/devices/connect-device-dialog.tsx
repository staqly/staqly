'use client'

import { useMutation } from '@tanstack/react-query'
import { Clock, RefreshCw, ShieldAlert, WifiOff } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useRef, useState } from 'react'

import { Callout } from '@/components/common/callout'
import { CopyButton } from '@/components/common/copy-button'
import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'

export const QR_WARNING =
  "This QR grants full access to your workspace. Only scan it with your own device and don't share a screenshot."

function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!active) {
      return
    }

    const t = setInterval(() => setNow(Date.now()), 1000)

    return () => clearInterval(t)
  }, [active])

  return now
}

/** Renders `backtick` spans from server hints as inline code. */
function withCode(text: string) {
  return text.split(/(`[^`]+`)/g).map((part, i) =>
    part.startsWith('`') && part.endsWith('`') ? (
      // oxlint-disable-next-line react/no-array-index-key -- fragments of one static string; position is their identity
      <code key={i} className="rounded bg-amber-500/15 px-1 font-mono text-[0.92em]">
        {part.slice(1, -1)}
      </code>
    ) : (
      part
    ),
  )
}

function formatLeft(ms: number) {
  const s = Math.max(0, Math.ceil(ms / 1000))

  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

export function ConnectDeviceDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Connect a device</DialogTitle>
          <DialogDescription>
            Scan with your phone&apos;s camera to open staqly there — already signed in.
          </DialogDescription>
        </DialogHeader>
        {open && <ConnectDeviceBody />}
      </DialogContent>
    </Dialog>
  )
}

function ConnectDeviceBody() {
  const link = useMutation({ mutationFn: () => api.createDeviceLink() })
  const { mutate } = link
  const started = useRef(false)

  // Generate a fresh one-time code each time the dialog opens; regenerate only on demand.
  useEffect(() => {
    if (started.current) {
      return
    }

    started.current = true
    mutate()
  }, [mutate])

  const data = link.data
  const now = useNow(!!data)
  const left = data ? new Date(data.expires_at).getTime() - now : 0
  const expired = !!data && left <= 0

  if (link.error) {
    return <ErrorState title="Could not create a link" error={link.error} onRetry={() => link.mutate()} />
  }

  if (!data || link.isPending) {
    return (
      <div className="grid justify-items-center gap-4 py-2">
        <Skeleton className="size-[232px] rounded-2xl" />
        <Skeleton className="h-10 w-full" />
      </div>
    )
  }

  return (
    <div className="flex min-w-0 flex-col gap-4" data-testid="connect-device">
      {!data.reachable && (
        <Callout role="note" data-testid="unreachable-hint" icon={WifiOff}>
          <span className="block font-medium">Your phone can&apos;t reach this computer yet</span>
          <span className="mt-1 block text-[13px] leading-relaxed whitespace-pre-line">
            {withCode(
              data.hint ||
                'This link points at localhost, so only this computer can open it. Open the dashboard to your network and set PUBLIC_DASHBOARD_URL — see the docs.',
            )}
          </span>
        </Callout>
      )}

      <div className="relative mx-auto">
        <div
          className={cn(
            'rounded-2xl bg-white p-4 shadow-sm ring-1 ring-black/5 transition',
            (expired || !data.reachable) && 'opacity-40',
            expired && 'blur-[2px]',
          )}
        >
          <QRCodeSVG
            value={data.url}
            size={200}
            level="M"
            marginSize={0}
            title="Device link QR code"
            data-testid="device-qr"
            imageSettings={{ src: '/icon.svg', height: 36, width: 36, excavate: true }}
          />
        </div>
        {expired && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-2">
            <span className="rounded-full bg-background/90 px-3 py-1 text-sm font-medium shadow-sm">Code expired</span>
          </div>
        )}
      </div>

      <div className="flex items-center justify-center gap-2 text-sm" aria-live="polite">
        <Clock className="size-4 text-muted-foreground" />
        {expired ? (
          <span className="text-muted-foreground">This code has expired.</span>
        ) : (
          <span className="text-muted-foreground">
            Expires in{' '}
            <span className="font-medium text-foreground tabular-nums" data-testid="device-countdown">
              {formatLeft(left)}
            </span>
            {' · '}works once
          </span>
        )}
      </div>

      <div className="flex items-center gap-1 rounded-lg border bg-muted/40 py-1 pr-1 pl-3">
        <span className="min-w-0 flex-1 truncate font-mono text-xs" title={data.url} data-testid="device-url">
          {data.url}
        </span>
        <CopyButton value={data.url} label="Copy device link" />
      </div>

      <Callout tone="danger" icon={ShieldAlert} className="text-[13px]">
        {QR_WARNING}
      </Callout>

      <Button variant={expired ? 'default' : 'outline'} className="pointer-coarse:h-11" onClick={() => link.mutate()}>
        <RefreshCw /> Generate new code
      </Button>
    </div>
  )
}
