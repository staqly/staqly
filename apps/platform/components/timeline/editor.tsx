'use client'

import {
  ArrowLeft,
  Check,
  Download,
  Film,
  ImageIcon,
  Loader2,
  Minus,
  Music,
  Pause,
  Play,
  Plus,
  Redo2,
  Save,
  Scaling,
  SkipBack,
  Type,
  Undo2,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { draftKey, dropDraft, offerDraft, writeDraft } from '@/components/design/drafts'
import { announceEdited } from '@/components/design/edited-media'
import { loadFont } from '@/components/design/fonts'
import { NameField } from '@/components/design/name-field'
import { SaveDialog } from '@/components/design/save-dialog'
import { useHistory } from '@/components/design/store'
import { Button } from '@/components/ui/button'
import { api, mediaUrl, type Media } from '@/lib/api'
import { downloadBlob } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { safeNextPath } from '@/lib/safe'
import { cn } from '@/lib/utils'

import { play as playSound } from './audio'
import {
  clipAt,
  clock,
  length,
  newClip,
  newSound,
  newTimeline,
  parseTimeline,
  splitClip,
  splitSound,
  type Clip,
  type Timeline,
} from './doc'
import { canExportVideo, exportAudio, exportVideo, type Progress } from './export'
import { addFromLibrary, AddPanelBody, Inspector, type AddPanel, type Ctx } from './panels'
import { drawFrame } from './render'
import { Tracks, type Selection } from './tracks'

interface Params {
  ids: string[]
  project: string
  back: string
  fromComposer: boolean
  /** The post's token when its Video button opened a new video (see useDesignedMedia). */
  for: string
}

function readParams(search: string): Params {
  const q = new URLSearchParams(search)

  return {
    ids: (q.get('ids') ?? '').split(',').filter(Boolean).slice(0, 30),
    project: q.get('project') ?? '',
    back: safeNextPath(q.get('return'), 'http://localhost', '/media'),
    fromComposer: q.get('from') === 'composer',
    for: q.get('for') ?? '',
  }
}

/** A frame size for the first picture's shape: 9:16, 4:5, 1:1 or 16:9. */
function sizeFor(m?: Pick<Media, 'width' | 'height'>) {
  const r = m?.width && m.height ? m.width / m.height : 9 / 16

  return r < 0.7 ? [1080, 1920] : r < 0.9 ? [1080, 1350] : r < 1.3 ? [1080, 1080] : [1920, 1080]
}

interface Opened {
  doc: Timeline
  project?: string
  output?: string
}

async function fromProject(id: string): Promise<Opened | null> {
  const [p, file] = await Promise.all([api.mediaProject(id), api.mediaProjectFile(id)])
  const doc = parseTimeline(await file.text())

  return doc ? { doc, project: p.id, output: p.output_id || undefined } : null
}

/** What the editor opens: a saved project, the file a project saved, or library files on a new timeline. */
async function open(params: Params, kind: Timeline['kind']): Promise<Opened | null> {
  if (params.project) {
    return fromProject(params.project)
  }

  if (!params.ids.length) {
    return null
  }

  // One file a timeline saved: its project opens again, editable.
  if (params.ids.length === 1) {
    for (const p of (await api.mediaProjects(params.ids[0]!)).data.slice(0, 5)) {
      if (p.output_id === params.ids[0]) {
        const o = await fromProject(p.id).catch(() => null)

        if (o?.doc.kind === kind) {
          return o
        }
      }
    }
  }

  const media = await Promise.all(params.ids.map((id) => api.media(id)))
  const visual = media.filter((m) => m.mime_type.startsWith('image/') || m.mime_type.startsWith('video/'))
  const [w, h] = sizeFor(visual[0])
  const doc = newTimeline(kind, w, h)

  doc.name = (media[0]?.original_name ?? doc.name).replace(/\.[a-z0-9]+$/i, '')

  if (kind === 'video') {
    doc.clips = visual.map((m) =>
      newClip({
        id: m.id,
        kind: m.mime_type.startsWith('video/') ? 'video' : 'image',
        src: mediaUrl(m.id),
        name: m.original_name.replace(/\.[a-z0-9]+$/i, ''),
        width: m.width,
        height: m.height,
        duration: m.duration_ms ? m.duration_ms / 1000 : undefined,
      }),
    )
  }

  // Sounds one after another; in a video they play under the pictures from the start.
  let at = 0

  for (const m of media.filter((x) => x.mime_type.startsWith('audio/') && x.duration_ms)) {
    const s = newSound(
      {
        id: m.id,
        src: mediaUrl(m.id),
        name: m.original_name.replace(/\.[a-z0-9]+$/i, ''),
        duration: m.duration_ms! / 1000,
      },
      at,
    )

    doc.sounds.push(s)
    at += s.duration
  }

  // A soundtrack fades out with the pictures.
  const pictures = doc.clips.reduce((n, c) => n + c.duration, 0)

  if (kind === 'video' && pictures && doc.sounds.length === 1 && doc.sounds[0]!.duration > pictures) {
    doc.sounds[0]!.duration = pictures
    doc.sounds[0]!.fadeOut = Math.min(2, pictures / 4)
  }

  return { doc }
}

/** The time in a video file a clip shows at `local` seconds into it. */
const fileTime = (c: Clip, local: number) => Math.min(c.in + local, Math.max(0, (c.fileDuration ?? Infinity) - 0.05))

/** Draws the preview at `at`: video elements follow the playhead (playing along, or sought when paused). */
function paint(
  ctx: CanvasRenderingContext2D,
  doc: Timeline,
  at: number,
  live: boolean,
  images: Map<string, HTMLImageElement>,
  videos: Map<string, HTMLVideoElement>,
) {
  const on = clipAt(doc, at)

  for (const [src, v] of videos) {
    const c = on?.clip.kind === 'video' && on.clip.src === src ? on.clip : null

    if (!c) {
      if (!v.paused) {
        v.pause()
      }

      continue
    }

    const want = fileTime(c, on!.local)

    if (live) {
      if (v.paused) {
        v.currentTime = want
        void v.play().catch(() => undefined)
      } else if (Math.abs(v.currentTime - want) > 0.25) {
        v.currentTime = want
      }
    } else {
      if (!v.paused) {
        v.pause()
      }

      if (Math.abs(v.currentTime - want) > 0.02) {
        v.currentTime = want
      }
    }
  }

  drawFrame(ctx, doc, at, (c) => {
    const el = c.kind === 'image' ? images.get(c.src) : videos.get(c.src)

    if (!el) {
      return null
    }

    return el instanceof HTMLImageElement
      ? el.complete && el.naturalWidth
        ? el
        : null
      : el.readyState >= 2
        ? el
        : null
  })
}

/** Loads the timeline's pictures, videos and caption fonts for the preview; `ready` runs as each one arrives. */
function preload(
  doc: Timeline,
  images: Map<string, HTMLImageElement>,
  videos: Map<string, HTMLVideoElement>,
  ready: () => void,
) {
  for (const c of doc.clips) {
    if (c.kind === 'image' && !images.has(c.src)) {
      const img = new Image()

      img.crossOrigin = 'anonymous'
      img.addEventListener('load', ready)
      img.src = c.src
      images.set(c.src, img)
    }

    if (c.kind === 'video' && !videos.has(c.src)) {
      const v = document.createElement('video')

      v.crossOrigin = 'anonymous'
      v.muted = true
      v.playsInline = true
      v.preload = 'auto'
      v.addEventListener('loadeddata', ready)
      v.addEventListener('seeked', ready)
      v.src = c.src
      videos.set(c.src, v)
    }
  }

  for (const f of new Set(doc.captions.map((c) => `${c.font}|${c.bold}`))) {
    void loadFont(f.split('|')[0]!, f.endsWith('true')).then(ready)
  }
}

const STAGE: Record<Progress['stage'], string> = {
  preparing: 'Getting the files ready',
  frames: 'Making the frames',
  sound: 'Mixing the sound',
  finishing: 'Finishing the file',
}

/** Exports the timeline (a video, or a sound) as a file, with its name and extension. */
async function render(doc: Timeline, onProgress: (p: Progress) => void, signal: AbortSignal) {
  const blob = doc.kind === 'video' ? await exportVideo(doc, onProgress, signal) : await exportAudio(doc, onProgress)
  const ext = blob.type === 'video/mp4' ? 'mp4' : blob.type === 'audio/mp4' ? 'm4a' : 'wav'

  return { blob, name: `${doc.name.trim() || (doc.kind === 'video' ? 'Video' : 'Sound')}.${ext}` }
}

export default function TimelineEditor({ search, kind }: { search: string; kind: Timeline['kind'] }) {
  const [params] = useState(() => readParams(search))
  const { doc, update, undo, redo, replace, canUndo, canRedo } = useHistory(newTimeline(kind))
  const [phase, setPhase] = useState<'loading' | 'ready'>(params.project || params.ids.length ? 'loading' : 'ready')
  const [panel, setPanel] = useState<AddPanel | null>(kind === 'video' ? 'media' : 'audio')
  const [sel, setSel] = useState<Selection>(null)
  const [time, setTime] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [pps, setPps] = useState(kind === 'video' ? 60 : 30)
  const [project, setProject] = useState(params.project)
  const [output, setOutput] = useState<string | undefined>()
  const [baseline, setBaseline] = useState<Timeline | null>(null)
  const [busy, setBusy] = useState<{ what: 'save' | 'download'; progress: Progress } | null>(null)
  const [asking, setAsking] = useState(false)
  const [leaving, setLeaving] = useState(false)
  const [supported, setSupported] = useState(true)
  const canvas = useRef<HTMLCanvasElement>(null)
  const audio = useRef<AudioContext | null>(null)
  const playback = useRef<{ origin: number; from: number; stop: () => void } | null>(null)
  const abort = useRef<AbortController | null>(null)
  const left = useRef(false)
  // Pictures and video elements the preview draws from, by file.
  const images = useRef(new Map<string, HTMLImageElement>())
  const videos = useRef(new Map<string, HTMLVideoElement>())
  const total = length(doc)
  const dirty = baseline ? baseline !== doc : canUndo
  const [draft, setDraft] = useState(() => draftKey(`staqly-${kind}-draft:`, params))
  const video = kind === 'video'

  useEffect(() => {
    void open(params, kind)
      .then((o) => {
        if (o) {
          replace(o.doc)
          setBaseline(o.doc)
          setProject(o.project ?? '')
          setOutput(o.output)
        }
      })
      .catch((e: unknown) => toastApiError(e, 'Could not open it'))
      .finally(() => {
        setPhase('ready')
        offerDraft(draftKey(`staqly-${kind}-draft:`, params), parseTimeline, replace)
      })
  }, [params, kind, replace])

  // The draft follows every change, a second after the last one.
  useEffect(() => {
    if (!dirty || phase !== 'ready') {
      return
    }

    const t = window.setTimeout(() => writeDraft(draft, doc), 1000)

    return () => window.clearTimeout(t)
  }, [dirty, phase, draft, doc])

  useEffect(() => {
    if (video) {
      void canExportVideo().then(setSupported)
    }
  }, [video])

  const draw = useCallback(
    (at: number, live: boolean) => {
      const ctx = canvas.current?.getContext('2d')

      if (ctx && video) {
        paint(ctx, doc, at, live, images.current, videos.current)
      }
    },
    [doc, video],
  )

  // A picture, a video frame or a font that comes in later is drawn when it arrives.
  const redraw = useRef(() => {})

  useEffect(() => {
    redraw.current = () => {
      if (!playback.current) {
        draw(time, false)
      }
    }
  })

  // Every picture and video of the timeline, loaded for the preview.
  useEffect(() => {
    preload(doc, images.current, videos.current, () => redraw.current())
  }, [doc])

  // Paused: the frame at the playhead, again whenever something changes or loads.
  useEffect(() => {
    if (!playing) {
      draw(time, false)
    }
  }, [draw, time, playing])

  const stop = useCallback(() => {
    playback.current?.stop()
    playback.current = null
    setPlaying(false)
    videos.current.forEach((v) => v.pause())
  }, [])

  const play = () => {
    if (playing) {
      stop()

      return
    }

    if (!audio.current) {
      audio.current = new AudioContext()
    }

    const ctx = audio.current
    const from = time >= total - 0.05 ? 0 : time

    void ctx.resume().then(() =>
      playSound(ctx, doc, from).then((h) => {
        playback.current = { ...h, from }
        setPlaying(true)
      }),
    )
  }

  // Playing: the clock is the sound's, so the pictures never drift from the music.
  useEffect(() => {
    if (!playing) {
      return
    }

    let raf = 0

    const tick = () => {
      const p = playback.current
      const ctx = audio.current

      if (!p || !ctx) {
        return
      }

      const at = p.from + Math.max(0, ctx.currentTime - p.origin)

      if (at >= total) {
        stop()
        setTime(total)

        return
      }

      draw(at, true)
      setTime(at)
      raf = requestAnimationFrame(tick)
    }

    raf = requestAnimationFrame(tick)

    return () => cancelAnimationFrame(raf)
  }, [playing, total, draw, stop])

  // A change stops the playback (the sound was scheduled for the timeline as it was).
  const edit = useCallback(
    (fn: (t: Timeline) => void, coalesce?: string) => {
      stop()
      update(fn, coalesce)
    },
    [stop, update],
  )

  const seek = (t: number) => {
    stop()
    setTime(Math.max(0, Math.min(total, t)))
  }

  const ctx: Ctx = { doc, update: edit, time, sel, onSelect: setSel }

  const removeSelection = () => {
    if (!sel) {
      return
    }

    update((t) => {
      if (sel.kind === 'clip') {
        t.clips = t.clips.filter((x) => x.id !== sel.id)
      } else if (sel.kind === 'caption') {
        t.captions = t.captions.filter((x) => x.id !== sel.id)
      } else {
        t.sounds = t.sounds.filter((x) => x.id !== sel.id)
      }
    })
    setSel(null)
  }

  const split = () =>
    update((t) => {
      if (sel?.kind === 'sound') {
        const i = t.sounds.findIndex((x) => x.id === sel.id)
        const parts = i >= 0 ? splitSound(t.sounds[i]!, time) : null

        if (parts) {
          t.sounds.splice(i, 1, ...parts)
        }

        return
      }

      const on = clipAt(t, time)
      const parts = on ? splitClip(on.clip, on.local) : null

      if (on && parts) {
        t.clips.splice(on.index, 1, ...parts)
      }
    })

  // Shortcuts, unless a field has the keyboard.
  const latest = useRef({ play, split, removeSelection, seek, time, undo, redo })

  useEffect(() => {
    latest.current = { play, split, removeSelection, seek, time, undo, redo }
  })

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement

      if (t.closest('input, textarea, select, [contenteditable=true]')) {
        return
      }

      const k = latest.current
      const mod = e.metaKey || e.ctrlKey

      if (e.key === ' ') {
        e.preventDefault()
        k.play()
      } else if (mod && e.key.toLowerCase() === 'z') {
        e.preventDefault()

        if (e.shiftKey) {
          k.redo()
        } else {
          k.undo()
        }
      } else if (!mod && e.key.toLowerCase() === 's') {
        e.preventDefault()
        k.split()
      } else if (e.key === 'Delete' || e.key === 'Backspace') {
        e.preventDefault()
        k.removeSelection()
      } else if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        e.preventDefault()
        k.seek(k.time + (e.key === 'ArrowLeft' ? -1 : 1) * (e.shiftKey ? 1 : 1 / 30))
      } else if (e.key === 'Home') {
        k.seek(0)
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    if (!dirty) {
      return
    }

    const warn = (e: BeforeUnloadEvent) => {
      if (!left.current) {
        e.preventDefault()
      }
    }

    window.addEventListener('beforeunload', warn)

    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  /** Makes the file, then hands it over: a download, or the library (the project saved next to it). */
  const make = (what: 'save' | 'download', folder = '', d: Timeline = doc) => {
    stop()

    const ctrl = new AbortController()

    abort.current = ctrl
    setBusy({ what, progress: { stage: 'preparing', share: 0 } })

    // The screen changes only when the progress moves by a percent, and nothing on it animates: on a slow or
    // software graphics card, redrawing the page starves the video encoder, which runs on the same GPU process.
    let shown = ''

    void render(
      d,
      (progress) => {
        const key = `${progress.stage}${Math.floor(progress.share * 100)}`

        if (key !== shown) {
          shown = key
          setBusy({ what, progress })
        }
      },
      ctrl.signal,
    )
      .then(async ({ blob, name }) => {
        if (what === 'download') {
          downloadBlob(name, blob)

          return
        }

        const media = await api.saveEdited(output, blob, name)

        if (folder) {
          await api.organizeMedia({ ids: [media.id], folder_id: folder })
        }

        const sources = [...new Set([...d.clips.map((c) => c.mediaId), ...d.sounds.map((s) => s.mediaId)])].slice(0, 20)
        const p = await api.saveMediaProject(
          { id: project || undefined, name: d.name, source_ids: sources, output_id: media.id, page_ids: [media.id] },
          new Blob([JSON.stringify(d)], { type: 'application/json' }),
        )

        // A post open in another tab follows: it takes the new file (a new video made from its Video button, or
        // this one saved again).
        if (params.fromComposer && (output || params.for)) {
          announceEdited(output ? { from: output, to: media.id } : { from: '', to: media.id, for: params.for })
        }

        setProject(p.id)
        setOutput(media.id)
        setBaseline(d)
        dropDraft(draft)
        setDraft(`staqly-${kind}-draft:${p.id}`)
        // A reload keeps the way back (the post it came from) and the post that follows its saves.
        window.history.replaceState(
          null,
          '',
          `?project=${encodeURIComponent(p.id)}${params.back !== '/media' ? `&return=${encodeURIComponent(params.back)}` : ''}${params.fromComposer ? '&from=composer' : ''}`,
        )
        toast.success(`Saved to Media as ${media.original_name}`, {
          description: 'Open it from Media to keep editing.',
        })
      })
      .catch((e: unknown) => {
        if (!(e instanceof DOMException && e.name === 'AbortError')) {
          toastApiError(e, what === 'save' ? 'Could not save' : 'Could not make the file')
        }
      })
      .finally(() => setBusy(null))
  }

  const save = () => (project ? make('save') : setAsking(true))
  const empty = video ? !doc.clips.length && !doc.sounds.length : !doc.sounds.length
  const PANELS: { id: AddPanel; label: string; icon: React.ReactNode }[] = video
    ? [
        { id: 'media', label: 'Media', icon: <ImageIcon /> },
        { id: 'audio', label: 'Music', icon: <Music /> },
        { id: 'text', label: 'Text', icon: <Type /> },
        { id: 'size', label: 'Size', icon: <Scaling /> },
      ]
    : [{ id: 'audio', label: 'Sounds', icon: <Music /> }]

  return (
    <div
      className="fixed inset-0 flex flex-col bg-background"
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault()

        for (const f of e.dataTransfer.files) {
          void api
            .uploadMedia(f, f.name)
            .then((m) => addFromLibrary(ctx, m))
            .catch((err: unknown) => toastApiError(err, `Could not add ${f.name}`))
        }
      }}
    >
      {asking && (
        <SaveDialog
          name={doc.name}
          description={
            video
              ? 'The video is made in your browser and saved to Media as an MP4; the timeline stays editable.'
              : 'The sound is mixed in your browser and saved to Media; the timeline stays editable.'
          }
          onClose={() => setAsking(false)}
          onSave={(name, folder) => {
            setAsking(false)

            const named = { ...doc, name }

            replace(named)
            make('save', folder, named)
          }}
        />
      )}
      <ConfirmDialog
        open={leaving}
        onOpenChange={setLeaving}
        title="Leave without saving?"
        description={`Your changes to this ${video ? 'video' : 'sound'} are not saved yet. Leave and they are gone, or stay and save them to Media.`}
        confirmLabel="Leave without saving"
        variant="destructive"
        onConfirm={() => {
          left.current = true
          dropDraft(draft)
          window.location.href = params.back
        }}
      />
      {busy && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/85">
          <div
            className="flex w-80 flex-col gap-3 rounded-xl border bg-card p-5 shadow-lg"
            role="status"
            aria-live="polite"
          >
            <p className="flex items-center gap-2 text-sm font-medium">
              <Film className="size-4 text-brand" /> {STAGE[busy.progress.stage]}
            </p>
            <div className="h-2 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-brand"
                style={{ width: `${Math.round(busy.progress.share * 100)}%` }}
              />
            </div>
            <p className="text-xs text-muted-foreground">Made in your browser: keep this tab open.</p>
            <Button variant="outline" size="sm" className="self-end" onClick={() => abort.current?.abort()}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      <header className="flex h-14 shrink-0 items-center gap-1.5 border-b px-2 sm:px-3">
        <Button variant="ghost" asChild>
          <a
            href={params.back}
            onClick={(e) => {
              if (dirty) {
                e.preventDefault()
                setLeaving(true)
              }
            }}
          >
            <ArrowLeft aria-hidden />
            <span className="max-sm:sr-only">Media</span>
          </a>
        </Button>
        <span className="h-5 w-px bg-border" aria-hidden />
        <span
          className="flex size-7 items-center justify-center rounded-md bg-brand-soft text-brand [&_svg]:size-4"
          aria-hidden
        >
          {video ? <Film /> : <Music />}
        </span>
        <NameField
          value={doc.name}
          placeholder={video ? 'Untitled video' : 'Untitled sound'}
          onChange={(name) => update((t) => void (t.name = name), 'name')}
        />
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            aria-label="Undo"
            title="Undo (Ctrl+Z)"
            disabled={!canUndo}
            onClick={undo}
          >
            <Undo2 />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Redo"
            title="Redo (Ctrl+Shift+Z)"
            disabled={!canRedo}
            onClick={redo}
          >
            <Redo2 />
          </Button>
          <span className="mx-1 h-5 w-px bg-border max-sm:hidden" aria-hidden />
          {video && (
            <span className="px-1 text-xs text-muted-foreground tabular-nums max-sm:hidden">
              {doc.width} x {doc.height}
            </span>
          )}
          <Button
            variant="outline"
            disabled={!!busy || empty || (video && !supported)}
            onClick={() => make('download')}
          >
            <Download />
            <span className="max-sm:sr-only">Download</span>
          </Button>
          <Button
            onClick={save}
            variant={dirty || !project ? 'default' : 'outline'}
            disabled={!!busy || phase !== 'ready' || empty || (video && !supported)}
            className="relative"
            data-testid="timeline-save"
          >
            {dirty || !project ? <Save /> : <Check />}
            {dirty || !project ? 'Save to Media' : 'Saved'}
            {dirty && (
              <span className="absolute -top-1 -right-1 flex size-2.5" aria-hidden>
                <span className="absolute inline-flex size-full animate-ping rounded-full bg-brand opacity-60 motion-reduce:hidden" />
                <span className="relative inline-flex size-2.5 rounded-full bg-brand ring-2 ring-background" />
              </span>
            )}
          </Button>
        </div>
      </header>

      {video && !supported && (
        <p className="border-b bg-amber-500/10 px-4 py-2 text-sm text-amber-900 dark:text-amber-200">
          This browser can&apos;t make video files. Edit here, and save from a recent Chrome or Edge.
        </p>
      )}

      <div className="relative flex min-h-0 flex-1 max-md:flex-col-reverse">
        <nav
          aria-label="Video tools"
          className="flex shrink-0 gap-0.5 border-r bg-background p-1.5 md:w-[76px] md:flex-col max-md:overflow-x-auto max-md:border-t max-md:border-r-0"
        >
          {PANELS.map((p) => (
            <button
              key={p.id}
              type="button"
              aria-pressed={panel === p.id}
              onClick={() => setPanel((cur) => (cur === p.id ? null : p.id))}
              className={cn(
                'flex min-w-16 flex-col items-center gap-1 rounded-lg px-1 py-2 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground [&_svg]:size-5',
                panel === p.id && 'bg-brand-soft text-brand',
              )}
            >
              {p.icon}
              {p.label}
            </button>
          ))}
        </nav>
        {panel && (
          <aside
            aria-label={PANELS.find((p) => p.id === panel)?.label}
            className="flex w-80 shrink-0 flex-col border-r bg-background max-md:absolute max-md:inset-x-0 max-md:bottom-[68px] max-md:z-20 max-md:h-[55%] max-md:w-full max-md:border-t max-md:shadow-lg"
          >
            <div className="flex h-12 shrink-0 items-center justify-between border-b pr-1.5 pl-4">
              <h2 className="text-sm font-semibold">{PANELS.find((p) => p.id === panel)?.label}</h2>
              <Button variant="ghost" size="icon-sm" aria-label="Close" onClick={() => setPanel(null)}>
                <X />
              </Button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              <AddPanelBody ctx={ctx} panel={panel} />
            </div>
          </aside>
        )}

        <main className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="flex min-h-0 flex-1">
            {/* The preview: the frame at the playhead, or the sound's summary. */}
            <div className="flex min-h-0 min-w-0 flex-1 items-center justify-center bg-muted/60 p-4 [background-image:radial-gradient(var(--border)_1px,transparent_1px)] [background-size:16px_16px]">
              {phase === 'loading' ? (
                <Loader2 className="animate-spin text-muted-foreground" />
              ) : video ? (
                <canvas
                  ref={canvas}
                  width={doc.width}
                  height={doc.height}
                  aria-label="Preview"
                  data-testid="timeline-preview"
                  className="max-h-full max-w-full rounded-md bg-black shadow-lg"
                  style={{ aspectRatio: `${doc.width} / ${doc.height}` }}
                />
              ) : (
                <div className="flex flex-col items-center gap-2 text-center">
                  <span className="flex size-20 items-center justify-center rounded-2xl bg-emerald-600/15 text-emerald-600 [&_svg]:size-10">
                    <Music />
                  </span>
                  <p className="text-sm font-medium">
                    {doc.sounds.length ? `${doc.sounds.length} sound clips` : 'No sound yet'}
                  </p>
                  <p className="text-xs text-muted-foreground">Trim, split, fade and line them up below.</p>
                </div>
              )}
            </div>
            <aside aria-label="Properties" className="w-72 shrink-0 overflow-y-auto border-l p-4 max-lg:hidden">
              <Inspector ctx={ctx} />
            </aside>
          </div>

          <div className="flex h-11 shrink-0 items-center gap-1 border-t px-2">
            <Button variant="ghost" size="icon-sm" aria-label="Back to the start" onClick={() => seek(0)}>
              <SkipBack />
            </Button>
            <Button
              size="icon-sm"
              aria-label={playing ? 'Pause' : 'Play'}
              title="Space"
              onClick={play}
              disabled={!total}
              data-testid="timeline-play"
            >
              {playing ? <Pause /> : <Play />}
            </Button>
            <span className="px-2 font-mono text-xs tabular-nums" data-testid="timeline-time">
              {clock(time)} / {clock(total)}
            </span>
            <Button variant="ghost" size="sm" onClick={split} title="Split at the playhead (S)">
              Split
            </Button>
            <div className="ml-auto flex items-center gap-0.5">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Zoom out the timeline"
                onClick={() => setPps((p) => Math.max(8, p / 1.4))}
              >
                <Minus />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Zoom in the timeline"
                onClick={() => setPps((p) => Math.min(240, p * 1.4))}
              >
                <Plus />
              </Button>
            </div>
          </div>
          <div className={cn('flex min-h-0 shrink-0 flex-col border-t', video ? 'h-[210px]' : 'h-[320px]')}>
            <Tracks doc={doc} time={time} pps={pps} sel={sel} onSelect={setSel} onSeek={seek} update={edit} />
          </div>
        </main>
      </div>
    </div>
  )
}
