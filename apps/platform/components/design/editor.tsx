'use client'

import { useQuery } from '@tanstack/react-query'
import type Konva from 'konva'
import {
  ArrowLeft,
  Check,
  CircleHelp,
  ChevronLeft,
  ChevronRight,
  Copy,
  Download,
  Loader2,
  Maximize,
  Minus,
  Plus,
  Redo2,
  Save,
  Trash2,
  Undo2,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useAiStatus } from '@/lib/ai-status'
import { api, mediaUrl, type Media, type Provider } from '@/lib/api'
import { downloadBlob } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'
import { safeNextPath } from '@/lib/safe'
import { cn } from '@/lib/utils'

import { discardAiRun } from './ai-panel'
import { DesignCanvas, DesignView, preloadPage } from './canvas'
import {
  copyPage,
  credits,
  newDesign,
  newImage,
  newPage,
  parseDesign,
  withGroups,
  uid,
  type Design,
  type El,
  type Page,
} from './doc'
import { draftKey, dropDraft, offerDraft, writeDraft } from './drafts'
import { announceEdited } from './edited-media'
import { NameField } from './name-field'
import { addFile, Panel, PANELS, type PanelCtx, type PanelId } from './panels'
import { jpegsToPdf } from './pdf'
import { SaveDialog } from './save-dialog'
import { useDesignStore } from './store'
import { Toolbar } from './toolbar'

interface Params {
  ids: string[]
  project: string
  back: string
  fromComposer: boolean
  /** The post's token when its Design button opened a new design (see useDesignedMedia). */
  for: string
}

function readParams(search: string): Params {
  const q = new URLSearchParams(search)

  return {
    ids: (q.get('ids') ?? '').split(',').filter(Boolean).slice(0, 10),
    project: q.get('project') ?? '',
    back: safeNextPath(q.get('return'), 'http://localhost', '/media'),
    fromComposer: q.get('from') === 'composer',
    for: q.get('for') ?? '',
  }
}

/** A new design around library pictures: the page takes the first one's size (at most 4096 px), each on top. */
function designFor(media: Media[]): Design {
  const first = media[0]!
  const k = Math.min(1, 4096 / Math.max(first.width || 1080, first.height || 1080))
  const d = newDesign(Math.round((first.width || 1080) * k), Math.round((first.height || 1080) * k))

  d.name = first.original_name.replace(/\.[a-z0-9]+$/i, '')
  // A page per picture (a carousel), in the order they were picked, each the size of the first: a picture of
  // another shape fits inside its page, whole.
  d.pages = media.map((m) => {
    const page = newPage()
    const w = m.width || 1080
    const h = m.height || 1080
    const fit = Math.min(d.width / w, d.height / h)

    page.elements = [
      {
        ...newImage(d, mediaUrl(m.id), w, h, { mediaId: m.id }),
        x: (d.width - w * fit) / 2,
        y: (d.height - h * fit) / 2,
        width: w * fit,
        height: h * fit,
      },
    ]

    return page
  })

  return d
}

/** The file each page saved as last, by page id (saving again replaces it, so posts follow). */
type Saved = Record<string, string>

const savedOf = (d: Design): Saved => Object.fromEntries(d.pages.flatMap((p) => (p.mediaId ? [[p.id, p.mediaId]] : [])))

interface Opened {
  design: Design
  project?: string
  saved?: Saved
  page?: string
  notice?: string
}

/** What the address names: a saved design, a library file saved by a design (that page), or pictures to design around. */
async function open(params: Params): Promise<Opened | null> {
  if (params.project) {
    const [p, file] = await Promise.all([api.mediaProject(params.project), api.mediaProjectFile(params.project)])
    const d = parseDesign(await file.text())

    if (d) {
      return { design: d, project: p.id, saved: savedOf(d) }
    }

    // A project of the former photo editor (before 2026-10-25): its saved picture opens instead.
    const notice = 'This project was made in the former photo editor'

    return p.output_id ? { design: designFor([await api.media(p.output_id)]), notice } : { design: newDesign(), notice }
  }

  if (!params.ids.length) {
    return null
  }

  if (params.ids.length === 1) {
    const id = params.ids[0]!

    for (const p of (await api.mediaProjects(id)).data.slice(0, 5)) {
      const d = parseDesign(await (await api.mediaProjectFile(p.id)).text())
      const page = d?.pages.find((x) => x.mediaId === id)

      if (d && page) {
        return { design: d, project: p.id, saved: savedOf(d), page: page.id }
      }
    }
  }

  const media = (await Promise.all(params.ids.map((id) => api.media(id)))).filter((m) =>
    m.mime_type.startsWith('image/'),
  )

  return media.length ? { design: designFor(media) } : null
}

/** A width-to-height ratio as people say it: 0.8 = 4:5, 1.91 = 1.91:1. */
const shape = (r?: number) =>
  !r ? 'any' : r < 1 ? `1:${Math.round((1 / r) * 100) / 100}` : `${Math.round(r * 100) / 100}:1`

const DRAFT = 'staqly-design-draft:'

// The off-screen copy is small; the file is drawn at full size from it (Konva redraws at the pixel ratio).
const RENDER_SCALE = 0.25

const twoFrames = () => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))

const hasPhotos = (p: Page) => p.elements.some((e) => e.type === 'image' && !e.hidden)

interface SaveArgs {
  design: Design
  saved: Saved
  project: string
  /** Draws page i on the canvas and answers it as a file. */
  render: (i: number, type: 'image/png' | 'image/jpeg') => Promise<Blob>
}

/**
 * Saves every page as a picture in Media (JPG with photos, PNG without), replacing what it saved before, and the
 * design itself as the project, its pages remembering their files.
 */
async function saveAll({ design, saved, project, render }: SaveArgs) {
  const base = design.name.trim() || 'Design'
  const n = design.pages.length
  const media: { page: string; before?: string; after: Media }[] = []

  for (const [i, page] of design.pages.entries()) {
    const type = hasPhotos(page) ? 'image/jpeg' : 'image/png'
    const name = `${base}${n > 1 ? ` (${i + 1} of ${n})` : ''}.${type === 'image/jpeg' ? 'jpg' : 'png'}`
    const blob = await render(i, type)
    const before = saved[page.id]
    const after = before ? await api.replaceMedia(before, blob, name) : await api.uploadMedia(blob, name)

    media.push({ page: page.id, before, after })
  }

  const files = Object.fromEntries(media.map((m) => [m.page, m.after.id]))
  const file: Design = { ...design, pages: design.pages.map((p) => ({ ...p, mediaId: files[p.id] })) }
  // Listed by every page's file too (GET /v1/media-projects?media=), so Edit on any page opens the design.
  const sources = [
    ...new Set([
      ...design.pages.flatMap((p) => p.elements.flatMap((e) => (e.type === 'image' && e.mediaId ? [e.mediaId] : []))),
      ...media.slice(1).map((m) => m.after.id),
    ]),
  ].slice(0, 20)
  const p = await api.saveMediaProject(
    {
      id: project || undefined,
      name: base,
      source_ids: sources,
      output_id: media[0]!.after.id,
      page_ids: media.map((m) => m.after.id),
    },
    new Blob([JSON.stringify(file)], { type: 'application/json' }),
  )

  return { project: p.id, saved: files, media }
}

export default function DesignEditor({ search }: { search: string }) {
  const [params] = useState(() => readParams(search))
  const store = useDesignStore(newDesign())
  const { design, update, undo, redo, replace } = store
  const [phase, setPhase] = useState<'loading' | 'ready'>(params.project || params.ids.length ? 'loading' : 'ready')
  const [panel, setPanel] = useState<PanelId | null>(params.project || params.ids.length ? null : 'templates')
  const [pageId, setPageId] = useState(design.pages[0]!.id)
  const [selected, setSelected] = useState<string[]>([])
  const [cropping, setCropping] = useState<string | null>(null)
  const [zoom, setZoom] = useState<number | 'fit'>('fit')
  const [fitScale, setFitScale] = useState(1)
  const [busy, setBusy] = useState('')
  const [project, setProject] = useState(params.project)
  const [files, setFiles] = useState<Saved>({})
  const [baseline, setBaseline] = useState<Design | null>(null)
  // Saving and downloading draw each page off screen: the page on screen stays put.
  const [rendering, setRendering] = useState<Page | null>(null)
  const view = useRef<Konva.Stage | null>(null)
  const page = design.pages.find((p) => p.id === pageId) ?? design.pages[0]!
  // AI shows only when the server offers an AI tool for pictures (a provider key and an operation).
  const ai = useAiStatus()
  const tools = PANELS.filter((p) => p.id !== 'ai' || ai.data?.operations.some((o) => o.kind === 'image'))
  // Platforms with picture rules, for downloads made to fit them.
  const providers = useQuery({ queryKey: qk.providers, queryFn: async () => (await api.providers()).data ?? [] })
  const channels = (providers.data ?? []).filter((p) => p.status === 'available' && p.limits.image?.max_bytes)
  // Any change since it opened or saved (a new design: anything at all, even only its name).
  const dirty = baseline ? baseline !== design : store.canUndo
  const [leaving, setLeaving] = useState(false)
  const [draft, setDraft] = useState(() => draftKey(DRAFT, params))
  const left = useRef(false)

  const showPage = useCallback((id: string) => {
    setPageId(id)
    setSelected([])
    setCropping(null)
  }, [])

  // An AI request still waiting when the editor closes is cancelled (its files go too).
  useEffect(() => discardAiRun, [])

  useEffect(() => {
    void open(params)
      .then((o) => {
        if (!o) {
          return
        }

        replace(o.design)
        setBaseline(o.design)
        setProject(o.project ?? '')
        setFiles(o.saved ?? {})
        setPageId(o.page ?? o.design.pages[0]!.id)

        if (o.notice) {
          toast.info(o.notice, { description: 'It opened as a new design.' })
        }
      })
      .catch((e: unknown) => toastApiError(e, 'Could not open it'))
      .finally(() => {
        setPhase('ready')
        offerDraft(draftKey(DRAFT, params), parseDesign, replace)
      })
  }, [params, replace])

  // The draft follows every change, a second after the last one.
  useEffect(() => {
    if (!dirty || phase !== 'ready') {
      return
    }

    const t = window.setTimeout(() => writeDraft(draft, design), 1000)

    return () => window.clearTimeout(t)
  }, [dirty, phase, draft, design])

  useEffect(() => {
    if (!dirty) {
      return
    }

    // Closing the tab or reloading: only the browser's own question can stop that. Leaving by the back link
    // asks in the editor instead (and then goes without this one).
    const warn = (e: BeforeUnloadEvent) => {
      if (!left.current) {
        e.preventDefault()
      }
    }

    window.addEventListener('beforeunload', warn)

    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  const updatePage = useCallback(
    (fn: (p: Page) => void, coalesce?: string) =>
      update((d) => {
        const p = d.pages.find((x) => x.id === pageId) ?? d.pages[0]!

        fn(p)
      }, coalesce),
    [update, pageId],
  )
  const add = useCallback(
    (...els: El[]) => {
      updatePage((p) => void p.elements.push(...els))
      setSelected(els.map((e) => e.id))
    },
    [updatePage],
  )
  // Picking one member of a group picks the whole group.
  const select = useCallback((ids: string[]) => setSelected(withGroups(page.elements, ids)), [page])
  const ctx: PanelCtx = { store, page, updatePage, selected, onSelect: select, add, showPage }

  // Shortcuts, unless a field has the keyboard.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement

      if (t.closest('input, textarea, select, [contenteditable="true"], [role="dialog"], [role="menu"]')) {
        return
      }

      const mod = e.metaKey || e.ctrlKey
      const key = e.key.toLowerCase()

      if (mod && key === 'z') {
        e.preventDefault()

        if (e.shiftKey) {
          redo()
        } else {
          undo()
        }
      } else if (mod && key === 'y') {
        e.preventDefault()
        redo()
      } else if (mod && key === 'd' && selected.length) {
        e.preventDefault()

        const copies = page.elements
          .filter((x) => selected.includes(x.id))
          .map((x) => ({ ...structuredClone(x), id: uid(), x: x.x + 24, y: x.y + 24, locked: false }))

        add(...copies)
      } else if (mod && key === 'g' && selected.length) {
        e.preventDefault()

        const id = uid()
        const ungroup = e.shiftKey

        updatePage((p) => {
          for (const x of p.elements) {
            if (selected.includes(x.id)) {
              x.group = ungroup ? undefined : id
            }
          }
        })
      } else if (mod && key === 'a') {
        e.preventDefault()
        setSelected(page.elements.filter((x) => !x.locked && !x.hidden).map((x) => x.id))
      } else if ((e.key === 'Delete' || e.key === 'Backspace') && selected.length) {
        e.preventDefault()
        updatePage((p) => void (p.elements = p.elements.filter((x) => !selected.includes(x.id) || x.locked)))
        setSelected([])
      } else if (e.key === 'Escape') {
        setSelected([])
        setCropping(null)
      } else if (e.key.startsWith('Arrow') && selected.length) {
        e.preventDefault()

        const step = e.shiftKey ? 10 : 1
        const dx = e.key === 'ArrowLeft' ? -step : e.key === 'ArrowRight' ? step : 0
        const dy = e.key === 'ArrowUp' ? -step : e.key === 'ArrowDown' ? step : 0

        updatePage((p) => {
          for (const x of p.elements) {
            if (selected.includes(x.id) && !x.locked) {
              x.x += dx
              x.y += dy
            }
          }
        }, 'nudge')
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [selected, page, undo, redo, updatePage, add])

  // Pictures pasted or dropped onto the editor go into the library and onto the page.
  const addFiles = async (list: File[]) => {
    for (const f of list.filter((x) => x.type.startsWith('image/'))) {
      await addFile(ctx, f, f.name || 'pasted.png').catch((e) =>
        toastApiError(e, `Could not add ${f.name || 'the picture'}`),
      )
    }
  }

  // The paste listener stays put and calls the newest addFiles (it reads the page on screen).
  const latestAddFiles = useRef(addFiles)

  useEffect(() => {
    latestAddFiles.current = addFiles
  })

  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const list = [...(e.clipboardData?.files ?? [])]

      if (list.length && !(e.target as HTMLElement).closest('input, textarea')) {
        e.preventDefault()
        void latestAddFiles.current(list)
      }
    }

    window.addEventListener('paste', onPaste)

    return () => window.removeEventListener('paste', onPaste)
  }, [])

  /** Draws page i off screen once its pictures and fonts are in, and answers it as a file at full size. */
  const render = async (i: number, type: 'image/png' | 'image/jpeg', quality = 0.92, k = 1) => {
    const p = design.pages[i]!

    await preloadPage(p)
    setRendering(p)
    await twoFrames()

    const stage = view.current!

    return (await stage.toBlob({ pixelRatio: k / RENDER_SCALE, mimeType: type, quality })) as Blob
  }

  /**
   * Every page as the platform takes it: its format, at most its pixels, under its file size (JPEG quality down to
   * 0.6 first, then smaller), and a word when the page's shape is outside what it accepts.
   */
  const forChannel = (p: Provider) =>
    run('download', async () => {
      const rules = p.limits.image ?? {}
      const type = !rules.formats?.length || rules.formats.includes('image/jpeg') ? 'image/jpeg' : 'image/png'
      const base = design.name.trim() || 'Design'
      const n = design.pages.length
      const ratio = design.width / design.height

      for (const i of design.pages.keys()) {
        let k = rules.max_pixels ? Math.min(1, Math.sqrt(rules.max_pixels / (design.width * design.height))) : 1
        let q = 0.92
        let blob = await render(i, type, q, k)

        while (rules.max_bytes && blob.size > rules.max_bytes && k > 0.2) {
          if (type === 'image/jpeg' && q > 0.65) {
            q -= 0.1
          } else {
            k *= 0.85
          }

          blob = await render(i, type, q, k)
        }

        downloadBlob(
          `${base}${n > 1 ? ` (${i + 1} of ${n})` : ''} (${p.name}).${type === 'image/jpeg' ? 'jpg' : 'png'}`,
          blob,
        )
      }

      if ((rules.min_ratio && ratio < rules.min_ratio - 0.01) || (rules.max_ratio && ratio > rules.max_ratio + 0.01)) {
        toast.warning(`${p.name} may crop or refuse this shape`, {
          description: `It takes pictures from ${shape(rules.min_ratio)} to ${shape(rules.max_ratio)}: pick a ${p.name} size under Size.`,
        })
      } else if (rules.min_width && design.width < rules.min_width) {
        toast.warning(`${p.name} wants pictures at least ${rules.min_width} pixels wide`)
      }
    })

  /** Every page in one PDF (LinkedIn posts a PDF as a document people swipe through). */
  const pdf = () =>
    run('download', async () => {
      const pages: Uint8Array[] = []

      for (const i of design.pages.keys()) {
        pages.push(new Uint8Array(await (await render(i, 'image/jpeg', 0.9)).arrayBuffer()))
      }

      downloadBlob(`${design.name.trim() || 'Design'}.pdf`, jpegsToPdf(pages))
    })

  const run = (label: string, fn: () => Promise<void>) => {
    setBusy(label)
    setSelected([])
    setCropping(null)
    // The selection frame leaves the canvas before anything is drawn into a file.
    void twoFrames()
      .then(fn)
      .catch((e: unknown) => toastApiError(e, label === 'save' ? 'Could not save' : 'Could not download'))
      .finally(() => {
        setBusy('')
        setRendering(null)
      })
  }

  // A new design asks for its name and folder first (once); a saved one saves straight away.
  const [asking, setAsking] = useState(false)
  const save = () => (project || params.fromComposer ? persist(design, '') : setAsking(true))
  const persist = (doc: Design, folder: string) =>
    run('save', async () => {
      const out = await saveAll({ design: doc, saved: files, project, render })

      if (folder) {
        await api.organizeMedia({ ids: out.media.map((m) => m.after.id), folder_id: folder })
      }

      setProject(out.project)
      setFiles(out.saved)
      setBaseline(doc)
      dropDraft(draft)
      setDraft(DRAFT + out.project)
      window.history.replaceState(
        null,
        '',
        `?project=${encodeURIComponent(out.project)}${params.back !== '/media' ? `&return=${encodeURIComponent(params.back)}` : ''}${params.fromComposer ? '&from=composer' : ''}`,
      )

      // A post open in another tab follows: its attachments become the new files (or get them, for a new design).
      if (params.fromComposer) {
        for (const [i, m] of out.media.entries()) {
          const from = m.before ?? (i === 0 ? params.ids[0] : undefined)

          if (from) {
            announceEdited({ from, to: m.after.id })
          } else if (params.for) {
            announceEdited({ from: '', to: m.after.id, for: params.for })
          }
        }
      }

      const owed = credits(doc)
      const n = out.media.length

      toast.success(n > 1 ? `Saved ${n} pages to Media` : `Saved to Media as ${out.media[0]!.after.original_name}`, {
        description: params.fromComposer
          ? 'The post you came from uses it now.'
          : owed.length
            ? `Credit when you post: ${owed.join(', ')}.`
            : 'Open it from Media to keep editing.',
      })
    })

  const download = (type: 'image/png' | 'image/jpeg', all: boolean) =>
    run('download', async () => {
      const at = page.id
      const n = design.pages.length
      const base = design.name.trim() || 'Design'
      const ext = type === 'image/png' ? 'png' : 'jpg'

      for (const [i, p] of design.pages.entries()) {
        if (all || p.id === at) {
          downloadBlob(`${base}${n > 1 ? ` (${i + 1} of ${n})` : ''}.${ext}`, await render(i, type))
        }
      }
    })

  const scale = zoom === 'fit' ? fitScale : zoom
  const zoomBy = (k: number) => setZoom(Math.min(4, Math.max(0.05, Math.round(scale * k * 100) / 100)))
  const toPost = params.back.startsWith('/posts')
  const index = design.pages.findIndex((p) => p.id === page.id)

  const pages = {
    add: () => {
      const p = newPage()

      update((d) => void d.pages.splice(index + 1, 0, p))
      showPage(p.id)
    },
    duplicate: () => {
      const p = copyPage(page)

      update((d) => void d.pages.splice(index + 1, 0, p))
      showPage(p.id)
    },
    remove: () => {
      const next = design.pages[index + 1] ?? design.pages[index - 1]

      update((d) => void d.pages.splice(index, 1))
      showPage(next!.id)
    },
    move: (by: number) =>
      update((d) => {
        const [p] = d.pages.splice(index, 1)

        d.pages.splice(Math.max(0, Math.min(d.pages.length, index + by)), 0, p!)
      }),
    /** Puts a page before the one at `before` (the number of pages: at the end). */
    moveTo: (id: string, before: number) =>
      update((d) => {
        const from = d.pages.findIndex((p) => p.id === id)
        const [p] = d.pages.splice(from, 1)

        d.pages.splice(before > from ? before - 1 : before, 0, p!)
      }),
  }

  return (
    <div
      className="fixed inset-0 flex flex-col bg-background"
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault()
        void addFiles([...e.dataTransfer.files])
      }}
    >
      {asking && (
        <SaveDialog
          name={design.name}
          pages={design.pages.length}
          onClose={() => setAsking(false)}
          onSave={(name, folder) => {
            setAsking(false)

            const named = { ...design, name }

            replace(named)
            persist(named, folder)
          }}
        />
      )}
      <ConfirmDialog
        open={leaving}
        onOpenChange={setLeaving}
        title="Leave without saving?"
        description="Your changes to this design are not saved yet. Leave and they are gone, or stay and save them to Media."
        confirmLabel="Leave without saving"
        variant="destructive"
        onConfirm={() => {
          left.current = true
          dropDraft(draft)
          window.location.href = params.back
        }}
      />
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
            <span className="max-sm:sr-only">{toPost ? 'Back to the post' : 'Media'}</span>
          </a>
        </Button>
        <span className="h-5 w-px bg-border" aria-hidden />
        <NameField value={design.name} onChange={(name) => update((d) => void (d.name = name), 'name')} />
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            aria-label="Undo"
            title="Undo (Ctrl+Z)"
            disabled={!store.canUndo}
            onClick={undo}
          >
            <Undo2 />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Redo"
            title="Redo (Ctrl+Shift+Z)"
            disabled={!store.canRedo}
            onClick={redo}
          >
            <Redo2 />
          </Button>
          <span className="mx-1 h-5 w-px bg-border max-sm:hidden" aria-hidden />
          <button
            type="button"
            className="px-1 text-xs text-muted-foreground tabular-nums hover:text-foreground max-sm:hidden pointer-coarse:h-11"
            title="Change the size"
            onClick={() => setPanel('size')}
          >
            {design.width} x {design.height}
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" disabled={!!busy || phase !== 'ready'}>
                {busy === 'download' ? <Loader2 className="animate-spin" /> : <Download />}
                <span className="max-sm:sr-only">Download</span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => download('image/png', false)}>PNG, sharpest</DropdownMenuItem>
              <DropdownMenuItem onSelect={() => download('image/jpeg', false)}>JPG, smallest file</DropdownMenuItem>
              {design.pages.length > 1 && (
                <>
                  <DropdownMenuItem onSelect={() => download('image/png', true)}>Every page, PNG</DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => download('image/jpeg', true)}>Every page, JPG</DropdownMenuItem>
                </>
              )}
              <DropdownMenuItem onSelect={pdf}>
                {design.pages.length > 1 ? 'Every page in one PDF (LinkedIn document)' : 'PDF'}
              </DropdownMenuItem>
              {channels.length > 0 && (
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>For a channel</DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {channels.map((p) => (
                      <DropdownMenuItem key={p.id} onSelect={() => forChannel(p)}>
                        {p.name}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button
            onClick={save}
            variant={dirty || !project ? 'default' : 'outline'}
            disabled={!!busy || phase !== 'ready'}
            tooltip={dirty ? 'You have unsaved changes' : project ? 'Everything is saved' : undefined}
            className="relative"
            data-testid="design-save"
          >
            {busy === 'save' ? <Loader2 className="animate-spin" /> : dirty || !project ? <Save /> : <Check />}
            {params.fromComposer ? 'Save and use in the post' : dirty || !project ? 'Save to Media' : 'Saved'}
            {dirty && (
              <span className="absolute -top-1 -right-1 flex size-2.5" aria-hidden>
                <span className="absolute inline-flex size-full animate-ping rounded-full bg-brand opacity-60 motion-reduce:hidden" />
                <span className="relative inline-flex size-2.5 rounded-full bg-brand ring-2 ring-background" />
              </span>
            )}
            {dirty && <span className="sr-only">(unsaved changes)</span>}
          </Button>
        </div>
      </header>

      <div className="relative flex min-h-0 flex-1 max-md:flex-col-reverse">
        <nav
          aria-label="Design tools"
          className="flex shrink-0 gap-0.5 border-r bg-background p-1.5 md:w-[76px] md:flex-col max-md:overflow-x-auto max-md:border-t max-md:border-r-0"
        >
          {tools.map((p) => (
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
            className="flex w-80 shrink-0 flex-col border-r bg-background max-md:absolute max-md:inset-x-0 max-md:bottom-[68px] max-md:z-20 max-md:h-[55%] max-md:w-full max-md:border-t max-md:border-r-0 max-md:shadow-lg"
          >
            <div className="flex h-12 shrink-0 items-center justify-between border-b pr-1.5 pl-4">
              <h2 className="text-sm font-semibold">{PANELS.find((p) => p.id === panel)?.label}</h2>
              <Button variant="ghost" size="icon-sm" aria-label="Close" onClick={() => setPanel(null)}>
                <X />
              </Button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-4">
              <Panel id={panel} ctx={ctx} />
            </div>
          </aside>
        )}
        <main className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="flex h-12 shrink-0 items-center border-b">
            <Toolbar ctx={ctx} cropping={cropping} onCropping={setCropping} />
          </div>
          <div className="relative min-h-0 flex-1">
            {phase === 'loading' ? (
              <div className="flex size-full items-center justify-center">
                <Loader2 className="animate-spin text-muted-foreground" />
              </div>
            ) : (
              <DesignCanvas
                design={design}
                page={page}
                updatePage={updatePage}
                selected={selected}
                onSelect={select}
                cropping={cropping}
                onCropping={(id) => {
                  setCropping(id)

                  if (id) {
                    setSelected([id])
                  }
                }}
                zoom={zoom}
                onFitScale={setFitScale}
                onDropEls={(els) => add(...els)}
              />
            )}
            <div className="absolute right-3 bottom-3 flex items-center gap-0.5 rounded-lg border bg-background p-0.5 shadow-sm">
              <Button variant="ghost" size="icon-sm" aria-label="Zoom out" onClick={() => zoomBy(1 / 1.25)}>
                <Minus />
              </Button>
              <button
                type="button"
                className="w-12 text-xs tabular-nums pointer-coarse:h-11"
                title="Fit to screen"
                onClick={() => setZoom('fit')}
              >
                {Math.round(scale * 100)}%
              </button>
              <Button variant="ghost" size="icon-sm" aria-label="Zoom in" onClick={() => zoomBy(1.25)}>
                <Plus />
              </Button>
              <Button variant="ghost" size="icon-sm" aria-label="Fit to screen" onClick={() => setZoom('fit')}>
                <Maximize />
              </Button>
            </div>
          </div>
          <PagesBar design={design} current={page.id} onShow={showPage} actions={pages} />
          {rendering && (
            <div className="pointer-events-none fixed top-0 -left-[10000px]" aria-hidden>
              <DesignView design={design} page={rendering} scale={RENDER_SCALE} stageRef={view} />
            </div>
          )}
        </main>
      </div>
    </div>
  )
}

/** The pages of the design (a carousel when there are several): pick one, add, copy, reorder, delete. */
function PagesBar({
  design,
  current,
  onShow,
  actions,
}: {
  design: Design
  current: string
  onShow: (id: string) => void
  actions: {
    add: () => void
    duplicate: () => void
    remove: () => void
    move: (by: number) => void
    moveTo: (id: string, before: number) => void
  }
}) {
  const h = 52
  const index = design.pages.findIndex((p) => p.id === current)
  const [dragging, setDragging] = useState<string | null>(null)
  const [over, setOver] = useState<number | null>(null)
  const n = design.pages.length

  return (
    <div className="flex h-[76px] shrink-0 items-center gap-2 border-t px-2" aria-label="Pages" role="group">
      <div className="flex min-w-0 flex-1 items-center gap-2 overflow-x-auto p-1.5">
        {design.pages.map((p, i) => (
          <button
            key={p.id}
            type="button"
            aria-label={`Page ${i + 1}`}
            aria-current={p.id === current ? 'page' : undefined}
            onClick={() => onShow(p.id)}
            // Dragged sideways to reorder (the arrows on the right do the same from the keyboard).
            draggable={n > 1}
            onDragStart={(e) => {
              e.dataTransfer.effectAllowed = 'move'
              e.dataTransfer.setData('text/plain', p.id)
              setDragging(p.id)
            }}
            onDragOver={(e) => {
              if (!dragging) {
                return
              }

              e.preventDefault()
              e.stopPropagation()

              const r = e.currentTarget.getBoundingClientRect()

              setOver(e.clientX < r.left + r.width / 2 ? i : i + 1)
            }}
            onDrop={(e) => {
              e.preventDefault()
              e.stopPropagation()

              if (dragging && over !== null) {
                actions.moveTo(dragging, over)
              }

              setDragging(null)
              setOver(null)
            }}
            onDragEnd={() => {
              setDragging(null)
              setOver(null)
            }}
            className={cn(
              'relative shrink-0 overflow-hidden rounded-md border bg-background transition-[opacity,box-shadow]',
              p.id === current ? 'ring-2 ring-brand' : 'hover:ring-2 hover:ring-border',
              dragging === p.id && 'opacity-40',
              dragging && over === i && 'shadow-[-4px_0_0_0_var(--brand)]',
              dragging && over === n && i === n - 1 && 'shadow-[4px_0_0_0_var(--brand)]',
            )}
          >
            <DesignView design={design} page={p} scale={h / design.height} />
            <span className="absolute bottom-0.5 left-0.5 rounded bg-background px-1 text-[10px] tabular-nums">
              {i + 1}
            </span>
          </button>
        ))}
        <Button
          variant="outline"
          size="icon"
          aria-label="Add a page"
          title="Add a page"
          onClick={actions.add}
          className="shrink-0"
        >
          <Plus />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="What are pages for?"
          tooltip="Several pages make a carousel: a post people swipe through on Instagram, LinkedIn, Threads, X or TikTok. Each page saves as its own picture in Media, in order, and a post gets them all."
          className="shrink-0 text-muted-foreground"
        >
          <CircleHelp />
        </Button>
      </div>
      <div className="flex shrink-0 items-center gap-0.5">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Move the page left"
          tooltip="Move the page left"
          disabled={index === 0}
          onClick={() => actions.move(-1)}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Move the page right"
          tooltip="Move the page right"
          disabled={index === design.pages.length - 1}
          onClick={() => actions.move(1)}
        >
          <ChevronRight />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Duplicate the page"
          tooltip="Duplicate the page"
          onClick={actions.duplicate}
        >
          <Copy />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Delete the page"
          tooltip="Delete the page"
          className="text-red-700 dark:text-red-400"
          disabled={design.pages.length === 1}
          onClick={actions.remove}
        >
          <Trash2 />
        </Button>
      </div>
    </div>
  )
}
