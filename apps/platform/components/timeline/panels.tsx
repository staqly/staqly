'use client'

import { useInfiniteQuery } from '@tanstack/react-query'
import { AudioWaveform, Captions, Copy, Film, Loader2, Music, Scissors, Search, Trash2, Upload } from 'lucide-react'
import { useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { ColorInput } from '@/components/common/color-input'
import { NativeSelect } from '@/components/common/native-select'
import { FONTS } from '@/components/design/fonts'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, errorMessage, mediaThumbUrl, mediaUrl, type Media, type MediaLibraryItem } from '@/lib/api'
import { cn } from '@/lib/utils'

import { beats, decode, mix } from './audio'
import {
  clipAt,
  clipStarts,
  newCaption,
  newClip,
  newSound,
  splitClip,
  splitSound,
  uid,
  VIDEO_SIZES,
  type Caption,
  type Clip,
  type Fit,
  type Motion,
  type Sound,
  type Timeline,
  type Transition,
} from './doc'
import type { Selection } from './tracks'

type Update = (fn: (t: Timeline) => void, coalesce?: string) => void

// Keep the lazy import outside the component so the React Compiler can memoise it.
const loadSpeech = () => import('./speech')

export interface Ctx {
  doc: Timeline
  update: Update
  time: number
  sel: Selection
  onSelect: (s: Selection) => void
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
      {children}
    </section>
  )
}

const oneDecimal = (v: number) => v.toFixed(1)

function Range({
  label,
  value,
  min,
  max,
  step = 0.1,
  format = oneDecimal,
  onChange,
}: {
  label: string
  value: number
  min: number
  max: number
  step?: number
  format?: (v: number) => string
  onChange: (v: number) => void
}) {
  return (
    <label className="flex flex-col gap-1 text-xs">
      <span className="flex justify-between text-muted-foreground">
        {label} <span className="text-foreground tabular-nums">{format(value)}</span>
      </span>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(+e.target.value)}
        className="accent-[var(--brand)]"
      />
    </label>
  )
}

/** A row of choices, one pressed. */
function Choice<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (v: T) => void
}) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div role="group" aria-label={label} className="flex flex-wrap gap-1">
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            aria-pressed={o.value === value}
            onClick={() => onChange(o.value)}
            className="rounded-md border px-2 py-1 text-xs hover:bg-muted aria-pressed:border-brand aria-pressed:bg-brand-soft aria-pressed:text-brand"
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  )
}

/** A library file as the timeline takes it: a clip for a picture or a video, a sound clip for audio. */
export function addFromLibrary(
  ctx: Ctx,
  m: Pick<Media, 'id' | 'mime_type' | 'original_name' | 'width' | 'height' | 'duration_ms'>,
) {
  const name = m.original_name.replace(/\.[a-z0-9]+$/i, '')
  const src = mediaUrl(m.id)
  const seconds = (m.duration_ms || 0) / 1000

  if (m.mime_type.startsWith('audio/') || ctx.doc.kind === 'audio') {
    if (!seconds) {
      toast.error(`${m.original_name} has no length yet`, { description: 'Open it in Media once, then add it again.' })

      return
    }

    // A sound editor lines sounds up one after another; a video puts music where the playhead is.
    const at =
      ctx.doc.kind === 'audio' ? ctx.doc.sounds.reduce((n, s) => Math.max(n, s.start + s.duration), 0) : ctx.time
    const s = newSound({ id: m.id, src, name, duration: seconds }, at)

    ctx.update((t) => void t.sounds.push(s))
    ctx.onSelect({ kind: 'sound', id: s.id })

    return
  }

  const c = newClip({
    id: m.id,
    kind: m.mime_type.startsWith('video/') ? 'video' : 'image',
    src,
    name,
    width: m.width,
    height: m.height,
    duration: seconds || undefined,
  })

  ctx.update((t) => void t.clips.push(c))
  ctx.onSelect({ kind: 'clip', id: c.id })
}

/**
 * Cuts on the music's beat: every picture lasts a whole number of beats (about two and a half seconds), and the
 * music's lead-in is trimmed so a beat falls where it starts. Video clips keep their length.
 */
export async function fitToBeat(ctx: Ctx, s: Sound) {
  const buf = await decode(s.src)

  if (!buf) {
    throw new Error('This sound could not be read')
  }

  const b = beats(buf.getChannelData(0), buf.sampleRate)
  const per = Math.max(1, Math.round(2.5 / b.period))
  // The first beat at or after where the sound starts in its file.
  const lead = (((b.first - s.in) % b.period) + b.period) % b.period

  ctx.update((t) => {
    for (const c of t.clips) {
      if (c.kind === 'image') {
        c.duration = per * b.period
      }
    }

    const k = t.sounds.find((x) => x.id === s.id)

    if (k) {
      k.in += lead
      k.duration = Math.max(0.5, Math.min(k.fileDuration - k.in, k.duration))
    }
  })

  return { bpm: Math.round(b.bpm), per }
}

function BeatButton({ ctx, s }: { ctx: Ctx; s: Sound }) {
  const [busy, setBusy] = useState(false)

  if (ctx.doc.kind !== 'video' || !ctx.doc.clips.some((c) => c.kind === 'image')) {
    return null
  }

  return (
    <Button
      variant="outline"
      size="sm"
      disabled={busy}
      title="Every picture lasts a whole number of beats"
      onClick={() => {
        setBusy(true)
        void fitToBeat(ctx, s)
          .then(({ bpm, per }) =>
            toast.success('Pictures fitted to the beat', {
              description: `${bpm} beats a minute: a new picture every ${per} ${per === 1 ? 'beat' : 'beats'}.`,
            }),
          )
          .catch((e: unknown) => toast.error('Could not find the beat', { description: errorMessage(e) }))
          .finally(() => setBusy(false))
      }}
    >
      {busy ? <Loader2 className="animate-spin" /> : <AudioWaveform />} Fit pictures to the beat
    </Button>
  )
}

/** Captions for everything said on the timeline (music, voice-over, videos' own sound), made on the device. */
function SpeechButton({ ctx }: { ctx: Ctx }) {
  const [busy, setBusy] = useState<string | null>(null)
  const sounds = ctx.doc.sounds.length > 0 || ctx.doc.clips.some((c) => c.kind === 'video' && c.volume > 0)

  return (
    <div className="flex flex-col gap-1.5">
      <Button
        variant="secondary"
        className="justify-start"
        disabled={!!busy || !sounds}
        onClick={() => {
          setBusy('Getting the speech model ready')
          void Promise.all([mix(ctx.doc), loadSpeech()])
            .then(([buf, speech]) =>
              speech
                .transcribe(buf, (share) => setBusy(`Downloading the speech model ${Math.round(share * 100)}%`))
                .then((pieces) => ({ pieces: speech.splitSpoken(pieces), speech })),
            )
            .then(({ pieces }) => {
              if (!pieces.length) {
                toast.info('No speech found', { description: 'Captions come from voices in the sound or the videos.' })

                return
              }

              const made = pieces.map((p) => ({
                ...newCaption(ctx.doc, p.start, p.text),
                end: Math.max(p.start + 0.4, p.end),
              }))

              ctx.update((t) => void t.captions.push(...made))
              toast.success(`${made.length} captions added`, {
                description: 'Check the words: select one to change it.',
              })
            })
            .catch((e: unknown) => toast.error('Could not make captions', { description: errorMessage(e) }))
            .finally(() => setBusy(null))
        }}
      >
        {/* No spinner while it listens: an animation redrawn on a slow or software graphics card starves the model. */}
        <Captions className={busy ? 'text-brand' : undefined} /> Captions from speech
      </Button>
      <p className="text-xs text-muted-foreground">
        {busy ??
          (sounds
            ? 'Listens on this device; the first time downloads a model (about 80 MB).'
            : 'Add a voice or a video with sound first.')}
      </p>
    </div>
  )
}

/** The library's files this editor takes: pictures and videos for a video (and sounds), sounds for a sound. */
function Library({ ctx, kind }: { ctx: Ctx; kind: 'image' | 'video' | 'audio' }) {
  const [q, setQ] = useState('')
  const list = useInfiniteQuery({
    queryKey: ['timeline-library', kind, q],
    queryFn: ({ pageParam }) => api.mediaLibrary({ kind, q: q || undefined, limit: 60, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (p) => p.next_offset || undefined,
  })
  const items = list.data?.pages.flatMap((p) => p.data) ?? []

  return (
    <div className="flex flex-col gap-2">
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search your library"
          aria-label={`Search ${kind === 'audio' ? 'sounds' : kind === 'video' ? 'videos' : 'pictures'}`}
          className="h-8 pl-8"
        />
      </div>
      {list.isPending ? (
        <Loader2 className="mx-auto animate-spin text-muted-foreground" />
      ) : !items.length ? (
        <p className="text-xs text-muted-foreground">Nothing here yet: upload some above.</p>
      ) : kind === 'audio' ? (
        <ul className="flex flex-col gap-1">
          {items.map((m) => (
            <li key={m.id}>
              <button
                type="button"
                onClick={() => addFromLibrary(ctx, m)}
                className="flex w-full items-center gap-2 rounded-md border px-2 py-1.5 text-left text-sm hover:bg-muted"
              >
                <Music className="size-4 shrink-0 text-emerald-600" />
                <span className="min-w-0 flex-1 truncate">{m.original_name}</span>
                <span className="text-xs text-muted-foreground tabular-nums">
                  {m.duration_ms ? `${Math.round(m.duration_ms / 1000)} s` : ''}
                </span>
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <div className="grid grid-cols-3 gap-1.5">
          {items.map((m: MediaLibraryItem) => (
            <button
              key={m.id}
              type="button"
              aria-label={`Add ${m.original_name}`}
              title={m.original_name}
              onClick={() => addFromLibrary(ctx, m)}
              className="relative aspect-square overflow-hidden rounded-md border bg-muted hover:ring-2 hover:ring-brand"
            >
              {kind === 'image' ? (
                <img src={mediaThumbUrl(m.id, 160)} alt="" className="size-full object-cover" draggable={false} />
              ) : (
                <>
                  <video src={`${mediaUrl(m.id)}#t=0.5`} preload="metadata" muted className="size-full object-cover" />
                  <Film className="absolute bottom-1 left-1 size-3.5 text-white drop-shadow" />
                </>
              )}
            </button>
          ))}
        </div>
      )}
      {list.hasNextPage && (
        <Button
          variant="outline"
          size="sm"
          onClick={() => void list.fetchNextPage()}
          disabled={list.isFetchingNextPage}
        >
          Show more
        </Button>
      )}
    </div>
  )
}

function UploadButton({ ctx, accept }: { ctx: Ctx; accept: string }) {
  const input = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)

  return (
    <>
      <input
        ref={input}
        type="file"
        accept={accept}
        multiple
        hidden
        onChange={(e) => {
          const files = [...(e.target.files ?? [])]

          e.target.value = ''
          setBusy(true)
          // One after another, in the order picked.
          void files
            .reduce<Promise<void>>(
              (p, f) =>
                p.then(() =>
                  api
                    .uploadMedia(f, f.name)
                    .then((m) => addFromLibrary(ctx, m))
                    .catch(
                      (err: unknown) => void toast.error(`Could not add ${f.name}`, { description: errorMessage(err) }),
                    ),
                ),
              Promise.resolve(),
            )
            .finally(() => setBusy(false))
        }}
      />
      <Button onClick={() => input.current?.click()} disabled={busy}>
        {busy ? <Loader2 className="animate-spin" /> : <Upload />} Upload
      </Button>
    </>
  )
}

const CAPTION_STYLES: { label: string; make: (c: Caption) => Caption }[] = [
  { label: 'Subtitle', make: (c) => c },
  {
    label: 'Title',
    make: (c) => ({ ...c, text: 'A big title', size: 0.08, y: 0.45, box: '', font: 'Anton', bold: false }),
  },
  { label: 'Label', make: (c) => ({ ...c, text: 'NEW', size: 0.04, y: 0.12, box: '#facc15', color: '#111827' }) },
  { label: 'Handwritten', make: (c) => ({ ...c, text: 'Hello there', size: 0.07, y: 0.5, box: '', font: 'Caveat' }) },
]

export type AddPanel = 'media' | 'audio' | 'text' | 'size'

/** The left panel: what to put on the timeline. */
export function AddPanelBody({ ctx, panel }: { ctx: Ctx; panel: AddPanel }) {
  const video = ctx.doc.kind === 'video'

  switch (panel) {
    case 'media':
      return (
        <div className="flex flex-col gap-4">
          <UploadButton
            ctx={ctx}
            accept="image/png,image/jpeg,image/webp,image/gif,video/mp4,video/webm,video/quicktime"
          />
          <Section title="Pictures">
            <Library ctx={ctx} kind="image" />
          </Section>
          <Section title="Videos">
            <Library ctx={ctx} kind="video" />
          </Section>
        </div>
      )
    case 'audio':
      return (
        <div className="flex flex-col gap-4">
          <UploadButton ctx={ctx} accept="audio/mpeg,audio/mp4,audio/wav,audio/x-wav,audio/ogg,audio/webm,audio/aac" />
          <p className="text-xs text-muted-foreground">
            {video
              ? 'A sound starts at the playhead. Drag it on the timeline to move it.'
              : 'Sounds line up one after another.'}
          </p>
          {video && ctx.doc.sounds[0] && <BeatButton ctx={ctx} s={ctx.doc.sounds[0]} />}
          <Library ctx={ctx} kind="audio" />
        </div>
      )
    case 'text':
      return (
        <div className="flex flex-col gap-2">
          <SpeechButton ctx={ctx} />
          <p className="pt-2 text-xs text-muted-foreground">A caption starts at the playhead, for two seconds.</p>
          {CAPTION_STYLES.map((s) => (
            <Button
              key={s.label}
              variant="secondary"
              className="justify-start"
              onClick={() => {
                const c = s.make(newCaption(ctx.doc, ctx.time))

                ctx.update((t) => void t.captions.push(c))
                ctx.onSelect({ kind: 'caption', id: c.id })
              }}
            >
              {s.label}
            </Button>
          ))}
        </div>
      )
    case 'size':
      return (
        <div className="flex flex-col gap-4">
          <Section title="Frame">
            {VIDEO_SIZES.map((s) => (
              <button
                key={s.label}
                type="button"
                aria-pressed={ctx.doc.width === s.width && ctx.doc.height === s.height}
                onClick={() =>
                  ctx.update((t) => {
                    t.width = s.width
                    t.height = s.height
                  })
                }
                className="flex items-center justify-between rounded-md border px-3 py-2 text-left text-sm hover:bg-muted aria-pressed:border-brand aria-pressed:bg-brand-soft"
              >
                {s.label}
                <span className="text-xs text-muted-foreground tabular-nums">
                  {s.width} x {s.height}
                </span>
              </button>
            ))}
          </Section>
          <Section title="Behind pictures that don't fill it">
            <div className="flex flex-wrap items-center gap-1.5">
              {['#000000', '#ffffff', '#111827', '#f3f4f6', '#1f8a4c'].map((c) => (
                <button
                  key={c}
                  type="button"
                  aria-label={`Background ${c}`}
                  aria-pressed={ctx.doc.background.toLowerCase() === c}
                  onClick={() => ctx.update((t) => void (t.background = c))}
                  className={cn(
                    'size-8 rounded-md border shadow-xs pointer-coarse:size-11',
                    ctx.doc.background.toLowerCase() === c && 'ring-2 ring-brand ring-offset-1 ring-offset-background',
                  )}
                  style={{ background: c }}
                />
              ))}
              <ColorInput
                label="Background colour"
                value={ctx.doc.background}
                onChange={(c) => ctx.update((t) => void (t.background = c), 'bg')}
              />
              <span className="ml-1 font-mono text-xs text-muted-foreground uppercase">{ctx.doc.background}</span>
            </div>
          </Section>
        </div>
      )
  }
}

const MOTIONS: { value: Motion; label: string }[] = [
  { value: 'none', label: 'Still' },
  { value: 'zoom-in', label: 'Zoom in' },
  { value: 'zoom-out', label: 'Zoom out' },
  { value: 'pan-left', label: 'Pan left' },
  { value: 'pan-right', label: 'Pan right' },
]

const TRANSITIONS: { value: Transition; label: string }[] = [
  { value: 'cut', label: 'Cut' },
  { value: 'fade', label: 'Fade' },
  { value: 'slide', label: 'Slide' },
  { value: 'zoom', label: 'Zoom' },
]

const FITS: { value: Fit; label: string }[] = [
  { value: 'cover', label: 'Fill the frame' },
  { value: 'contain', label: 'Show it whole' },
]

function ClipInspector({ ctx, c }: { ctx: Ctx; c: Clip }) {
  const set = (fn: (k: Clip) => void, key?: string) =>
    ctx.update((t) => {
      const k = t.clips.find((x) => x.id === c.id)

      if (k) {
        fn(k)
      }
    }, key)
  const i = ctx.doc.clips.findIndex((x) => x.id === c.id)
  const at = clipAt(ctx.doc, ctx.time)
  const max = c.kind === 'video' ? Math.max(0.5, (c.fileDuration ?? 60) - c.in) : 30

  return (
    <div className="flex flex-col gap-4">
      <p className="truncate text-sm font-medium">{c.name}</p>
      <Range
        label="On screen for"
        value={c.duration}
        min={0.5}
        max={max}
        format={(v) => `${v.toFixed(1)} s`}
        onChange={(v) => set((k) => void (k.duration = v), 'duration')}
      />
      {c.kind === 'video' && c.fileDuration && (
        <Range
          label="Starts in the video at"
          value={c.in}
          min={0}
          max={Math.max(0, c.fileDuration - 0.5)}
          format={(v) => `${v.toFixed(1)} s`}
          onChange={(v) =>
            set((k) => void ((k.in = v), (k.duration = Math.min(k.duration, k.fileDuration! - v))), 'in')
          }
        />
      )}
      <Choice label="Fit" value={c.fit} options={FITS} onChange={(v) => set((k) => void (k.fit = v))} />
      <Choice label="Movement" value={c.motion} options={MOTIONS} onChange={(v) => set((k) => void (k.motion = v))} />
      {i > 0 && (
        <Choice
          label="Comes in with"
          value={c.transition}
          options={TRANSITIONS}
          onChange={(v) => set((k) => void (k.transition = v))}
        />
      )}
      {c.kind === 'video' && (
        <Range
          label="Its own sound"
          value={c.volume}
          min={0}
          max={1}
          step={0.05}
          format={(v) => (v ? `${Math.round(v * 100)}%` : 'Off')}
          onChange={(v) => set((k) => void (k.volume = v), 'volume')}
        />
      )}
      <div className="mt-1 flex flex-wrap items-center gap-2 border-t pt-4">
        <Button
          variant="outline"
          size="sm"
          disabled={at?.clip.id !== c.id}
          title="Split at the playhead (S)"
          onClick={() =>
            ctx.update((t) => {
              const parts = at && splitClip(c, at.local)

              if (parts) {
                t.clips.splice(i, 1, ...parts)
              }
            })
          }
        >
          <Scissors /> Split
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => ctx.update((t) => void t.clips.splice(i + 1, 0, { ...structuredClone(c), id: uid() }))}
        >
          <Copy /> Duplicate
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto text-red-700 dark:text-red-400"
          onClick={() => {
            ctx.update((t) => void t.clips.splice(i, 1))
            ctx.onSelect(null)
          }}
        >
          <Trash2 /> Delete
        </Button>
      </div>
    </div>
  )
}

function CaptionInspector({ ctx, c }: { ctx: Ctx; c: Caption }) {
  const set = (fn: (k: Caption) => void, key?: string) =>
    ctx.update((t) => {
      const k = t.captions.find((x) => x.id === c.id)

      if (k) {
        fn(k)
      }
    }, key)

  return (
    <div className="flex flex-col gap-4">
      <textarea
        aria-label="Caption text"
        value={c.text}
        rows={3}
        maxLength={300}
        onChange={(e) => set((k) => void (k.text = e.target.value), 'text')}
        className="rounded-md border bg-transparent px-2 py-1.5 text-sm"
      />
      <div className="grid grid-cols-2 gap-2 text-xs">
        <label className="flex flex-col gap-1 text-muted-foreground">
          From (s)
          <Input
            type="number"
            min={0}
            step={0.1}
            value={c.start}
            onChange={(e) => set((k) => void (k.start = Math.max(0, Math.min(k.end - 0.3, +e.target.value))), 'from')}
            className="h-8"
          />
        </label>
        <label className="flex flex-col gap-1 text-muted-foreground">
          To (s)
          <Input
            type="number"
            min={0}
            step={0.1}
            value={c.end}
            onChange={(e) => set((k) => void (k.end = Math.max(k.start + 0.3, +e.target.value)), 'to')}
            className="h-8"
          />
        </label>
      </div>
      <NativeSelect
        aria-label="Font"
        value={c.font}
        style={{ fontFamily: `"${c.font}"` }}
        onChange={(e) => set((k) => void (k.font = e.target.value))}
      >
        {FONTS.map((f) => (
          <option key={f} value={f}>
            {f}
          </option>
        ))}
      </NativeSelect>
      <Range
        label="Size"
        value={c.size}
        min={0.02}
        max={0.15}
        step={0.005}
        format={(v) => `${Math.round(v * 1000) / 10}%`}
        onChange={(v) => set((k) => void (k.size = v), 'size')}
      />
      <Range
        label="Height on the frame"
        value={c.y}
        min={0.05}
        max={0.95}
        step={0.01}
        format={(v) => `${Math.round(v * 100)}%`}
        onChange={(v) => set((k) => void (k.y = v), 'y')}
      />
      <div className="flex items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <input
            type="color"
            aria-label="Text colour"
            value={c.color}
            onChange={(e) => set((k) => void (k.color = e.target.value), 'color')}
          />
          Colour
        </label>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={c.bold}
            onChange={(e) => set((k) => void (k.bold = e.target.checked))}
            className="accent-[var(--brand)]"
          />
          Bold
        </label>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={!!c.box}
            onChange={(e) => set((k) => void (k.box = e.target.checked ? 'rgba(0,0,0,0.55)' : ''))}
            className="accent-[var(--brand)]"
          />
          Box
        </label>
      </div>
      <Button
        variant="outline"
        size="sm"
        className="mt-1 self-end text-red-700 dark:text-red-400"
        onClick={() => {
          ctx.update((t) => void (t.captions = t.captions.filter((x) => x.id !== c.id)))
          ctx.onSelect(null)
        }}
      >
        <Trash2 /> Delete
      </Button>
    </div>
  )
}

function SoundInspector({ ctx, s }: { ctx: Ctx; s: Sound }) {
  const set = (fn: (k: Sound) => void, key?: string) =>
    ctx.update((t) => {
      const k = t.sounds.find((x) => x.id === s.id)

      if (k) {
        fn(k)
      }
    }, key)
  const inside = ctx.time > s.start && ctx.time < s.start + s.duration

  return (
    <div className="flex flex-col gap-4">
      <p className="truncate text-sm font-medium">{s.name}</p>
      <Range
        label="Volume"
        value={s.volume}
        min={0}
        max={1.5}
        step={0.05}
        format={(v) => `${Math.round(v * 100)}%`}
        onChange={(v) => set((k) => void (k.volume = v), 'volume')}
      />
      <Range
        label="Fade in"
        value={s.fadeIn}
        min={0}
        max={Math.min(10, s.duration / 2)}
        format={(v) => `${v.toFixed(1)} s`}
        onChange={(v) => set((k) => void (k.fadeIn = v), 'fadein')}
      />
      <Range
        label="Fade out"
        value={s.fadeOut}
        min={0}
        max={Math.min(10, s.duration / 2)}
        format={(v) => `${v.toFixed(1)} s`}
        onChange={(v) => set((k) => void (k.fadeOut = v), 'fadeout')}
      />
      <Range
        label="Starts at"
        value={s.start}
        min={0}
        max={Math.max(60, s.start + 10)}
        format={(v) => `${v.toFixed(1)} s`}
        onChange={(v) => set((k) => void (k.start = v), 'start')}
      />
      <BeatButton ctx={ctx} s={s} />
      <div className="mt-1 flex flex-wrap items-center gap-2 border-t pt-4">
        <Button
          variant="outline"
          size="sm"
          disabled={!inside}
          title="Split at the playhead (S)"
          onClick={() =>
            ctx.update((t) => {
              const parts = splitSound(s, ctx.time)
              const i = t.sounds.findIndex((x) => x.id === s.id)

              if (parts) {
                t.sounds.splice(i, 1, ...parts)
              }
            })
          }
        >
          <Scissors /> Split
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto text-red-700 dark:text-red-400"
          onClick={() => {
            ctx.update((t) => void (t.sounds = t.sounds.filter((x) => x.id !== s.id)))
            ctx.onSelect(null)
          }}
        >
          <Trash2 /> Delete
        </Button>
      </div>
    </div>
  )
}

/** The right panel: what the selection is and how to change it. */
export function Inspector({ ctx }: { ctx: Ctx }) {
  const { sel, doc } = ctx
  const c = sel?.kind === 'clip' ? doc.clips.find((x) => x.id === sel.id) : undefined
  const cap = sel?.kind === 'caption' ? doc.captions.find((x) => x.id === sel.id) : undefined
  const s = sel?.kind === 'sound' ? doc.sounds.find((x) => x.id === sel.id) : undefined

  if (c) {
    return <ClipInspector ctx={ctx} c={c} />
  }

  if (cap) {
    return <CaptionInspector ctx={ctx} c={cap} />
  }

  if (s) {
    return <SoundInspector ctx={ctx} s={s} />
  }

  const starts = clipStarts(doc)

  return (
    <div className="flex flex-col gap-3 text-sm text-muted-foreground">
      <p>Pick something on the timeline to change it.</p>
      <ul className={cn('flex flex-col gap-1 text-xs', !doc.clips.length && 'hidden')}>
        {doc.clips.map((k, i) => (
          <li key={k.id}>
            <button type="button" className="hover:underline" onClick={() => ctx.onSelect({ kind: 'clip', id: k.id })}>
              {starts[i]!.toFixed(1)} s · {k.name}
            </button>
          </li>
        ))}
      </ul>
      <p className="text-xs">
        Space plays and pauses, S splits at the playhead, Delete removes the selection, the arrow keys step a frame.
      </p>
    </div>
  )
}
