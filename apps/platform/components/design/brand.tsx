'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Upload, X } from 'lucide-react'
import { useRef, useState } from 'react'
import { toast } from 'sonner'

import { NativeSelect } from '@/components/common/native-select'
import { Button } from '@/components/ui/button'
import { api, errorMessage, mediaThumbUrl, mediaUrl, type DesignBrand } from '@/lib/api'

import { newImage, newText, type El } from './doc'
import { FONTS } from './fonts'
import { ColorInput, dragProps, measure, Section, type PanelCtx } from './panels'

const KEY = ['design-brand']

/** The workspace's brand kit (shared: everyone editing media sees the same one). */
export const useBrand = () => useQuery({ queryKey: KEY, queryFn: api.designBrand, staleTime: 60_000 })

/** Colours, fonts and logos of the brand: a click puts one on the design, the kit changes for the workspace. */
export function BrandPanel({ ctx }: { ctx: PanelCtx }) {
  const qc = useQueryClient()
  const brand = useBrand()
  const [colour, setColour] = useState('#1f8a4c')
  const [font, setFont] = useState('')
  const [uploading, setUploading] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const save = useMutation({
    mutationFn: api.setDesignBrand,
    onMutate: (b) => qc.setQueryData(KEY, b),
    onError: (e) => {
      toast.error('Could not change the brand kit', { description: errorMessage(e) })
      void qc.invalidateQueries({ queryKey: KEY })
    },
    onSuccess: (b) => qc.setQueryData(KEY, b),
  })
  const b = brand.data
  const d = ctx.store.design

  if (!b) {
    return <Loader2 className="mx-auto animate-spin text-muted-foreground" />
  }

  const change = (next: Partial<DesignBrand>) => save.mutate({ ...b, ...next })
  const sel = ctx.selected.length ? ctx.page.elements.filter((e) => ctx.selected.includes(e.id)) : []
  const picture = sel.length === 1 && sel[0]!.type === 'image' ? sel[0] : undefined

  // A brand colour goes onto what is selected (text, shapes), or the page's background when nothing is.
  const paint = (c: string) => {
    if (!sel.length) {
      ctx.updatePage((p) => void (p.background = { kind: 'color', color: c }))

      return
    }

    ctx.updatePage((p) => {
      for (const e of p.elements) {
        if (ctx.selected.includes(e.id) && (e.type === 'text' || e.type === 'shape')) {
          e.fill = c

          if (e.type === 'shape') {
            e.gradient = undefined
          }
        }
      }
    })
  }

  const heading = (f: string): El => ({ ...newText(d, 'heading'), text: f, fontFamily: f })

  const logo = async (id: string) => {
    const { w, h } = await measure(mediaUrl(id))

    return newImage(d, mediaUrl(id), w, h, { mediaId: id })
  }

  return (
    <div className="flex flex-col gap-5 text-sm">
      <Section title="Colours">
        <div className="grid grid-cols-6 gap-1.5">
          {b.colors.map((c) => (
            <span key={c} className="group relative">
              <button
                type="button"
                aria-label={`Use ${c}`}
                title={sel.length ? 'Paint the selection' : 'Paint the page background'}
                onClick={() => paint(c)}
                className="aspect-square w-full rounded-md border"
                style={{ background: c }}
              />
              <button
                type="button"
                aria-label={`Remove ${c} from the brand`}
                onClick={() => change({ colors: b.colors.filter((x) => x !== c) })}
                className="absolute -top-1.5 -right-1.5 hidden size-4 items-center justify-center rounded-full border bg-background group-hover:flex focus-visible:flex"
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <ColorInput label="New brand colour" value={colour} onChange={setColour} />
          <Button
            variant="outline"
            size="sm"
            disabled={b.colors.includes(colour) || b.colors.length >= 24}
            onClick={() => change({ colors: [...b.colors, colour] })}
          >
            <Plus /> Add colour
          </Button>
        </div>
      </Section>
      <Section title="Fonts">
        <div className="flex flex-col gap-1.5">
          {b.fonts.map((f) => (
            <span key={f} className="group flex items-center gap-1">
              <Button
                variant="secondary"
                className="flex-1 justify-start text-lg"
                style={{ fontFamily: `"${f}"` }}
                onClick={() => ctx.add(heading(f))}
                {...dragProps(() => heading(f))}
              >
                {f}
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Remove ${f} from the brand`}
                onClick={() => change({ fonts: b.fonts.filter((x) => x !== f) })}
              >
                <X />
              </Button>
            </span>
          ))}
        </div>
        {b.fonts.length < 6 && (
          <NativeSelect
            aria-label="Add a brand font"
            value={font}
            onChange={(e) => {
              setFont('')

              if (e.target.value) {
                change({ fonts: [...b.fonts, e.target.value] })
              }
            }}
          >
            <option value="">Add a font...</option>
            {FONTS.filter((f) => !b.fonts.includes(f)).map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </NativeSelect>
        )}
      </Section>
      <Section title="Logos">
        <div className="grid grid-cols-3 gap-2">
          {b.logos.map((id) => (
            <span key={id} className="group relative">
              <button
                type="button"
                aria-label="Add the logo to the page"
                onClick={() => void logo(id).then((el) => ctx.add(el))}
                className="flex aspect-square w-full items-center justify-center rounded-lg border bg-muted/40 p-2 hover:ring-2 hover:ring-brand"
              >
                <img src={mediaThumbUrl(id, 160)} alt="" className="max-h-full max-w-full object-contain" />
              </button>
              <button
                type="button"
                aria-label="Remove the logo from the brand"
                onClick={() => change({ logos: b.logos.filter((x) => x !== id) })}
                className="absolute -top-1.5 -right-1.5 hidden size-4 items-center justify-center rounded-full border bg-background group-hover:flex focus-visible:flex"
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
        <input
          ref={input}
          type="file"
          accept="image/png,image/svg+xml,image/webp,image/jpeg"
          hidden
          onChange={(e) => {
            const f = e.target.files?.[0]

            e.target.value = ''

            if (!f) {
              return
            }

            setUploading(true)
            void api
              .uploadMedia(f, f.name)
              .then((m) => change({ logos: [...b.logos, m.id] }))
              .catch((err: unknown) => toast.error('Could not upload the logo', { description: errorMessage(err) }))
              .finally(() => setUploading(false))
          }}
        />
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={uploading || b.logos.length >= 12}
            onClick={() => input.current?.click()}
          >
            {uploading ? <Loader2 className="animate-spin" /> : <Upload />} Upload a logo
          </Button>
          {picture?.type === 'image' && picture.mediaId && !b.logos.includes(picture.mediaId) && (
            <Button
              variant="outline"
              size="sm"
              disabled={b.logos.length >= 12}
              onClick={() => change({ logos: [...b.logos, picture.mediaId!] })}
            >
              <Plus /> Add the selected picture
            </Button>
          )}
        </div>
      </Section>
    </div>
  )
}
