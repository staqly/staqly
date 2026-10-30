'use client'

/*
 * Each platform's post, drawn with our own markup in its light or dark look (the dashboard's theme picks which).
 * The colours here imitate the platforms, so they are set apart from lib/tones.ts on purpose; greys and link
 * colours are the nearest shades that keep 4.5:1 on their background. Brand marks come from lib/providers only.
 */
import {
  BarChart2,
  Bookmark,
  CornerUpLeft,
  Eye,
  Globe,
  Heart,
  ListFilter,
  MessageCircle,
  MessageSquare,
  MoreHorizontal,
  Music2,
  Repeat2,
  Send,
  Share,
  Star,
  ThumbsDown,
  ThumbsUp,
} from 'lucide-react'
import { useState } from 'react'

import { ChannelAvatar, ChannelStack } from '@/components/channels/channel-avatars'
import type { Channel, Media } from '@/lib/api'
import { ProviderIcon } from '@/lib/providers'
import { formatTime } from '@/lib/schedule'
import { cn } from '@/lib/utils'

import {
  atHandle,
  Avatar,
  bareHandle,
  Carousel,
  isVideo,
  MediaGrid,
  MediaItem,
  MediaStripView,
  PostText,
  ratioOf,
  SYSTEM_FONT,
  type PreviewComment,
  type PreviewProps,
} from './parts'

type Colors = Partial<
  Record<'bg' | 'fg' | 'muted' | 'accent' | 'line' | 'border' | 'bubble' | 'name' | 'chat' | 'dot', string>
>

/**
 * Each platform's colours, light then dark, as CSS variables on its frame. They live here, in the preview's own
 * chunk, not as utility classes in the dashboard's stylesheet that every page loads.
 */
const PALETTES: Record<string, [Colors, Colors]> = {
  x: [
    { bg: '#fff', fg: '#0f1419', muted: '#536471', accent: '#0b6bb3', line: '#cfd9de', border: '#cfd9de' },
    { bg: '#000', fg: '#e7e9ea', muted: '#8b9096', accent: '#4cb0f6', line: '#333639', border: '#2f3336' },
  ],
  bluesky: [
    { bg: '#fff', fg: '#0b0f14', muted: '#5b6b7c', accent: '#0a66d0', line: '#d4dbe2', border: '#d4dbe2' },
    { bg: '#161e27', fg: '#f1f3f5', muted: '#aebbc9', accent: '#4fa2fe', line: '#2e4052', border: '#2e4052' },
  ],
  threads: [
    { bg: '#fff', fg: '#000', muted: '#6b6b6b', accent: '#0064c8', line: '#d9d9d9', border: '#e5e5e5' },
    { bg: '#101010', fg: '#f3f5f7', muted: '#a0a0a0', accent: '#5fb2ff', line: '#333', border: '#2a2a2a' },
  ],
  mastodon: [
    { bg: '#fff', fg: '#282c37', muted: '#606984', accent: '#4a2fc0', line: '#c0cdd9', border: '#c0cdd9' },
    { bg: '#282c37', fg: '#fff', muted: '#9baec8', accent: '#a3a4ff', line: '#444b5d', border: '#393f4f' },
  ],
  linkedin: [
    { bg: '#fff', fg: '#1d1d1d', muted: '#666', accent: '#0a66c2', bubble: '#f2f2f2', border: '#dcdcdc' },
    { bg: '#1b1f23', fg: '#e9e9e9', muted: '#a6a6a6', accent: '#71b7fb', bubble: '#293138', border: '#38434f' },
  ],
  facebook: [
    { bg: '#fff', fg: '#050505', muted: '#65676b', accent: '#0059c7', bubble: '#f0f2f5', border: '#dadde1' },
    { bg: '#242526', fg: '#e4e6eb', muted: '#b0b3b8', accent: '#66adff', bubble: '#3a3b3c', border: '#3e4042' },
  ],
  instagram: [
    {
      bg: '#fff',
      fg: '#000',
      muted: '#737373',
      accent: '#00376b',
      border: '#dbdbdb',
      line: '#a8a8a8',
      bubble: '#efefef',
      dot: '#0095f6',
    },
    {
      bg: '#000',
      fg: '#f5f5f5',
      muted: '#a8a8a8',
      accent: '#e0f1ff',
      border: '#262626',
      line: '#555',
      bubble: '#121212',
      dot: '#0095f6',
    },
  ],
  youtube: [
    { bg: '#fff', fg: '#0f0f0f', muted: '#606060', accent: '#065fd4', bubble: '#f2f2f2' },
    { bg: '#0f0f0f', fg: '#f1f1f1', muted: '#aaa', accent: '#3ea6ff', bubble: '#272727' },
  ],
  slack: [
    { bg: '#fff', fg: '#1d1c1d', muted: '#616061', accent: '#1264a3', border: '#ddd' },
    { bg: '#1a1d21', fg: '#d1d2d3', muted: '#ababad', accent: '#1d9bd1', border: '#35373b' },
  ],
  discord: [
    { bg: '#fff', fg: '#313338', muted: '#5c5e66', accent: '#0068e0', name: '#060607', line: '#c4c9ce' },
    { bg: '#313338', fg: '#dbdee1', muted: '#a5abb3', accent: '#00a8fc', name: '#f2f3f5', line: '#4e5058' },
  ],
  telegram: [
    { bg: '#fff', fg: '#000', chat: '#cfdcc0', bubble: '#fff', muted: '#5b6e7d', accent: '#0f6aa3', line: '#e1effa' },
    {
      bg: '#17212b',
      fg: '#fff',
      chat: '#0e1621',
      bubble: '#182533',
      muted: '#8fa3b5',
      accent: '#71baf2',
      line: '#1f3a52',
    },
  ],
}

const vars = (c: Colors) =>
  Object.entries(c)
    .map(([k, v]) => `--pv-${k}:${v}`)
    .join(';')

const PALETTE_CSS = Object.entries(PALETTES)
  .map(([id, [light, dark]]) => `.pv-${id}{${vars(light)}}.dark .pv-${id}{${vars(dark)}}`)
  .join('')

/** The classes a template uses; the frame's `pv-<platform>` class sets what they mean. */
function palette(id: string) {
  return {
    frame: `pv-${id} bg-(--pv-bg) text-(--pv-fg)`,
    muted: 'text-(--pv-muted)',
    accent: 'text-(--pv-accent)',
    line: 'bg-(--pv-line)',
    border: 'border-(--pv-border)',
    bubble: 'bg-(--pv-bubble)',
    box: 'bg-(--pv-bubble)',
    name: 'text-(--pv-name)',
    chat: 'bg-(--pv-chat)',
    bar: 'bg-(--pv-bg) text-(--pv-fg)',
  }
}

/** The frame every template sits in: names the preview for screen readers, keeps the platform's font. */
function Frame({ label, className, children }: { label: string; className?: string; children: React.ReactNode }) {
  return (
    <article aria-label={label} className={cn('overflow-hidden text-left', SYSTEM_FONT, className)}>
      {children}
    </article>
  )
}

/** A row of the platform's actions: drawn, not working (the post isn't out yet). */
function Actions({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <div aria-hidden className={cn('flex items-center', className)}>
      {children}
    </div>
  )
}

function useClockLabel() {
  // Once, when the preview opens: a picture of a post needs no ticking clock.
  const [time] = useState(() => formatTime(new Date()))

  return time
}

/** Comments as the same account's posts under the first, joined by a line from avatar to avatar (X, Bluesky, Threads, Mastodon). */
function SelfThread({
  comments,
  render,
}: {
  comments: PreviewComment[]
  render: (c: PreviewComment, last: boolean) => React.ReactNode
}) {
  return comments.map((c, i) => <div key={c.key}>{render(c, i === comments.length - 1)}</div>)
}

// ---------------------------------------------------------------------------------------------------------------
// X

const X = palette('x')

function XPost({
  channel,
  text,
  media,
  max,
  count,
  thread,
}: {
  channel: Channel
  text: string
  media: Media[]
  max: number
  count: (t: string) => number
  thread: boolean
}) {
  return (
    <div className="flex gap-3 px-4 pt-3">
      <div className="flex flex-col items-center">
        <Avatar channel={channel} />
        {thread && <span aria-hidden className={cn('mt-1 w-0.5 flex-1', X.line)} />}
      </div>
      <div className="min-w-0 flex-1 pb-3">
        <p className="flex min-w-0 items-baseline gap-1 text-[15px] leading-5">
          <span className="truncate font-bold">{channel.name}</span>
          <span className={cn('truncate', X.muted)}>{atHandle(channel)}</span>
          <span className={cn('shrink-0', X.muted)}>· now</span>
        </p>
        <PostText
          text={text}
          max={max}
          count={count}
          accent={X.accent}
          shortLinks
          className="mt-0.5 text-[15px] leading-5"
        />
        <MediaGrid media={media} className={cn('mt-3 rounded-2xl border', X.border)} min={0.75} max={16 / 9} />
        <Actions className={cn('mt-3 max-w-md justify-between', X.muted)}>
          <MessageCircle className="size-[18px]" />
          <Repeat2 className="size-[18px]" />
          <Heart className="size-[18px]" />
          <BarChart2 className="size-[18px]" />
          <span className="flex gap-3">
            <Bookmark className="size-[18px]" />
            <Share className="size-[18px]" />
          </span>
        </Actions>
      </div>
    </div>
  )
}

function XTemplate(p: PreviewProps) {
  return (
    <Frame label={`X post by ${p.channel.name}`} className={cn('rounded-2xl border', X.frame, X.border)}>
      <XPost {...p} max={p.maxChars || 280} thread={p.comments.length > 0} />
      <SelfThread
        comments={p.comments}
        render={(c, last) => (
          <XPost
            channel={p.channel}
            text={c.text}
            media={c.media}
            max={p.maxChars || 280}
            count={p.count}
            thread={!last}
          />
        )}
      />
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Bluesky

const BSKY = palette('bluesky')

function BlueskyPost({ p, text, media, thread }: { p: PreviewProps; text: string; media: Media[]; thread: boolean }) {
  return (
    <div className="flex gap-2.5 px-4 pt-3">
      <div className="flex flex-col items-center">
        <Avatar channel={p.channel} className="size-[42px]" />
        {thread && <span aria-hidden className={cn('mt-1 w-0.5 flex-1', BSKY.line)} />}
      </div>
      <div className="min-w-0 flex-1 pb-3">
        <p className="flex min-w-0 items-baseline gap-1 text-[15px] leading-5">
          <span className="truncate font-semibold">{p.channel.name}</span>
          <span className={cn('truncate', BSKY.muted)}>{atHandle(p.channel)}</span>
          <span className={cn('shrink-0', BSKY.muted)}>· now</span>
        </p>
        <PostText
          text={text}
          max={p.maxChars || 300}
          accent={BSKY.accent}
          shortLinks
          className="mt-0.5 text-[15px] leading-5"
        />
        <MediaGrid media={media} className={cn('mt-2 rounded-xl border', BSKY.border)} />
        <Actions className={cn('mt-2.5 max-w-80 justify-between', BSKY.muted)}>
          <MessageCircle className="size-[18px]" />
          <Repeat2 className="size-[18px]" />
          <Heart className="size-[18px]" />
          <MoreHorizontal className="size-[18px]" />
        </Actions>
      </div>
    </div>
  )
}

function BlueskyTemplate(p: PreviewProps) {
  return (
    <Frame label={`Bluesky post by ${p.channel.name}`} className={cn('rounded-xl border', BSKY.frame, BSKY.border)}>
      <BlueskyPost p={p} text={p.text} media={p.media} thread={p.comments.length > 0} />
      <SelfThread
        comments={p.comments}
        render={(c, last) => <BlueskyPost p={p} text={c.text} media={c.media} thread={!last} />}
      />
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Threads

const THREADS = palette('threads')

function ThreadsPost({ p, text, media, thread }: { p: PreviewProps; text: string; media: Media[]; thread: boolean }) {
  return (
    <div className="flex gap-3 px-4 pt-3">
      <div className="flex flex-col items-center">
        <Avatar channel={p.channel} className="size-9" />
        {thread && <span aria-hidden className={cn('mt-1.5 w-0.5 flex-1 rounded-full', THREADS.line)} />}
      </div>
      <div className="min-w-0 flex-1 pb-3">
        <p className="flex min-w-0 items-baseline gap-1.5 text-[15px]">
          <span className="truncate font-semibold">{bareHandle(p.channel)}</span>
          <span className={cn('shrink-0', THREADS.muted)}>now</span>
          <MoreHorizontal aria-hidden className="ml-auto size-5 shrink-0 self-center" />
        </p>
        <PostText text={text} max={p.maxChars || 500} accent={THREADS.accent} className="text-[15px] leading-snug" />
        <MediaStripView media={media} className="mt-2" />
        <Actions className="mt-2.5 gap-5">
          <Heart className="size-5" />
          <MessageCircle className="size-5" />
          <Repeat2 className="size-5" />
          <Send className="size-5" />
        </Actions>
      </div>
    </div>
  )
}

function ThreadsTemplate(p: PreviewProps) {
  return (
    <Frame
      label={`Threads post by ${p.channel.name}`}
      className={cn('rounded-2xl border', THREADS.frame, THREADS.border)}
    >
      <ThreadsPost p={p} text={p.text} media={p.media} thread={p.comments.length > 0} />
      <SelfThread
        comments={p.comments}
        render={(c, last) => <ThreadsPost p={p} text={c.text} media={c.media} thread={!last} />}
      />
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Mastodon

const MASTO = palette('mastodon')

function MastodonPost({ p, text, media, thread }: { p: PreviewProps; text: string; media: Media[]; thread: boolean }) {
  return (
    <div className="flex gap-2.5 px-4 pt-3.5">
      <div className="flex flex-col items-center">
        <Avatar channel={p.channel} className="size-[46px] rounded-lg" />
        {thread && <span aria-hidden className={cn('mt-1 w-0.5 flex-1', MASTO.line)} />}
      </div>
      <div className="min-w-0 flex-1 pb-3.5">
        <div className="flex min-w-0 items-start gap-2 text-[15px] leading-tight">
          <div className="min-w-0 flex-1">
            <p className="truncate font-bold">{p.channel.name}</p>
            <p className={cn('truncate text-sm', MASTO.muted)}>{atHandle(p.channel)}</p>
          </div>
          <span className={cn('flex shrink-0 items-center gap-1 text-sm', MASTO.muted)}>
            <Globe className="size-3.5" aria-hidden /> now
          </span>
        </div>
        <PostText
          text={text}
          max={p.maxChars || 500}
          accent={MASTO.accent}
          shortLinks
          className="mt-2 text-[15px] leading-[22px]"
        />
        <MediaGrid media={media} className="mt-2.5 rounded-lg" />
        <Actions className={cn('mt-3 max-w-80 justify-between', MASTO.muted)}>
          <CornerUpLeft className="size-[18px]" />
          <Repeat2 className="size-[18px]" />
          <Star className="size-[18px]" />
          <Bookmark className="size-[18px]" />
          <MoreHorizontal className="size-[18px]" />
        </Actions>
      </div>
    </div>
  )
}

function MastodonTemplate(p: PreviewProps) {
  return (
    <Frame label={`Mastodon post by ${p.channel.name}`} className={cn('rounded-lg border', MASTO.frame, MASTO.border)}>
      <MastodonPost p={p} text={p.text} media={p.media} thread={p.comments.length > 0} />
      <SelfThread
        comments={p.comments}
        render={(c, last) => <MastodonPost p={p} text={c.text} media={c.media} thread={!last} />}
      />
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// LinkedIn

// LinkedIn sets links, hashtags and mentions in semibold blue.
const LI = { ...palette('linkedin'), accent: 'font-semibold text-(--pv-accent)' }

function LinkedInTemplate(p: PreviewProps) {
  const page = p.channel.provider === 'linkedin_page'

  return (
    <Frame label={`LinkedIn post by ${p.channel.name}`} className={cn('rounded-lg border', LI.frame, LI.border)}>
      <div className="flex gap-2 px-4 pt-3">
        <Avatar channel={p.channel} className={cn('size-12', page && 'rounded-sm')} />
        <div className="min-w-0 flex-1 text-sm leading-tight">
          <p className="truncate font-semibold">{p.channel.name}</p>
          {p.channel.handle && <p className={cn('truncate text-xs', LI.muted)}>{p.channel.handle}</p>}
          <p className={cn('flex items-center gap-1 text-xs', LI.muted)}>
            now · <Globe className="size-3" aria-hidden />
          </p>
        </div>
        <MoreHorizontal aria-hidden className={cn('size-5 shrink-0', LI.muted)} />
      </div>
      {/* LinkedIn folds the text after three lines (about 210 characters) behind "...more". */}
      <PostText
        text={p.text}
        max={p.maxChars}
        fold={[210, 3]}
        accent={LI.accent}
        moreClass={cn('font-normal hover:underline', LI.muted)}
        className="px-4 pt-3 pb-2 text-sm leading-5"
      />
      <MediaGrid media={p.media} className="mt-1" min={0.8} max={1.91} />
      <Actions className={cn('mx-4 mt-1 justify-around border-t py-1 text-sm font-semibold', LI.border, LI.muted)}>
        {(
          [
            [ThumbsUp, 'Like'],
            [MessageSquare, 'Comment'],
            [Repeat2, 'Repost'],
            [Send, 'Send'],
          ] as const
        ).map(([Icon, label]) => (
          <span key={label} className="flex items-center gap-1.5 px-2 py-2.5">
            <Icon className="size-5" />
            <span className="max-[400px]:sr-only">{label}</span>
          </span>
        ))}
      </Actions>
      {p.comments.length > 0 && (
        <ul className="grid gap-3 px-4 pt-1 pb-4" aria-label="Comments">
          {p.comments.map((c) => (
            <li key={c.key} className="flex gap-2">
              <Avatar channel={p.channel} className={cn('size-8', page && 'rounded-sm')} />
              <div className={cn('min-w-0 flex-1 rounded-lg rounded-tl-none px-3 py-2 text-sm', LI.bubble)}>
                <p className="flex items-center gap-1.5 text-xs font-semibold">
                  {p.channel.name}
                  <span className="rounded-sm bg-(--pv-muted) px-1 text-[10px] font-semibold text-(--pv-bg)">
                    Author
                  </span>
                </p>
                <PostText text={c.text} accent={LI.accent} className="mt-1 leading-5" />
                <MediaGrid media={c.media} className="mt-2 rounded-md" />
              </div>
            </li>
          ))}
        </ul>
      )}
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Facebook

const FB = palette('facebook')

function FacebookTemplate(p: PreviewProps) {
  // A short status with no pictures shows in large type on Facebook.
  const big = !p.media.length && [...p.text].length < 86 && !p.text.includes('\n')

  return (
    <Frame
      label={`Facebook post by ${p.channel.name}`}
      className={cn('rounded-lg border shadow-sm', FB.frame, FB.border)}
    >
      <div className="flex gap-2 px-4 pt-3">
        <Avatar channel={p.channel} />
        <div className="min-w-0 flex-1 leading-tight">
          <p className="truncate text-[15px] font-semibold">{p.channel.name}</p>
          <p className={cn('flex items-center gap-1 text-[13px]', FB.muted)}>
            Just now · <Globe className="size-3" aria-hidden />
          </p>
        </div>
        <MoreHorizontal aria-hidden className={cn('size-5 shrink-0', FB.muted)} />
      </div>
      <PostText
        text={p.text}
        max={p.maxChars}
        fold={[480, 5]}
        more="See more"
        accent={FB.accent}
        moreClass="font-semibold hover:underline"
        className={cn('px-4 pt-2 pb-3', big ? 'text-2xl leading-8' : 'text-[15px] leading-5')}
      />
      <MediaGrid media={p.media} more min={0.8} max={1.91} />
      <Actions className={cn('mx-4 mt-2 justify-around border-t py-1 text-[15px] font-semibold', FB.border, FB.muted)}>
        {(
          [
            [ThumbsUp, 'Like'],
            [MessageCircle, 'Comment'],
            [Share, 'Share'],
          ] as const
        ).map(([Icon, label]) => (
          <span key={label} className="flex items-center gap-1.5 px-2 py-1.5">
            <Icon className="size-5" />
            {label}
          </span>
        ))}
      </Actions>
      {p.comments.length > 0 && (
        <ul className={cn('grid gap-2 border-t px-4 py-3', FB.border)} aria-label="Comments">
          {p.comments.map((c) => (
            <li key={c.key} className="flex gap-2">
              <Avatar channel={p.channel} className="size-8" />
              <div className="min-w-0">
                <div className={cn('rounded-2xl px-3 py-2 text-[15px]', FB.bubble)}>
                  <p className="text-[13px] font-semibold">{p.channel.name}</p>
                  <PostText text={c.text} accent={FB.accent} className="leading-5" />
                </div>
                <MediaGrid media={c.media} className="mt-1 max-w-60 rounded-2xl" />
              </div>
            </li>
          ))}
        </ul>
      )}
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Instagram

const IG = palette('instagram')

function NeedsMedia({ what, className }: { what: string; className?: string }) {
  return (
    <div className={cn('grid aspect-square place-items-center p-6 text-center text-sm', className)}>
      <p>{what}</p>
    </div>
  )
}

function InstagramTemplate(p: PreviewProps) {
  const handle = bareHandle(p.channel)

  return (
    <Frame label={`Instagram post by ${handle}`} className={cn('rounded-lg border', IG.frame, IG.border)}>
      <div className="flex items-center gap-2.5 px-3 py-2.5">
        <Avatar channel={p.channel} className="size-8" />
        <p className="min-w-0 flex-1 truncate text-sm font-semibold">{handle}</p>
        <MoreHorizontal aria-hidden className="size-5 shrink-0" />
      </div>
      {p.media.length ? (
        <Carousel media={p.media} ratio={ratioOf(p.media[0], 0.8, 1.91, 1)} dotClass="bg-(--pv-line)" />
      ) : (
        <NeedsMedia what="Instagram posts need a picture or a video." className={IG.bubble} />
      )}
      <Actions className="gap-4 px-3 pt-3">
        <Heart className="size-6" />
        <MessageCircle className="size-6 -scale-x-100" />
        <Send className="size-6" />
        <Bookmark className="ml-auto size-6" />
      </Actions>
      {/* The caption folds after about 125 characters (two lines) behind "more". */}
      <PostText
        text={p.text}
        max={p.maxChars}
        fold={[125, 2]}
        more="... more"
        accent={IG.accent}
        moreClass={IG.muted}
        lead={<span className="mr-1.5 font-semibold">{handle}</span>}
        className="px-3 pt-2.5 text-sm leading-[18px]"
      />
      {p.comments.length > 0 && (
        <ul className="grid gap-1 px-3 pt-1.5 text-sm leading-[18px]" aria-label="Comments">
          {p.comments.map((c) => (
            <li key={c.key}>
              <PostText
                text={c.text}
                accent={IG.accent}
                lead={<span className="mr-1.5 font-semibold">{handle}</span>}
              />
            </li>
          ))}
        </ul>
      )}
      <p className={cn('px-3 pt-2 pb-3 text-xs', IG.muted)}>Just now</p>
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// TikTok (always dark: the video plays full screen)

function TikTokTemplate(p: PreviewProps) {
  const handle = bareHandle(p.channel)
  const video = p.media.find(isVideo)
  const photos = p.media.filter((m) => !isVideo(m))

  return (
    <Frame
      label={`TikTok post by ${handle}`}
      className="relative mx-auto aspect-[9/16] max-h-[min(36rem,75svh)] w-full max-w-80 rounded-2xl bg-black text-white"
    >
      <div className="absolute inset-0">
        {video ? (
          <MediaItem media={video} />
        ) : photos.length ? (
          <Carousel media={photos} ratio={9 / 16} dotsInside dotClass="bg-white/50" activeDotClass="bg-white" />
        ) : (
          <NeedsMedia what="TikTok posts need a video or pictures." className="aspect-auto h-full" />
        )}
      </div>
      {/* The right rail: the account and the actions. */}
      <Actions className="absolute right-2 bottom-24 flex-col gap-5">
        <Avatar channel={p.channel} className="size-11 ring-2 ring-white" />
        <Heart className="size-8 fill-white" />
        <MessageCircle className="size-8 fill-white" />
        <Bookmark className="size-8 fill-white" />
        <Share className="size-8" />
      </Actions>
      <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/60 to-transparent px-3 pt-12 pb-4 pr-16">
        <p className="text-[15px] font-semibold">{handle}</p>
        <PostText
          text={p.text}
          max={p.maxChars}
          fold={[80, 2]}
          more="more"
          accent="font-semibold"
          moreClass="font-semibold"
          className="mt-1 text-sm leading-[18px]"
        />
        <p aria-hidden className="mt-2 flex items-center gap-1.5 text-xs">
          <Music2 className="size-3.5" /> original sound - {handle}
        </p>
      </div>
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// YouTube

const YT = palette('youtube')

function YouTubeTemplate(p: PreviewProps) {
  const video = p.media.find(isVideo)
  // The title is the channel's own setting; until it is filled in, the first line stands in for it.
  const title = p.settings.title?.trim() || p.text.split('\n')[0]?.slice(0, 100) || 'Untitled video'

  return (
    <Frame label={`YouTube video by ${p.channel.name}`} className={cn('rounded-xl pb-3', YT.frame)}>
      <div className="aspect-video overflow-hidden rounded-xl bg-black text-white">
        {video ? (
          <MediaItem media={video} />
        ) : (
          <NeedsMedia what="YouTube needs a video." className="aspect-auto h-full" />
        )}
      </div>
      <div className="flex gap-3 px-1 pt-3">
        <Avatar channel={p.channel} className="size-9" />
        <div className="min-w-0 flex-1">
          <p className="line-clamp-2 text-base leading-snug font-semibold wrap-anywhere" title={title}>
            {title}
          </p>
          <p className={cn('mt-0.5 text-sm', YT.muted)}>{p.channel.name} · No views · Just now</p>
        </div>
      </div>
      <div className={cn('mx-1 mt-3 rounded-xl p-3', YT.box)}>
        <PostText
          text={p.text}
          max={p.maxChars}
          fold={[160, 3]}
          more="...more"
          accent={YT.accent}
          moreClass="font-semibold"
          className="text-sm leading-5"
        />
      </div>
      {p.comments.length > 0 && (
        // As under a video on youtube.com: the count with Sort by, then each comment with the channel's handle on
        // the grey pill YouTube gives the video's own channel, the time, and the like, dislike and Reply row.
        <section aria-label="Comments" className="px-1 pt-5 pb-2">
          <p className="flex items-center gap-8">
            <span className="text-xl leading-7 font-bold">
              {p.comments.length} {p.comments.length === 1 ? 'Comment' : 'Comments'}
            </span>
            <span className="flex items-center gap-2 text-sm font-medium">
              <ListFilter aria-hidden className="size-5" /> Sort by
            </span>
          </p>
          <ul className="mt-6 grid gap-4">
            {p.comments.map((c) => (
              <li key={c.key} className="flex gap-4">
                <Avatar channel={p.channel} className="size-10" />
                <div className="min-w-0 flex-1">
                  <p className="flex flex-wrap items-center gap-1 text-xs leading-[18px]">
                    <span className={cn('rounded-full px-1.5 font-medium', YT.bubble)}>{atHandle(p.channel)}</span>
                    <span className={YT.muted}>Just now</span>
                  </p>
                  <PostText text={c.text} accent={YT.accent} className="mt-0.5 text-sm leading-5" />
                  <p className="mt-1 -ml-2 flex items-center text-xs font-medium">
                    <span className="grid size-8 place-items-center">
                      <ThumbsUp aria-hidden className="size-4" />
                    </span>
                    <span className="grid size-8 place-items-center">
                      <ThumbsDown aria-hidden className="size-4" />
                    </span>
                    <span className="ml-1 rounded-full px-3 py-2">Reply</span>
                  </p>
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Slack

const SLACK = palette('slack')

function SlackMessage({ p, text, media }: { p: PreviewProps; text: string; media: Media[] }) {
  const time = useClockLabel()

  return (
    <div className="flex gap-2 px-4 py-2">
      <Avatar channel={p.channel} className="size-9 rounded-md" />
      <div className="min-w-0 flex-1">
        <p className="flex items-baseline gap-2 text-[15px]">
          <span className="truncate font-bold">{p.channel.name}</span>
          <span className={cn('shrink-0 text-xs', SLACK.muted)}>{time}</span>
        </p>
        <PostText text={text} max={p.maxChars} accent={SLACK.accent} className="text-[15px] leading-[22px]" />
        <MediaGrid media={media} className="mt-2 max-w-sm rounded-lg" />
      </div>
    </div>
  )
}

function SlackTemplate(p: PreviewProps) {
  return (
    <Frame
      label={`Slack message from ${p.channel.name}`}
      className={cn('rounded-lg border py-2', SLACK.frame, SLACK.border)}
    >
      <SlackMessage p={p} text={p.text} media={p.media} />
      {p.comments.length > 0 && (
        <>
          <p className={cn('flex items-center gap-2 pr-4 pb-1 pl-[60px] text-[13px] font-bold', SLACK.accent)}>
            <Avatar channel={p.channel} className="size-6 rounded" />
            {p.comments.length} {p.comments.length === 1 ? 'reply' : 'replies'}
          </p>
          <section aria-label="Thread" className={cn('mt-1 border-t pt-1', SLACK.border)}>
            {p.comments.map((c) => (
              <SlackMessage key={c.key} p={p} text={c.text} media={c.media} />
            ))}
          </section>
        </>
      )}
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Discord

const DISCORD = palette('discord')

function DiscordMessage({
  p,
  text,
  media,
  replyTo,
}: {
  p: PreviewProps
  text: string
  media: Media[]
  replyTo?: string
}) {
  const time = useClockLabel()

  return (
    <div className="px-4 py-1.5">
      {replyTo != null && (
        <p aria-hidden className={cn('ml-[18px] flex min-w-0 items-center gap-1 text-[13px]', DISCORD.muted)}>
          <span className="mt-2 h-2 w-8 shrink-0 rounded-tl-md border-t-2 border-l-2 border-(--pv-line)" />
          <Avatar channel={p.channel} className="size-4" />
          <span className={cn('shrink-0 font-semibold', DISCORD.name)}>{p.channel.name}</span>
          <span className="truncate">{replyTo || 'Click to see attachment'}</span>
        </p>
      )}
      <div className="flex gap-4">
        <Avatar channel={p.channel} />
        <div className="min-w-0 flex-1">
          <p className="flex items-baseline gap-2">
            <span className={cn('truncate font-medium', DISCORD.name)}>{p.channel.name}</span>
            <span className={cn('shrink-0 text-xs', DISCORD.muted)}>Today at {time}</span>
          </p>
          <PostText text={text} max={p.maxChars} accent={DISCORD.accent} className="text-base leading-[22px]" />
          <MediaGrid media={media} className="mt-1.5 max-w-sm rounded-lg" />
        </div>
      </div>
    </div>
  )
}

function DiscordTemplate(p: PreviewProps) {
  const parts = [p.text, ...p.comments.map((c) => c.text)]

  return (
    <Frame label={`Discord message from ${p.channel.name}`} className={cn('rounded-lg py-2', DISCORD.frame)}>
      <DiscordMessage p={p} text={p.text} media={p.media} />
      {p.comments.map((c, i) => (
        <DiscordMessage key={c.key} p={p} text={c.text} media={c.media} replyTo={parts[i]!.split('\n')[0]} />
      ))}
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Telegram

const TG = palette('telegram')

function TelegramBubble({ p, text, media, quote }: { p: PreviewProps; text: string; media: Media[]; quote?: string }) {
  const time = useClockLabel()

  return (
    <div className={cn('max-w-[min(100%,28rem)] overflow-hidden rounded-xl shadow-sm', TG.bubble)}>
      {' '}
      <MediaGrid media={media} min={0.6} max={1.91} />
      <div className="px-2.5 pt-1.5 pb-1">
        {quote != null && (
          <p className="mb-1 rounded border-l-[3px] border-(--pv-accent) bg-(--pv-line) px-2 py-0.5 text-[13px]">
            <span className={cn('block font-semibold', TG.accent)}>{p.channel.name}</span>
            <span className="line-clamp-1">{quote || 'Photo'}</span>
          </p>
        )}
        <PostText text={text} max={p.maxChars} accent={TG.accent} className="text-[15px] leading-5" />
        <p className={cn('flex items-center justify-end gap-1 text-xs', TG.muted)}>
          <Eye className="size-3.5" aria-hidden /> 1 · {time}
        </p>
      </div>
    </div>
  )
}

function TelegramTemplate(p: PreviewProps) {
  const parts = [p.text, ...p.comments.map((c) => c.text)]

  return (
    <Frame label={`Telegram message in ${p.channel.name}`} className={cn('rounded-xl', TG.frame)}>
      <div className={cn('flex items-center gap-3 px-3 py-2', TG.bar)}>
        <Avatar channel={p.channel} className="size-9" />
        <div className="min-w-0 leading-tight">
          <p className="truncate font-semibold">{p.channel.name}</p>
          <p className={cn('text-[13px]', TG.muted)}>channel</p>
        </div>
      </div>
      <div className={cn('grid gap-1.5 p-3', TG.chat)}>
        <TelegramBubble p={p} text={p.text} media={p.media} />
        {p.comments.map((c, i) => (
          <TelegramBubble key={c.key} p={p} text={c.text} media={c.media} quote={parts[i]!.split('\n')[0]} />
        ))}
      </div>
    </Frame>
  )
}

// ---------------------------------------------------------------------------------------------------------------
// Every other platform, and All channels

/** A neutral card in the dashboard's own look: the platform's mark in the corner names where it goes. */
export function GenericCard({
  header,
  mark,
  label,
  text,
  media,
  comments,
  max = 0,
  count,
  commentAuthor,
}: {
  header: React.ReactNode
  mark?: React.ReactNode
  label: string
  text: string
  media: Media[]
  comments: PreviewComment[]
  max?: number
  count?: (t: string) => number
  commentAuthor?: Channel
}) {
  return (
    <Frame label={label} className="rounded-2xl border bg-card p-4 text-card-foreground shadow-xs">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">{header}</div>
        {mark}
      </div>
      {text.trim() ? (
        <PostText
          text={text}
          max={max}
          count={count}
          accent="text-brand-strong"
          className="mt-3 text-[15px] leading-relaxed"
        />
      ) : (
        <p className="mt-3 text-[15px] text-muted-foreground italic">No text yet</p>
      )}
      <MediaGrid media={media} className="mt-3 rounded-xl" />
      {comments.length > 0 && (
        <ol className="mt-4 grid gap-3 border-l-2 pl-4" aria-label="Comments under the post">
          {comments.map((c, i) => (
            <li key={c.key} className="text-sm">
              <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                {commentAuthor && <ChannelAvatar channel={commentAuthor} className="size-4" />}
                Comment {i + 1}
              </p>
              <PostText text={c.text} accent="text-brand-strong" className="mt-1 leading-relaxed" />
              <MediaGrid media={c.media} className="mt-2 max-w-xs rounded-lg" />
            </li>
          ))}
        </ol>
      )}
    </Frame>
  )
}

function GenericTemplate(p: PreviewProps) {
  return (
    <GenericCard
      label={`${p.providerName} post by ${p.channel.name}`}
      header={
        <div className="flex min-w-0 items-center gap-3">
          <ChannelAvatar channel={p.channel} className="size-10" />
          <div className="min-w-0 leading-tight">
            <p className="truncate font-semibold">{p.channel.name}</p>
            <p className="truncate text-sm text-muted-foreground">
              {[p.channel.handle, p.providerName].filter(Boolean).join(' · ')}
            </p>
          </div>
        </div>
      }
      mark={<ProviderIcon id={p.channel.provider} name={p.providerName} className="size-5" />}
      text={p.settings.title?.trim() ? `${p.settings.title.trim()}\n\n${p.text}` : p.text}
      media={p.media}
      comments={p.comments}
      max={p.maxChars}
      count={p.count}
      commentAuthor={p.channel}
    />
  )
}

/** The neutral preview for All channels: the shared text, media and comments, under every picked channel. */
export function AllChannelsCard({
  channels,
  text,
  media,
  comments,
}: {
  channels: Channel[]
  text: string
  media: Media[]
  comments: PreviewComment[]
}) {
  return (
    <GenericCard
      label="Post for all channels"
      header={
        <div className="flex min-w-0 items-center gap-3">
          {channels.length > 0 && <ChannelStack channels={channels} max={4} size="size-8" />}
          <div className="min-w-0 leading-tight">
            <p className="truncate font-semibold">All channels</p>
            <p className="text-sm text-muted-foreground">
              {channels.length
                ? `The shared text, for ${channels.length} channel${channels.length === 1 ? '' : 's'}`
                : 'No channels picked yet'}
            </p>
          </div>
        </div>
      }
      text={text}
      media={media}
      comments={comments}
    />
  )
}

/** One channel's post as its platform shows it; a platform without its own look gets the neutral card with its mark. */
export function PlatformPreview(p: PreviewProps) {
  return (
    <>
      <style>{PALETTE_CSS}</style>
      <Template {...p} />
    </>
  )
}

function Template(p: PreviewProps) {
  switch (p.channel.provider) {
    case 'x':
      return <XTemplate {...p} />
    case 'bluesky':
      return <BlueskyTemplate {...p} />
    case 'threads':
      return <ThreadsTemplate {...p} />
    case 'mastodon':
      return <MastodonTemplate {...p} />
    case 'linkedin':
    case 'linkedin_page':
      return <LinkedInTemplate {...p} />
    case 'facebook':
      return <FacebookTemplate {...p} />
    case 'instagram':
    case 'instagram_business':
      return <InstagramTemplate {...p} />
    case 'tiktok':
      return <TikTokTemplate {...p} />
    case 'youtube':
      return <YouTubeTemplate {...p} />
    case 'slack':
      return <SlackTemplate {...p} />
    case 'discord':
    case 'discord_webhook':
      return <DiscordTemplate {...p} />
    case 'telegram':
      return <TelegramTemplate {...p} />
    default:
      return <GenericTemplate {...p} />
  }
}
