'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Sparkles } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Textarea } from '@/components/ui/textarea'
import { api, type Item } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, syncItemInCache } from '@/lib/queries'

/**
 * The model's one-line summary, marked as AI-generated, with Regenerate: optional extra context helps the
 * model (what the item is for, who it is for...).
 */
export function ItemSummary({ item }: { item: Item }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [context, setContext] = useState('')
  const { mutate, isPending } = useMutation({
    mutationFn: () => api.summarizeItem(item.id, context.trim()),
    onSuccess: (updated) => {
      syncItemInCache(qc, updated)
      invalidateAfterItemChange(qc, updated.id)
      setOpen(false)
    },
    onError: (e) => toastApiError(e),
  })

  const regenerate = (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="link" size="xs" className="h-auto p-0 text-xs text-muted-foreground" disabled={isPending}>
          <RefreshCw className={isPending ? 'animate-spin' : undefined} />
          {isPending ? 'Generating...' : item.summary ? 'Regenerate' : 'Summarize with AI'}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="grid w-80 gap-2">
        <label htmlFor="summary-context" className="text-sm font-medium">
          Extra context <span className="font-normal text-muted-foreground">(optional)</span>
        </label>
        <Textarea
          id="summary-context"
          rows={3}
          maxLength={500}
          value={context}
          placeholder="e.g. A caption for our sneaker shop's Instagram"
          onChange={(e) => setContext(e.target.value)}
        />
        <p className="text-xs text-muted-foreground">Helps the model describe this item. Takes a few seconds.</p>
        <Button size="sm" onClick={() => mutate()} disabled={isPending}>
          <Sparkles /> {isPending ? 'Generating...' : 'Generate summary'}
        </Button>
      </PopoverContent>
    </Popover>
  )

  return (
    <div
      className="grid gap-1 rounded-xl border border-violet-500/25 bg-violet-500/5 px-3 py-2"
      data-testid="item-summary"
    >
      <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <Sparkles className="size-3.5 text-violet-500" /> AI summary
        </span>
        {regenerate}
      </div>
      {item.summary ? (
        <p className="text-sm">{item.summary}</p>
      ) : (
        <p className="text-sm text-muted-foreground">No summary yet.</p>
      )}
    </div>
  )
}
