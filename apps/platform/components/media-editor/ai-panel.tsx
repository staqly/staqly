'use client'

import { useEditorServices, useEditorState } from '@opengpex/editor/core/context'
import { useQuery } from '@tanstack/react-query'
import { Eraser, ImagePlus, Loader2, Maximize2, RotateCw, Scissors, Sparkles, Wand2, X } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { NativeSelect } from '@/components/common/native-select'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { api, errorMessage } from '@/lib/api'
import { qk } from '@/lib/queries'

import { aiExpand } from './ai-ops'
import { PLACEMENTS, ratioLabel } from './fit'
import { useStaqlyCommands } from './hooks'
import { MaskPainter } from './mask-painter'
import { aiSize, bitmapOf, cover, downscale } from './raster'

const ON_DEVICE = 'opengpex.drawers.ai_tools'

/** One AI request at a time: progress with elapsed seconds, cancel, and retry after a failure. */
function useAiRun() {
  const [busy, setBusy] = useState<{ label: string } | null>(null)
  const [error, setError] = useState<{ message: string; retry: () => void } | null>(null)
  const [seconds, setSeconds] = useState(0)
  const abort = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!busy) {
      return
    }

    const t = setInterval(() => setSeconds((n) => n + 1), 1000)

    return () => clearInterval(t)
  }, [busy])

  const run = (label: string, fn: (signal: AbortSignal) => Promise<void>): Promise<void> => {
    const ctrl = new AbortController()

    abort.current?.abort()
    abort.current = ctrl
    setError(null)
    setSeconds(0)
    setBusy({ label })

    return fn(ctrl.signal)
      .catch((e: unknown) => {
        if (!(e instanceof DOMException && e.name === 'AbortError')) {
          setError({ message: errorMessage(e), retry: () => void run(label, fn) })
        }
      })
      .then(() => setBusy(null))
  }

  return {
    busy,
    error,
    run,
    seconds,
    cancel: () => abort.current?.abort(),
  }
}

/**
 * The editor's AI panel. Server AI (make, change by a description, remove a marked object, extend) runs on the
 * workspace's own OpenAI key and adds its result as a new layer, or a new version when the shape changes, so
 * undo or closing the version takes it back. Background removal and the other on-device tools open
 * OpenGPEX's AI drawer: they run in the browser, without a key.
 */
export function AiPanel({ onClose }: { onClose: () => void }) {
  const status = useQuery({ queryKey: qk.ai, queryFn: api.ai })
  const { state } = useEditorState()
  const { actions } = useEditorServices()
  const cmd = useStaqlyCommands()
  const ai = useAiRun()
  const [prompt, setPrompt] = useState('')
  const [shape, setShape] = useState('1x1')
  const [masking, setMasking] = useState<Blob | null>(null)
  const [maskPrompt, setMaskPrompt] = useState('')
  const [expandTo, setExpandTo] = useState(PLACEMENTS[2] ? `${PLACEMENTS[2].w}:${PLACEMENTS[2].h}` : '9:16')
  const st = status.data
  const caps = st?.configured ? st.capabilities : undefined
  const hasPicture = state.frames.order.length > 0
  const disabled = !!ai.busy

  const picture = async () => {
    const blob = await cmd.exportImage({ format: 'image/png' })
    const bmp = await bitmapOf(blob)
    const size = { w: bmp.width, h: bmp.height }

    bmp.close()

    return { blob, size }
  }

  const generate = () =>
    ai.run('Making the picture', async (signal) => {
      const [w, h] = shape.split('x').map(Number)
      const out = await api.aiImage('generate', { prompt, size: aiSize(st!.model, w!, h!) }, signal)

      await cmd.addImageLayer({ blob: out, name: 'AI picture.png' })
      toast.success('Added as a layer')
    })

  const edit = () =>
    ai.run('Changing the picture', async (signal) => {
      const { blob, size } = await picture()
      const out = await api.aiImage(
        'edit',
        { prompt, size: aiSize(st!.model, size.w, size.h), image: await downscale(blob) },
        signal,
      )

      await cmd.addImageLayer({ blob: await cover(out, size.w, size.h), name: 'AI edit.png' })
      toast.success('Added as a layer on top', { description: 'Hide or delete the layer to go back.' })
    })

  const removeMarked = (mask: Blob, w: number, h: number, source: Blob) =>
    ai.run('Removing the marked area', async (signal) => {
      const size = aiSize(st!.model, w, h)
      const [sw, sh] = size.split('x').map(Number)
      const out = await api.aiImage(
        'edit',
        {
          prompt: `Remove what is marked and fill it in so it matches its surroundings. ${maskPrompt}`.trim(),
          size,
          image: await cover(source, sw!, sh!),
          mask: await cover(mask, sw!, sh!),
        },
        signal,
      )

      await cmd.addImageLayer({ blob: await cover(out, w, h), name: 'AI removal.png' })
      toast.success('Added as a layer on top', { description: 'Hide or delete the layer to go back.' })
    })

  const expand = () =>
    ai.run('Extending the picture', async (signal) => {
      const [rw, rh] = expandTo.split(':').map(Number)
      const { blob } = await picture()

      await cmd.addVersion({
        blob: await aiExpand(blob, rw! / rh!, st!.model, signal),
        name: `Extended to ${expandTo}.png`,
      })
      toast.success(`Extended to ${expandTo}`, { description: 'A new version beside the original, in the dock below.' })
    })

  const openOnDevice = () => {
    const ids = state.ui.activeSidebarIds ?? []

    if (!ids.includes(ON_DEVICE)) {
      actions.updateUI({ activeSidebarIds: [...ids, ON_DEVICE] })
    }

    onClose()
  }

  const noKey = !st?.configured

  return (
    <aside
      aria-label="AI"
      data-testid="ai-panel"
      className="absolute top-0 right-0 bottom-0 z-[300] flex w-full max-w-sm flex-col border-l bg-background shadow-xl"
    >
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Sparkles className="size-4" aria-hidden />
        <h2 className="flex-1 text-sm font-semibold">AI</h2>
        <Button size="icon" variant="ghost" aria-label="Close AI" onClick={onClose}>
          <X />
        </Button>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto p-4 text-sm">
        <Section title="On this device" icon={<Scissors />}>
          <p className="text-muted-foreground">
            Remove the background, select or erase an object, upscale. Runs in your browser, no key needed; the first
            use downloads the model.
          </p>
          <Button variant="outline" onClick={openOnDevice} disabled={!hasPicture}>
            <Wand2 /> Open the on-device tools
          </Button>
        </Section>

        {status.isPending ? null : noKey ? (
          <Callout
            tone="info"
            action={
              st?.can_manage ? (
                <Button size="sm" variant="outline" asChild>
                  {/* oxlint-disable-next-line nextjs/no-html-link-for-pages -- leaves the isolated editor page */}
                  <a href="/integrations/ai">Set up AI</a>
                </Button>
              ) : undefined
            }
          >
            {st?.can_manage
              ? 'Making and changing pictures by a description uses your own OpenAI key.'
              : 'An admin can add an OpenAI key under Integrations to make and change pictures by a description.'}
          </Callout>
        ) : (
          st?.reason && <Callout tone="warning">{st.reason}</Callout>
        )}

        <Section title="Describe" icon={<ImagePlus />}>
          <Textarea
            aria-label="Description"
            placeholder="A bright, modern office in the background"
            value={prompt}
            maxLength={4000}
            onChange={(e) => setPrompt(e.target.value)}
            disabled={noKey || disabled}
          />
          <div className="flex flex-wrap gap-2">
            <NativeSelect
              aria-label="Shape of a new picture"
              value={shape}
              onChange={(e) => setShape(e.target.value)}
              className="w-28"
              disabled={noKey || disabled}
            >
              <option value="1x1">Square</option>
              <option value="3x2">Landscape</option>
              <option value="2x3">Portrait</option>
            </NativeSelect>
            <Button
              onClick={generate}
              disabled={!caps?.generate || !prompt.trim() || disabled}
              data-testid="ai-generate"
            >
              Make it
            </Button>
            <Button
              variant="outline"
              onClick={edit}
              disabled={!caps?.edit || !prompt.trim() || !hasPicture || disabled}
              title={caps && !caps.edit ? `${st?.model} cannot change pictures` : undefined}
            >
              Change the picture
            </Button>
          </div>
        </Section>

        <Section title="Remove an object" icon={<Eraser />}>
          <p className="text-muted-foreground">Paint over what should go; the AI fills in what was behind it.</p>
          <Button
            variant="outline"
            disabled={!caps?.inpaint || !hasPicture || disabled}
            onClick={async () => setMasking((await picture()).blob)}
          >
            <Eraser /> Mark what to remove
          </Button>
        </Section>

        <Section title="Extend to a shape" icon={<Maximize2 />}>
          <div className="flex flex-wrap gap-2">
            <NativeSelect
              aria-label="Target shape"
              value={expandTo}
              onChange={(e) => setExpandTo(e.target.value)}
              className="w-44"
              disabled={!caps?.expand || disabled}
            >
              {[...new Set(PLACEMENTS.map((p) => `${p.w}:${p.h}`))].map((r) => {
                const [w, h] = r.split(':').map(Number)
                const names = PLACEMENTS.filter((p) => `${p.w}:${p.h}` === r).map((p) => `${p.platform} ${p.name}`)

                return (
                  <option key={r} value={r}>
                    {ratioLabel(w!, h!)} · {names[0]}
                    {names.length > 1 ? ` +${names.length - 1}` : ''}
                  </option>
                )
              })}
            </NativeSelect>
            <Button variant="outline" onClick={expand} disabled={!caps?.expand || !hasPicture || disabled}>
              Extend
            </Button>
          </div>
        </Section>

        {ai.busy && (
          <div role="status" className="flex items-center gap-2 rounded-lg border p-3">
            <Loader2 className="size-4 animate-spin" aria-hidden />
            <span className="flex-1">
              {ai.busy.label}... {ai.seconds}s
            </span>
            <Button size="sm" variant="ghost" onClick={ai.cancel}>
              Cancel
            </Button>
          </div>
        )}
        {ai.error && (
          <Callout
            tone="danger"
            action={
              <Button size="sm" variant="outline" onClick={ai.error.retry}>
                <RotateCw /> Retry
              </Button>
            }
          >
            {ai.error.message}
          </Callout>
        )}
      </div>

      {masking && (
        <MaskPainter
          picture={masking}
          prompt={maskPrompt}
          onPrompt={setMaskPrompt}
          onCancel={() => setMasking(null)}
          onDone={(mask, w, h) => {
            const source = masking

            setMasking(null)
            void removeMarked(mask, w, h, source)
          }}
        />
      )}
    </aside>
  )
}

function Section({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="flex items-center gap-2 text-sm font-medium [&_svg]:size-4 [&_svg]:text-muted-foreground">
        {icon}
        {title}
      </h3>
      {children}
    </section>
  )
}
