'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { InlineCode } from '@/components/common/code-block'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { ErrorState } from '@/components/common/error-state'
import { SecretField } from '@/components/common/secret-field'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { cn } from '@/lib/utils'

const accessTokenKey = ['access-token'] as const

/**
 * A local install's access token: what agents, scripts and n8n send to sign in (this browser signed in with it
 * too). Masked until revealed; a new token stops the old one at once and signs every other browser out.
 */
export function AccessTokenCard({ className }: { className?: string }) {
  const qc = useQueryClient()
  // gcTime 0: the secret leaves the cache (and this tab's memory) as soon as the card closes.
  const token = useQuery({ queryKey: accessTokenKey, queryFn: api.accessToken, staleTime: 60_000, gcTime: 0 })
  const [confirming, setConfirming] = useState(false)
  const rotate = useMutation({
    mutationFn: api.rotateAccessToken,
    onSuccess: (res) => {
      qc.setQueryData(accessTokenKey, { token: res.token })
      void qc.invalidateQueries({ queryKey: ['sessions'] })
      toast.success('New access token made', {
        description:
          res.sessions_revoked > 0
            ? `The old one stopped working and ${res.sessions_revoked === 1 ? 'one other browser was' : `${res.sessions_revoked} other browsers were`} signed out. Give your agents the new token.`
            : 'The old one stopped working. Give your agents the new token.',
      })
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <section className={cn('grid gap-3 rounded-xl border bg-card p-4', className)} data-testid="access-token-card">
      <div className="flex flex-wrap items-start gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-brand-soft text-brand">
          <KeyRound className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">Access token</h2>
          <p className="text-sm text-muted-foreground">
            Agents, scripts and n8n sign in with it. In a terminal, set <InlineCode>STAQLY_API_KEY</InlineCode> to it
            and the commands below pick it up.
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => setConfirming(true)}
          disabled={rotate.isPending || !token.data}
          data-testid="rotate-access-token"
        >
          <RefreshCw /> {rotate.isPending ? 'Making a new token...' : 'New token'}
        </Button>
      </div>
      {token.isLoading ? (
        <Skeleton className="h-9" />
      ) : token.error ? (
        <ErrorState title="Could not load the access token" error={token.error} onRetry={() => token.refetch()} />
      ) : (
        token.data && <SecretField key={token.data.token} value={token.data.token} label="access token" masked />
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Make a new access token?"
        description="The current token stops working right away: agents and scripts that use it need the new one. Every other browser is signed out; this one stays signed in."
        confirmLabel="Make a new token"
        variant="destructive"
        onConfirm={() => rotate.mutate()}
      />
    </section>
  )
}
