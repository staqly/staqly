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

/**
 * The server's "the platform did not accept these details: Instagram: Unsupported request" splits at the first
 * colon into the sentence and the platform's own text (without its name, the title has it); other messages keep
 * their parenthesised part as the platform's text, like delivery errors.
 */
export function splitConnectError(error: string, name: string): { summary: string; detail: string } {
  const colon = error.indexOf(': ')
  const { summary, detail } =
    colon < 0 ? splitDeliveryError(error) : { summary: error.slice(0, colon), detail: error.slice(colon + 2) }

  return {
    summary: summary.charAt(0).toUpperCase() + summary.slice(1),
    detail: detail.startsWith(`${name}: `) ? detail.slice(name.length + 2) : detail,
  }
}

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
  const { summary, detail } = splitConnectError(error, name)
  const retry = useMutation({
    mutationFn: () => api.oauthStart(provider),
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => toastApiError(e),
  })
  const leaving = retry.isPending || retry.isSuccess

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md" data-testid="connect-error">
        <DialogHeader className="items-center text-center sm:text-center">
          {p && <ProviderTile id={provider} name={name} className="mb-2 size-12" />}
          <DialogTitle>{p ? `${name} wasn't connected` : 'The channel was not connected'}</DialogTitle>
          <DialogDescription className="wrap-anywhere">{summary}</DialogDescription>
        </DialogHeader>
        {detail && (
          <figure className="grid gap-1.5" data-testid="connect-error-detail">
            <figcaption className="text-xs font-medium text-muted-foreground">
              {p ? `${name} answered` : 'The platform answered'}
            </figcaption>
            <blockquote className="max-h-40 overflow-y-auto rounded-md border-l-2 border-destructive bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap text-foreground wrap-anywhere select-text">
              {detail}
            </blockquote>
          </figure>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Back to Channels
          </Button>
          {/* A platform asking for its server first (Mastodon) starts again from Channels, where it asks. */}
          {p?.auth === 'oauth2' && !p.credential_fields?.length && (
            // Busy until the browser has left for the platform, not only until the API answered.
            <Button onClick={() => retry.mutate()} disabled={leaving} aria-busy={leaving}>
              {leaving ? 'Opening...' : 'Try again'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
