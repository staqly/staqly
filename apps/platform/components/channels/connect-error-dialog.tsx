'use client'

import { useMutation } from '@tanstack/react-query'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { api } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { splitDeliveryError } from '@/lib/posts'
import { ProviderTile } from '@/lib/providers'
import { useProviders } from '@/lib/queries'

/** A connection that came back from the platform with an error, over the Channels page it returned to. */
export function ConnectErrorDialog({
  provider,
  error,
  onClose,
}: {
  provider: string
  error: string
  onClose: () => void
}) {
  const p = useProviders().data?.find((x) => x.id === provider)
  const name = p?.name ?? 'The platform'
  const { summary, detail } = splitDeliveryError(error)
  const retry = useMutation({
    mutationFn: () => api.oauthStart(provider),
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => toastApiError(e),
  })

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md" data-testid="connect-error">
        <DialogHeader className="items-center text-center sm:text-center">
          {p && <ProviderTile id={provider} name={name} className="mb-2 size-12" />}
          <DialogTitle>{p ? `${name} wasn't connected` : 'The channel was not connected'}</DialogTitle>
          <DialogDescription className="wrap-anywhere">{summary}</DialogDescription>
        </DialogHeader>
        {detail && (
          <details className="text-sm text-muted-foreground">
            <summary className="cursor-pointer select-none">Technical details</summary>
            <code className="mt-1 block font-mono text-xs wrap-anywhere">{detail}</code>
          </details>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Back to Channels
          </Button>
          {p?.auth === 'oauth2' && (
            <Button onClick={() => retry.mutate()} disabled={retry.isPending}>
              {retry.isPending ? 'Opening...' : 'Try again'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
