'use client'

import { useQuery } from '@tanstack/react-query'
import { LayoutTemplate, Loader2, RotateCw, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { api, errorMessage, type Media, type Provider } from '@/lib/api'
import { qk, useProviders } from '@/lib/queries'

import { formatBytes, placementFits, type PlacementFit, type Picture } from './fit'
import { useStaqlyCommands } from './hooks'
import { OptimizeDialog, makeVariant, type Strategy } from './optimize'
import { pictureFacts } from './raster'

const key = (f: PlacementFit) => `${f.provider}:${f.name}`

export interface Facts extends Picture {
  blob: Blob
  small: { data: Uint8ClampedArray; w: number; h: number }
}

/** Saves a cropped version per placement into a "Versions of <name>" folder next to the source, tagged "version". */
async function saveVersions(
  facts: Facts,
  chosen: PlacementFit[],
  source: Media | undefined,
  providerOf: (f: PlacementFit) => Provider | undefined,
) {
  const base = (source?.original_name ?? 'picture').replace(/\.[a-z0-9]+$/i, '')
  const folders = await api.mediaFolders()
  const name = `Versions of ${base}`.slice(0, 100)
  const folder =
    folders.data.find((f) => f.name === name && f.parent_id === (source?.folder_id ?? '')) ??
    (await api.createMediaFolder({ name, parent_id: source?.folder_id || undefined }))
  const saved: Media[] = []

  for (const f of chosen) {
    // One at a time: each is a full-size encode and upload.
    const blob = await makeVariant(facts, f, { kind: 'crop' }, providerOf(f))
    const media = await api.uploadMedia(
      blob,
      `${base} - ${f.platform} ${f.name} ${f.label.replace(':', 'x')}.${blob.type === 'image/jpeg' ? 'jpg' : 'png'}`,
    )

    saved.push(media)
  }

  if (saved.length) {
    await api.organizeMedia({ ids: saved.map((m) => m.id), folder_id: folder.id, add_tags: ['version'] })
  }

  return { count: saved.length, name }
}

/**
 * Channels: how the open picture fits each platform's placements. "Can't post" is a platform's hard limit
 * (its API refuses the file); "Crops n%" is a shape that only looks better another way. Optimize makes a
 * version that fits; "Make versions" saves one per chosen placement to the library.
 */
export function ChannelsPanel({ sources, onClose }: { sources: Media[]; onClose: () => void }) {
  const cmd = useStaqlyCommands()
  const providers = useProviders()
  const ai = useQuery({ queryKey: qk.ai, queryFn: api.ai })
  const [optimizing, setOptimizing] = useState<PlacementFit | null>(null)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [saving, setSaving] = useState(false)
  const source = sources[0]
  // The picture as it is now (the editor's state changes under the panel: "Check again" measures anew).
  const [run, setRun] = useState(0)
  const [measured, setMeasured] = useState<{ run: number; facts?: Facts; error?: string } | null>(null)

  useEffect(() => {
    let gone = false
    const format = source?.mime_type === 'image/jpeg' ? 'image/jpeg' : 'image/png'

    void cmd
      .exportImage({ format })
      .then(async (blob) => ({ ...(await pictureFacts(blob)), blob, bytes: blob.size, mime: format }))
      .then(
        (facts) => !gone && setMeasured({ run, facts }),
        (e) => !gone && setMeasured({ run, error: errorMessage(e) }),
      )

    return () => {
      gone = true
    }
  }, [cmd, source, run])

  const facts = measured?.run === run ? (measured.facts ?? null) : null
  const error = measured?.run === run ? (measured.error ?? '') : ''

  const fits = useMemo(
    () => (facts && providers.data ? placementFits(facts, providers.data as Provider[]) : []),
    [facts, providers.data],
  )
  const byPlatform = useMemo(() => Map.groupBy(fits, (f) => f.platform), [fits])
  const providerOf = (f: PlacementFit) => (providers.data as Provider[] | undefined)?.find((p) => p.id === f.provider)

  const saveVariants = () => {
    if (!facts) {
      return
    }

    const chosen = fits.filter((f) => picked.has(key(f)) && f.status !== 'unsupported')

    setSaving(true)
    saveVersions(facts, chosen, source, providerOf)
      .then(
        ({ count, name }) => {
          toast.success(`${count} ${count === 1 ? 'version' : 'versions'} saved`, {
            description: `In the folder "${name}" in Media.`,
          })
          setPicked(new Set())
        },
        (e) => toast.error('Could not save the versions', { description: errorMessage(e) }),
      )
      .then(() => setSaving(false))
  }

  return (
    <aside
      aria-label="Channels"
      data-testid="channels-panel"
      className="absolute top-0 right-0 bottom-0 z-[300] flex w-full max-w-sm flex-col border-l bg-background shadow-xl"
    >
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <LayoutTemplate className="size-4" aria-hidden />
        <h2 className="flex-1 text-sm font-semibold">Channels</h2>
        <Button size="icon" variant="ghost" aria-label="Check again" onClick={() => setRun((n) => n + 1)}>
          <RotateCw />
        </Button>
        <Button size="icon" variant="ghost" aria-label="Close channels" onClick={onClose}>
          <X />
        </Button>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4 text-sm">
        {error && <Callout tone="danger">{error}</Callout>}
        {error ? null : !facts || !providers.data ? (
          <p className="flex items-center gap-2 text-muted-foreground">
            <Loader2 className="size-4 animate-spin" aria-hidden /> Checking the picture...
          </p>
        ) : (
          <>
            <p className="text-muted-foreground">
              {facts.width} × {facts.height} · {formatBytes(facts.bytes)} ·{' '}
              {facts.mime === 'image/jpeg' ? 'JPG' : 'PNG'}
              {facts.alpha ? ' · see-through areas' : ''}. &ldquo;Can&apos;t post&rdquo; is a platform&apos;s limit;
              crops are only about the shape.
            </p>
            {[...byPlatform.entries()].map(([platform, rows]) => (
              <section key={platform} className="flex flex-col gap-1.5">
                <h3 className="text-xs font-medium text-muted-foreground uppercase">{platform}</h3>
                {rows.map((f) => (
                  <div key={key(f)} className="flex items-start gap-2 rounded-lg border p-2" data-testid="fit-row">
                    <Checkbox
                      aria-label={`Make a version for ${f.platform} ${f.name}`}
                      className="mt-0.5"
                      checked={picked.has(key(f))}
                      disabled={f.status === 'unsupported'}
                      onCheckedChange={(on) =>
                        setPicked((cur) => {
                          const next = new Set(cur)

                          if (on) {
                            next.add(key(f))
                          } else {
                            next.delete(key(f))
                          }

                          return next
                        })
                      }
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{f.name}</span>
                        <span className="font-mono text-xs text-muted-foreground">{f.label}</span>
                        <Badge
                          variant={f.status === 'ok' ? 'secondary' : 'outline'}
                          className={
                            f.status === 'unsupported'
                              ? 'text-red-700 dark:text-red-400'
                              : f.status === 'adjust'
                                ? 'text-amber-700 dark:text-amber-400'
                                : 'text-brand'
                          }
                        >
                          {f.status === 'ok'
                            ? 'Fits'
                            : f.status === 'adjust'
                              ? `Crops ${Math.round(f.crop * 100)}%`
                              : "Can't post"}
                        </Badge>
                      </div>
                      {f.reasons.length > 0 && (
                        <p className="mt-0.5 text-xs text-muted-foreground">{f.reasons.join(' · ')}</p>
                      )}
                    </div>
                    {f.status !== 'ok' && f.status !== 'unsupported' && (
                      <Button size="sm" variant="outline" onClick={() => setOptimizing(f)}>
                        Optimize
                      </Button>
                    )}
                    {f.status === 'unsupported' && f.reasons.some((r) => /Too (tall|wide)|Larger|Takes/.test(r)) && (
                      <Button size="sm" variant="outline" onClick={() => setOptimizing(f)}>
                        Fix
                      </Button>
                    )}
                  </div>
                ))}
              </section>
            ))}
          </>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2 border-t p-3">
        <span className="flex-1 text-xs text-muted-foreground">
          {picked.size
            ? `${picked.size} chosen: a cropped version each, to a folder in Media`
            : 'Choose placements to save a version for each'}
        </span>
        <Button disabled={!picked.size || saving || !facts} onClick={saveVariants} data-testid="save-variants">
          {saving ? 'Saving...' : 'Make versions'}
        </Button>
      </div>

      {optimizing && facts && (
        <OptimizeDialog
          facts={facts}
          fit={optimizing}
          provider={providerOf(optimizing)}
          canExpand={!!ai.data?.configured && ai.data.capabilities.expand}
          model={ai.data?.model ?? ''}
          onClose={() => setOptimizing(null)}
          onApply={async (blob, strategy: Strategy) => {
            await cmd.addVersion({
              blob,
              name: `${optimizing.platform} ${optimizing.name} ${optimizing.label.replace(':', 'x')} (${strategy}).${blob.type === 'image/jpeg' ? 'jpg' : 'png'}`,
            })
            toast.success(`Version for ${optimizing.platform} ${optimizing.name}`, {
              description: 'Beside the original in the dock below; save it like any picture.',
            })
            setOptimizing(null)
          }}
        />
      )}
    </aside>
  )
}
