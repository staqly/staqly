'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RotateCcw } from 'lucide-react'
import { useEffect } from 'react'

import { CopyButton } from '@/components/common/copy-button'
import { Button } from '@/components/ui/button'
import { api, errorMessage } from '@/lib/api'
import { namedToast } from '@/lib/notify'
import { qk } from '@/lib/queries'

/**
 * Telegram through the server's bot: add the bot to the channel or group, send /connect CODE there, and the
 * server connects that chat; this waits for it. `bot` is the bot's @username.
 */
export function TelegramConnect({ bot, onDone, onOwnBot }: { bot: string; onDone: () => void; onOwnBot: () => void }) {
  const qc = useQueryClient()
  const start = useMutation({ mutationFn: api.startTelegramConnect })
  const code = start.data?.code
  const status = useQuery({
    queryKey: ['telegram-connect', code],
    queryFn: () => api.telegramConnect(code!),
    enabled: !!code,
    // Every 2 s while it waits; nothing once it is connected or the code ran out.
    refetchInterval: (q) => (q.state.data && q.state.data.status !== 'waiting' ? false : 2000),
  })
  const state = status.data ?? start.data
  const name = bot.replace(/^@/, '')

  // A code as the panel opens.
  useEffect(() => {
    start.mutate()
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- once per opening
  }, [])

  useEffect(() => {
    if (status.data?.status === 'connected') {
      void qc.invalidateQueries({ queryKey: qk.channels })
      void api.channels().then((r) => {
        const c = r.data?.find((ch) => ch.id === status.data!.channel_id)

        namedToast(c?.name ?? 'Telegram', 'is connected', c?.handle || undefined)
      })
      onDone()
    }
  }, [status.data, qc, onDone])

  if (start.isError) {
    return (
      <div className="grid gap-3">
        <p className="text-sm text-destructive">{errorMessage(start.error)}</p>
        <Button variant="outline" className="justify-self-start" onClick={() => start.mutate()}>
          <RotateCcw /> Try again
        </Button>
      </div>
    )
  }

  return (
    <div className="grid gap-5" data-testid="telegram-connect">
      <ol className="grid gap-5">
        <li className="grid gap-2">
          <p className="text-sm font-medium">1. Add {bot} to your channel or group</p>
          <p className="text-xs text-muted-foreground">
            In a channel it must be an administrator allowed to post messages (and to delete them, so it can tidy up).
          </p>
          <div className="flex flex-wrap gap-2">
            <Button asChild variant="outline" size="sm">
              <a
                href={`https://t.me/${name}?startchannel=true&admin=post_messages+delete_messages`}
                target="_blank"
                rel="noopener noreferrer"
              >
                Add to a channel
              </a>
            </Button>
            <Button asChild variant="outline" size="sm">
              <a href={`https://t.me/${name}?startgroup=true`} target="_blank" rel="noopener noreferrer">
                Add to a group
              </a>
            </Button>
          </div>
        </li>
        <li className="grid gap-2">
          <p className="text-sm font-medium">2. Send this in that chat</p>
          {state ? (
            <div className="flex items-center gap-2 rounded-lg border bg-muted/40 py-1.5 pr-1.5 pl-3">
              <code className="min-w-0 flex-1 truncate font-mono text-sm" data-testid="telegram-command">
                {state.command}
              </code>
              <CopyButton value={state.command} label="Copy the command" />
            </div>
          ) : (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Getting a code...
            </p>
          )}
        </li>
      </ol>
      {state?.status === 'expired' ? (
        <div className="grid gap-2">
          <p className="text-sm text-muted-foreground">This code ran out. Get a new one and send that instead.</p>
          <Button variant="outline" className="justify-self-start" onClick={() => start.mutate()}>
            <RotateCcw /> New code
          </Button>
        </div>
      ) : (
        state && (
          <div className="grid gap-1" role="status">
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" /> Waiting for the command in your chat...
            </p>
            {state.error && <p className="text-sm text-destructive">{state.error}</p>}
          </div>
        )
      )}
      <button
        type="button"
        onClick={onOwnBot}
        className="justify-self-start text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
      >
        Use a bot of your own instead
      </button>
    </div>
  )
}
