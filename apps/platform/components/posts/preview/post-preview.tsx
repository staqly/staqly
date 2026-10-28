'use client'

import { ArrowLeft } from 'lucide-react'
import { lazy, memo, Suspense, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import type { Channel, Media, Provider } from '@/lib/api'

import { AllChannelsTab, ChannelTab } from '../channel-tab'

// The platforms' templates load with the first preview, apart from the drawer around them.
const PreviewBody = lazy(() => import('./preview-body').then((m) => ({ default: m.PreviewBody })))

export interface PreviewState {
  /** The picked channels. */
  channels: Channel[]
  providers: Provider[]
  /** The shared text. */
  content: string
  /** Each channel's own text (null: the shared one) and settings. */
  overrides: Record<string, { content: string | null; settings: Record<string, string> }>
  media: Media[]
  thread: { uid: string; content: string; media: Media[] }[]
  tags: string[]
}

function PreviewSkeleton() {
  return (
    <div className="grid gap-3 rounded-2xl border p-4">
      <div className="flex items-center gap-3">
        <Skeleton className="size-10 rounded-full" />
        <Skeleton className="h-4 w-40" />
      </div>
      <Skeleton className="h-20" />
    </div>
  )
}

/** The drawer's body: its own tab state, so every opening starts on the channel the composer shows. */
function PreviewContent({ state, initialTab }: { state: PreviewState; initialTab: string }) {
  const [tab, setTab] = useState(initialTab)
  const { channels } = state
  // A channel taken out of the post falls back to All channels.
  const shown = channels.some((c) => c.id === tab) ? tab : 'all'

  return (
    <div className="grid min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)]">
      <div role="tablist" aria-label="Preview for" className="flex flex-wrap items-center gap-1.5 border-b px-4 pb-3">
        <AllChannelsTab selected={shown === 'all'} onSelect={setTab} />
        {channels.map((ch) => (
          <ChannelTab
            key={ch.id}
            channel={ch}
            selected={shown === ch.id}
            label="Preview for"
            testId="preview-tab"
            onSelect={setTab}
          />
        ))}
      </div>
      <div
        role="tabpanel"
        aria-label={shown === 'all' ? 'All channels' : channels.find((c) => c.id === shown)?.name}
        // A scrolling region keyboards can reach.
        tabIndex={0}
        className="overflow-y-auto overscroll-contain bg-muted/40 p-4 pb-[max(1rem,env(safe-area-inset-bottom))] outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
        data-testid="post-preview-panel"
      >
        <Suspense fallback={<PreviewSkeleton />}>
          <PreviewBody state={state} tab={shown} />
        </Suspense>
      </div>
    </div>
  )
}

/**
 * The live preview beside the composer: a second drawer, not modal, so the composer stays usable while it is open
 * and every keystroke shows here. On phones it covers the composer, with a way back in its header.
 */
export const PostPreviewSheet = memo(function PostPreviewSheet({
  open,
  onOpenChange,
  state,
  initialTab,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  state: PreviewState
  /** The composer's text tab: 'all' or a channel id. */
  initialTab: string
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} modal={false}>
      <SheetContent
        side="left"
        className="gap-0 data-[side=left]:w-full data-[side=left]:sm:max-w-md xl:data-[side=left]:max-w-[min(32rem,max(24rem,calc(100vw-57rem)))]"
        data-testid="post-preview-sheet"
      >
        <SheetHeader className="flex-row items-center gap-2">
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label="Back to the post"
            tooltip="Back to the post"
            onClick={() => onOpenChange(false)}
            className="-ml-1.5"
          >
            <ArrowLeft />
          </Button>
          <div className="min-w-0">
            <SheetTitle>Preview</SheetTitle>
            <SheetDescription>How the post looks on each channel, as you type.</SheetDescription>
          </div>
        </SheetHeader>
        {open && <PreviewContent state={state} initialTab={initialTab} />}
      </SheetContent>
    </Sheet>
  )
})
