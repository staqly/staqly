'use client'

import { useEditorServices, useEditorState } from '@opengpex/editor/core/context'
import { ArrowLeft, ChevronDown, Download, FileImage, FolderOpen, Save } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { api, ApiError, type Media } from '@/lib/api'
import { downloadBlob } from '@/lib/format'
import { toastApiError } from '@/lib/notify'

import { announceEdited } from './edited-media'
import { readParams, useStaqlyCommands } from './hooks'

/** The original's format when it can hold the result, PNG otherwise (see-through pixels, several pictures). */
export function outputFormat(source: Pick<Media, 'mime_type'> | undefined): {
  format: 'image/png' | 'image/jpeg' | 'image/webp'
  ext: string
} {
  if (source?.mime_type === 'image/jpeg') {
    return { format: 'image/jpeg', ext: 'jpg' }
  }

  if (source?.mime_type === 'image/webp') {
    return { format: 'image/webp', ext: 'webp' }
  }

  return { format: 'image/png', ext: 'png' }
}

export const baseName = (name: string) => name.replace(/\.[a-z0-9]+$/i, '')

/**
 * The bar above the editor: back to where you came from, and the ways to keep the work. Saving makes a new
 * file by default; replacing the original is a confirmed choice; the project keeps layers editable.
 */
export function EditorBar({
  onSources,
  panels,
}: {
  onSources?: (m: Media[]) => void
  /** Buttons for the editor's staqly panels (AI, channels), left of saving. */
  panels?: React.ReactNode
}) {
  const { state } = useEditorState()
  const { plugins, actions } = useEditorServices()
  const cmd = useStaqlyCommands()
  const [params] = useState(() => readParams(typeof window === 'undefined' ? '' : window.location.search))
  const [sources, setSources] = useState<Media[]>([])
  const [phase, setPhase] = useState<'loading' | 'ready' | 'error'>('loading')
  const [busy, setBusy] = useState('')
  const [confirmReplace, setConfirmReplace] = useState(false)
  const [leaving, setLeaving] = useState(false)
  const [saved, setSaved] = useState<Media | null>(null)
  const [projectId, setProjectId] = useState(params.project)
  const booted = useRef(false)
  const [baseline, setBaseline] = useState<unknown>(null)
  const dirty = phase === 'ready' && baseline !== null && baseline !== state.frames

  // Open the files once the editor has started.
  useEffect(() => {
    if (!state.isLoaded || booted.current) {
      return
    }

    booted.current = true

    const boot = async () => {
      // The editor's own commands (open a file, export) register a moment after it reports loaded.
      for (let i = 0; i < 150 && !plugins.getCommand('adv.frame.trunk'); i++) {
        await new Promise((r) => setTimeout(r, 100))
      }

      // Every visit starts clean: OpenGPEX keeps its last session in the browser, staqly opens what was asked.
      if (state.frames.order.length) {
        actions.removeFrame([...state.frames.order], null)
      }

      let opened: Media[] = []

      if (params.project) {
        const project = await api.mediaProject(params.project)

        opened = (await Promise.all(project.source_ids.map((id) => api.media(id).catch(() => null)))).filter(
          (m): m is Media => !!m,
        )
        await cmd.unpack(await api.mediaProjectFile(params.project))
      } else if (params.ids.length) {
        const media = await Promise.all(params.ids.map((id) => api.media(id)))

        opened = media.filter((m) => m.mime_type.startsWith('image/'))

        if (!opened.length) {
          throw new Error('Only pictures can be edited here.')
        }

        const blobs = await Promise.all(opened.map((m) => api.mediaFile(m.id)))

        await cmd.openMedia({ files: opened.map((m, i) => ({ blob: blobs[i]!, name: m.original_name })) })
      }

      return opened
    }

    boot().then(
      (opened) => {
        setSources(opened)
        onSources?.(opened)
        setPhase('ready')
      },
      (e) => {
        setPhase('error')
        toastApiError(
          e instanceof ApiError && e.status === 404 ? new Error('That file is not in this workspace any more.') : e,
          'Could not open the file',
        )
      },
    )
    // oxlint-disable-next-line react/exhaustive-deps -- runs once, when the editor has loaded
  }, [state.isLoaded, params, cmd, plugins, onSources])

  // "No changes" is the state right after the files opened (once the editor settled), and after each save.
  useEffect(() => {
    if (phase === 'ready' && baseline === null) {
      const t = setTimeout(() => setBaseline(state.frames), 800)

      return () => clearTimeout(t)
    }
  }, [phase, baseline, state.frames])

  useEffect(() => {
    if (!dirty) {
      return
    }

    const warn = (e: BeforeUnloadEvent) => e.preventDefault()

    window.addEventListener('beforeunload', warn)

    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  const run = useCallback(async (label: string, fn: () => Promise<void>) => {
    setBusy(label)
    await fn().catch((e) => toastApiError(e, 'Could not save'))
    setBusy('')
  }, [])

  const source = sources[0]
  const fmt = outputFormat(sources.length === 1 ? source : undefined)
  const title = sources.length > 1 ? `${sources.length} pictures` : (source?.original_name ?? 'New picture')
  const toPost = params.back.startsWith('/posts')
  const backHref = params.back
  const fileName = `${baseName(source?.original_name ?? 'picture')} (edited).${fmt.ext}`

  const afterSave = (media: Media, message: string) => {
    setBaseline(state.frames)
    setSaved(media)

    // A post open in another tab (Edit on its attachment) takes the edit.
    if (source) {
      announceEdited({ from: source.id, to: media.id })
    }

    toast.success(message, {
      description: params.fromComposer ? 'The post you came from uses it now.' : undefined,
      action: params.fromComposer
        ? { label: 'Back to the post', onClick: () => window.close() }
        : { label: toPost ? 'Back to the post' : 'Open in Media', onClick: () => go(params.back) },
    })
  }

  const saveNew = () =>
    run('new', async () => {
      const media = await api.uploadMedia(await cmd.exportImage({ format: fmt.format }), fileName)

      // Next to the original in the library.
      if (source?.folder_id) {
        await api.organizeMedia({ ids: [media.id], folder_id: source.folder_id }).catch(() => undefined)
      }

      afterSave(media, `Saved as ${media.original_name}`)
    })

  const replace = () =>
    run('replace', async () => {
      if (!source) {
        return
      }

      const media = await api.replaceMedia(
        source.id,
        await cmd.exportImage({ format: fmt.format }),
        source.original_name,
      )

      setSources([media, ...sources.slice(1)])
      afterSave(media, 'The original is replaced')
    })

  const saveProject = () =>
    run('project', async () => {
      const project = await api.saveMediaProject(
        {
          id: projectId || undefined,
          name: baseName(source?.original_name ?? 'Untitled'),
          source_ids: sources.map((s) => s.id),
          output_id: saved?.id,
        },
        await cmd.pack(),
      )

      setProjectId(project.id)
      setBaseline(state.frames)
      window.history.replaceState(
        null,
        '',
        `?project=${encodeURIComponent(project.id)}${params.back !== '/media' ? `&return=${encodeURIComponent(params.back)}` : ''}`,
      )
      toast.success('Project saved', {
        description: 'Open it again from the file in Media to keep editing its layers.',
      })
    })

  const download = () =>
    run('download', async () => {
      downloadBlob(fileName, await cmd.exportImage({ format: fmt.format }))
    })

  const ready = phase === 'ready' && !busy

  return (
    <header
      aria-label="Edit media"
      data-testid="editor-bar"
      className="relative z-[60] flex h-12 shrink-0 items-center gap-2 border-b bg-background px-2 sm:px-3"
    >
      <Button variant="ghost" asChild>
        <a
          href={backHref}
          onClick={(e) => {
            if (dirty) {
              e.preventDefault()
              setLeaving(true)
            }
          }}
        >
          <ArrowLeft aria-hidden />
          <span className="max-sm:sr-only">{toPost ? 'Back to the post' : 'Back to Media'}</span>
        </a>
      </Button>
      <span className="h-5 w-px bg-border" aria-hidden />
      <h1 className="min-w-0 truncate px-1 text-sm font-medium" title={title}>
        {title}
        {dirty && <span className="ml-2 text-xs font-normal text-muted-foreground">Unsaved changes</span>}
      </h1>
      <div className="flex-1" />
      {panels}
      <ButtonGroup>
        <Button disabled={!ready} onClick={saveNew} data-testid="save-new">
          <Save aria-hidden /> {busy ? 'Saving...' : 'Save as new file'}
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" disabled={!ready} aria-label="More ways to save" data-testid="save-menu">
              <ChevronDown aria-hidden />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-64">
            {sources.length === 1 && (
              <DropdownMenuItem onSelect={() => setConfirmReplace(true)}>
                <FileImage aria-hidden /> Replace the original
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={saveProject} data-testid="save-project">
              <FolderOpen aria-hidden /> {projectId ? 'Save the project' : 'Save as a project'}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={download}>
              <Download aria-hidden /> Download
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </ButtonGroup>

      <ConfirmDialog
        open={confirmReplace}
        onOpenChange={setConfirmReplace}
        title={`Replace ${source?.original_name ?? 'the original'}?`}
        description="The edited picture takes its place: its name, alt text, folder and tags, and the drafts that use it. The original goes to Deleted. Scheduled and published posts keep the original, so a file they use is saved as a new file instead."
        confirmLabel="Replace"
        onConfirm={replace}
      />
      <ConfirmDialog
        open={leaving}
        onOpenChange={setLeaving}
        title="Leave without saving?"
        description="Your changes to this picture are lost. Save it as a new file or as a project to keep them."
        confirmLabel="Leave"
        variant="destructive"
        onConfirm={() => {
          setBaseline(state.frames)
          go(backHref)
        }}
      />
    </header>
  )
}

/** A full page load: the editor page is cross-origin isolated, the dashboard is not. */
function go(href: string) {
  window.location.assign(href)
}
