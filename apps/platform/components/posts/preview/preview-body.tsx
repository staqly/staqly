'use client'

import { Info } from 'lucide-react'

import { applyLinks, linksNotice } from '@/lib/links'
import { charCount, xLength } from '@/lib/posts'
import { appendHashtags, hashtagsFor } from '@/lib/tags'

import type { PreviewComment } from './parts'
import type { PreviewState } from './post-preview'
import { AllChannelsCard, PlatformPreview } from './templates'

function Note({ children }: { children: React.ReactNode }) {
  return (
    <p className="mt-3 flex items-start gap-1.5 text-xs text-muted-foreground">
      <Info className="mt-px size-3.5 shrink-0" aria-hidden />
      <span className="min-w-0 wrap-anywhere">{children}</span>
    </p>
  )
}

const written = (parts: PreviewState['thread']) => parts.filter((p) => p.content.trim() || p.media.length)

/**
 * The picked tab's preview, made from the composer's state as the API would send it: the channel's own text or the
 * shared one, with its links handled its way and the tags as hashtags at the end.
 */
export function PreviewBody({ state, tab }: { state: PreviewState; tab: string }) {
  const channel = state.channels.find((c) => c.id === tab)

  if (!channel) {
    return (
      <AllChannelsCard
        channels={state.channels}
        text={appendHashtags(state.content, state.tags)}
        media={state.media}
        comments={written(state.thread).map((p) => ({ key: p.uid, text: p.content, media: p.media }))}
      />
    )
  }

  const provider = state.providers.find((p) => p.id === channel.provider)
  const custom = channel.provider === 'custom'
  const links = applyLinks(channel.links, state.overrides[channel.id]?.content ?? state.content)
  const text = hashtagsFor(channel, provider) ? appendHashtags(links.text, state.tags) : links.text
  const parts = written(state.thread)
  const takesComments = custom || !!channel.comments
  const comments: PreviewComment[] = takesComments
    ? parts.map((p) => ({ key: p.uid, text: applyLinks(channel.links, p.content).text, media: p.media }))
    : []

  return (
    <>
      <PlatformPreview
        // A new channel starts with its text folded again.
        key={channel.id}
        channel={channel}
        providerName={provider?.name ?? channel.provider}
        text={text}
        media={state.media}
        comments={comments}
        maxChars={provider?.limits?.max_chars ?? 0}
        count={channel.provider === 'x' ? xLength : charCount}
        settings={{ ...state.overrides[channel.id]?.settings, ...(state.title.trim() ? { title: state.title } : {}) }}
      />
      {links.found.length > 0 && <Note>{linksNotice(channel.links, channel.name)}</Note>}
      {!takesComments && parts.length > 0 && (
        <Note>{channel.name} can&apos;t post comments: the post goes out there without them.</Note>
      )}
    </>
  )
}
