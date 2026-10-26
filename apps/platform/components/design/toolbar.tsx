'use client'

import {
  AlignCenter,
  AlignCenterHorizontal,
  AlignCenterVertical,
  AlignEndHorizontal,
  AlignEndVertical,
  AlignHorizontalDistributeCenter,
  AlignStartHorizontal,
  AlignStartVertical,
  AlignVerticalDistributeCenter,
  Group,
  LayoutGrid,
  Ungroup,
  AlignLeft,
  AlignRight,
  ArrowDownToLine,
  ArrowUpToLine,
  Bold,
  Check,
  Copy,
  Crop,
  Eraser,
  FlipHorizontal2,
  Files,
  FlipVertical2,
  Italic,
  Loader2,
  Lock,
  Shapes,
  SlidersHorizontal,
  Trash2,
  Underline,
  Unlock,
  WandSparkles,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { NativeSelect } from '@/components/common/native-select'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Toggle } from '@/components/ui/toggle'
import { api, errorMessage, type Media } from '@/lib/api'
import { cn } from '@/lib/utils'

import {
  align,
  bounds,
  distribute,
  iconSrc,
  NO_FILTERS,
  uid,
  type AlignTo,
  type El,
  type Fade,
  type ImageEl,
  type MaskKind,
  type ShapeEl,
  type TextEl,
} from './doc'
import { FONTS } from './fonts'
import { ColorInput, css, Elapsed, FillPicker, MoveToPageItems, swapPicture, type PanelCtx } from './panels'

function Slider({
  label,
  value,
  min,
  max,
  step = 1,
  onChange,
  format = String,
}: {
  label: string
  value: number
  min: number
  max: number
  step?: number
  onChange: (v: number) => void
  format?: (v: number) => string
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

function Pop({ label, icon, children }: { label: string; icon: ReactNode; children: ReactNode }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="sm">
          {icon} <span className="max-md:sr-only">{label}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className="flex w-64 flex-col gap-3">{children}</PopoverContent>
    </Popover>
  )
}

const Sep = () => <span className="mx-1 h-5 w-px shrink-0 bg-border" aria-hidden />

/** The bar over the canvas: what the selection can do, from its font to its order on the page. */
export function Toolbar({
  ctx,
  cropping,
  onCropping,
}: {
  ctx: PanelCtx
  cropping: string | null
  onCropping: (id: string | null) => void
}) {
  const { selected, onSelect, updatePage: update } = ctx
  const els = ctx.page.elements.filter((e) => selected.includes(e.id))
  const el = els.length === 1 ? els[0] : undefined

  if (!els.length) {
    return <p className="px-3 text-sm text-muted-foreground max-md:hidden">Pick something on the page to change it.</p>
  }

  /** Changes the selected elements; edits from one control in a row are one undo step. */
  const set = <T extends El>(fn: (e: T) => void, key?: string) =>
    update((p) => {
      for (const e of p.elements) {
        if (selected.includes(e.id)) {
          fn(e as T)
        }
      }
    }, key)

  const order = (top: boolean) =>
    update((p) => {
      const picked = p.elements.filter((e) => selected.includes(e.id))
      const rest = p.elements.filter((e) => !selected.includes(e.id))

      p.elements = top ? [...rest, ...picked] : [...picked, ...rest]
    })

  const duplicate = () => {
    const copies = els.map((e) => ({
      ...structuredClone(e),
      id: Math.random().toString(36).slice(2, 10),
      x: e.x + 24,
      y: e.y + 24,
      locked: false,
    }))

    update((p) => void p.elements.push(...copies))
    onSelect(copies.map((c) => c.id))
  }

  const locked = els.every((e) => e.locked)
  const grouped = els.some((e) => e.group)

  // Several: to the box around them; one: to the page.
  const lineUp = (to: AlignTo) =>
    update((p) => {
      const picked = p.elements.filter((e) => selected.includes(e.id) && !e.locked)

      align(picked, to, els.length > 1 ? bounds(els) : { x: 0, y: 0, ...ctx.store.design })
    })
  const spread = (axis: 'x' | 'y') =>
    update((p) =>
      distribute(
        p.elements.filter((e) => selected.includes(e.id)),
        axis,
      ),
    )

  return (
    <div className="flex min-w-0 items-center gap-0.5 overflow-x-auto px-2 [scrollbar-width:thin]">
      {el?.type === 'text' && <TextTools el={el} set={set} />}
      {el?.type === 'shape' && <ShapeTools el={el} set={set} />}
      {el?.type === 'image' && el.icon && (
        <ColorInput
          label="Icon colour"
          value={el.icon.color}
          onChange={(c) =>
            set<ImageEl>((x) => {
              if (x.icon) {
                x.icon.color = c
                x.src = iconSrc(x.icon.svg, c)
              }
            }, 'icon-colour')
          }
        />
      )}
      {el?.type === 'image' && !el.icon && (
        <ImageTools
          el={el}
          set={set}
          ctx={ctx}
          cropping={cropping === el.id}
          onCropping={(on) => onCropping(on ? el.id : null)}
        />
      )}
      {el && <Sep />}
      <Pop label="Look" icon={<SlidersHorizontal />}>
        {el && fadeable(el) && (
          <FadeControls fade={el.fade} onChange={(f, key) => set((x) => void (x.fade = f), key)} />
        )}
        <Slider
          label="Transparency"
          value={Math.round((1 - (el?.opacity ?? 1)) * 100)}
          min={0}
          max={100}
          format={(v) => `${v}%`}
          onChange={(v) => set((e) => void (e.opacity = 1 - v / 100), 'opacity')}
        />
        <label className="flex items-center justify-between text-sm">
          Shadow
          <input
            type="checkbox"
            className="accent-[var(--brand)]"
            checked={!!el?.shadow}
            onChange={(e) =>
              set(
                (x) =>
                  void (x.shadow = e.target.checked
                    ? { color: '#000000', blur: 24, x: 0, y: 8, opacity: 0.35 }
                    : undefined),
              )
            }
          />
        </label>
        {el?.shadow && (
          <>
            <Slider
              label="Blur"
              value={el.shadow.blur}
              min={0}
              max={100}
              onChange={(v) => set((x) => void (x.shadow!.blur = v), 'sh-blur')}
            />
            <Slider
              label="Distance"
              value={el.shadow.y}
              min={0}
              max={80}
              onChange={(v) => set((x) => void (x.shadow!.y = v), 'sh-y')}
            />
            <Slider
              label="Strength"
              value={Math.round(el.shadow.opacity * 100)}
              min={0}
              max={100}
              format={(v) => `${v}%`}
              onChange={(v) => set((x) => void (x.shadow!.opacity = v / 100), 'sh-o')}
            />
            <div className="flex items-center gap-2 text-sm">
              <ColorInput
                label="Shadow colour"
                value={el.shadow.color}
                onChange={(c) => set((x) => void (x.shadow!.color = c), 'sh-c')}
              />
              Colour
            </div>
          </>
        )}
      </Pop>
      <Pop label="Arrange" icon={<LayoutGrid />}>
        <span className="text-xs text-muted-foreground">
          {els.length > 1 ? 'Line up the selection' : 'Line up on the page'}
        </span>
        <div className="grid grid-cols-6 gap-1">
          {ALIGN.map((a) => (
            <Button
              key={a.to}
              variant="outline"
              size="icon-sm"
              aria-label={a.label}
              tooltip={a.label}
              onClick={() => lineUp(a.to)}
            >
              {a.icon}
            </Button>
          ))}
        </div>
        {els.length > 2 && (
          <div className="grid grid-cols-2 gap-1">
            <Button variant="outline" size="sm" onClick={() => spread('x')}>
              <AlignHorizontalDistributeCenter /> Space across
            </Button>
            <Button variant="outline" size="sm" onClick={() => spread('y')}>
              <AlignVerticalDistributeCenter /> Space down
            </Button>
          </div>
        )}
      </Pop>
      {(els.length > 1 || grouped) && (
        <Button
          variant="ghost"
          size="sm"
          title={grouped ? 'Ungroup (Ctrl+Shift+G)' : 'Group (Ctrl+G)'}
          onClick={() => {
            const id = uid()

            set((e) => void (e.group = grouped ? undefined : id))
          }}
        >
          {grouped ? <Ungroup /> : <Group />} <span className="max-md:sr-only">{grouped ? 'Ungroup' : 'Group'}</span>
        </Button>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm" title="Move to another page">
            <Files /> <span className="max-md:sr-only">Move to page</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <MoveToPageItems ctx={ctx} ids={selected} />
        </DropdownMenuContent>
      </DropdownMenu>
      <Sep />
      <Button
        variant="ghost"
        size="icon"
        aria-label="Bring to front"
        title="Bring to front"
        onClick={() => order(true)}
      >
        <ArrowUpToLine />
      </Button>
      <Button variant="ghost" size="icon" aria-label="Send to back" title="Send to back" onClick={() => order(false)}>
        <ArrowDownToLine />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        aria-label={locked ? 'Unlock' : 'Lock'}
        title={locked ? 'Unlock' : 'Lock'}
        onClick={() => set((e) => void (e.locked = !locked))}
      >
        {locked ? <Lock /> : <Unlock />}
      </Button>
      <Button variant="ghost" size="icon" aria-label="Duplicate" title="Duplicate (Ctrl+D)" onClick={duplicate}>
        <Copy />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Delete"
        title="Delete"
        className="text-red-600 dark:text-red-400"
        onClick={() => {
          update((p) => void (p.elements = p.elements.filter((e) => !selected.includes(e.id))))
          onSelect([])
        }}
      >
        <Trash2 />
      </Button>
    </div>
  )
}

/** One-click looks for a photo: the Adjust sliders set for it. */
const LOOKS: { name: string; filters: ImageEl['filters'] }[] = [
  { name: 'Original', filters: { ...NO_FILTERS } },
  { name: 'Vivid', filters: { ...NO_FILTERS, saturation: 0.6, contrast: 12 } },
  { name: 'Bright', filters: { ...NO_FILTERS, brightness: 0.1, contrast: 5 } },
  { name: 'Dramatic', filters: { ...NO_FILTERS, contrast: 35, saturation: -0.3, brightness: -0.05 } },
  { name: 'Mono', filters: { ...NO_FILTERS, grayscale: true, contrast: 15 } },
  { name: 'Muted', filters: { ...NO_FILTERS, saturation: -0.8, brightness: 0.04 } },
]

/** A CSS look close to the Konva filters, for the previews. */
const cssFilter = (f: ImageEl['filters']) =>
  `brightness(${1 + f.brightness}) contrast(${1 + f.contrast / 100}) saturate(${Math.max(0, 1 + f.saturation / 2)}) blur(${f.blur / 10}px)${f.grayscale ? ' grayscale(1)' : ''}`

const ALIGN: { to: AlignTo; label: string; icon: ReactNode }[] = [
  { to: 'left', label: 'Align left', icon: <AlignStartVertical /> },
  { to: 'center', label: 'Align centres', icon: <AlignCenterVertical /> },
  { to: 'right', label: 'Align right', icon: <AlignEndVertical /> },
  { to: 'top', label: 'Align top', icon: <AlignStartHorizontal /> },
  { to: 'middle', label: 'Align middles', icon: <AlignCenterHorizontal /> },
  { to: 'bottom', label: 'Align bottom', icon: <AlignEndHorizontal /> },
]

/** Pictures and filled shapes fade; text and lines don't. */
const fadeable = (e: El) => e.type === 'image' || (e.type === 'shape' && e.shape !== 'line' && e.shape !== 'arrow')

const FADE_SIDES: { side: Fade['side']; label: string; css: string }[] = [
  { side: 'left', label: 'From the left', css: 'to right' },
  { side: 'right', label: 'From the right', css: 'to left' },
  { side: 'top', label: 'From the top', css: 'to bottom' },
  { side: 'bottom', label: 'From the bottom', css: 'to top' },
]

/** Fading out towards one side: off, or which side and how far in. */
function FadeControls({ fade, onChange }: { fade?: Fade; onChange: (f: Fade | undefined, key?: string) => void }) {
  return (
    <div className="flex flex-col gap-2 border-b pb-3">
      <span className="text-xs text-muted-foreground">Fade to see-through</span>
      <div className="grid grid-cols-5 gap-1" role="group" aria-label="Fade">
        <button
          type="button"
          aria-pressed={!fade}
          title="No fade"
          onClick={() => onChange(undefined)}
          className="flex h-9 items-center justify-center rounded-md border text-xs aria-pressed:ring-2 aria-pressed:ring-brand"
        >
          None
        </button>
        {FADE_SIDES.map((f) => (
          <button
            key={f.side}
            type="button"
            aria-label={f.label}
            title={f.label}
            aria-pressed={fade?.side === f.side}
            onClick={() => onChange({ side: f.side, size: fade?.size ?? 0.5 })}
            className="h-9 rounded-md border aria-pressed:ring-2 aria-pressed:ring-brand"
            style={{ background: `linear-gradient(${f.css}, transparent, var(--foreground))` }}
          />
        ))}
      </div>
      {fade && (
        <Slider
          label="Fade length"
          value={Math.round(fade.size * 100)}
          min={5}
          max={100}
          format={(v) => `${v}%`}
          onChange={(v) => onChange({ ...fade, size: v / 100 }, 'fade')}
        />
      )}
    </div>
  )
}

type Setter = <T extends El>(fn: (e: T) => void, key?: string) => void

function TextTools({ el, set }: { el: TextEl; set: Setter }) {
  const st = set as (fn: (e: TextEl) => void, key?: string) => void

  return (
    <>
      <NativeSelect
        aria-label="Font"
        className="w-40 shrink-0"
        value={el.fontFamily}
        style={{ fontFamily: `"${el.fontFamily}"` }}
        onChange={(e) => st((t) => void (t.fontFamily = e.target.value))}
      >
        {FONTS.map((f) => (
          <option key={f} value={f}>
            {f}
          </option>
        ))}
      </NativeSelect>
      <input
        aria-label="Font size"
        type="number"
        min={4}
        max={1000}
        value={Math.round(el.fontSize)}
        onChange={(e) => +e.target.value >= 4 && st((t) => void (t.fontSize = +e.target.value), 'size')}
        className="h-8 w-16 shrink-0 rounded-lg border bg-transparent px-2 text-sm tabular-nums pointer-coarse:h-11"
      />
      <ColorInput label="Text colour" value={el.fill} onChange={(c) => st((t) => void (t.fill = c), 'fill')} />
      <Pop label="Effects" icon={<WandSparkles />}>
        <Slider
          label="Curve"
          value={el.curve ?? 0}
          min={-100}
          max={100}
          format={(v) => (v ? `${v > 0 ? 'Arch' : 'Smile'} ${Math.abs(v)}` : 'Straight')}
          onChange={(v) => st((t) => void (t.curve = v || undefined), 'curve')}
        />
        <label className="flex items-center justify-between text-sm">
          Gradient letters
          <input
            type="checkbox"
            className="accent-[var(--brand)]"
            checked={!!el.gradient}
            onChange={(e) =>
              st(
                (t) =>
                  void (t.gradient = e.target.checked
                    ? { kind: 'gradient', from: t.fill, to: '#db2777', angle: 0 }
                    : undefined),
              )
            }
          />
        </label>
        {el.gradient && (
          <div className="flex items-center gap-2">
            <ColorInput
              label="First colour"
              value={el.gradient.from}
              onChange={(c) => st((t) => void (t.gradient!.from = c), 'tg-from')}
            />
            <ColorInput
              label="Last colour"
              value={el.gradient.to}
              onChange={(c) => st((t) => void (t.gradient!.to = c), 'tg-to')}
            />
            <input
              type="range"
              aria-label="Gradient angle"
              min={0}
              max={360}
              value={el.gradient.angle}
              onChange={(e) => st((t) => void (t.gradient!.angle = +e.target.value), 'tg-angle')}
              className="min-w-0 flex-1 accent-[var(--brand)]"
            />
          </div>
        )}
      </Pop>
      <Sep />
      <Toggle size="sm" aria-label="Bold" pressed={el.bold} onPressedChange={(v) => st((t) => void (t.bold = v))}>
        <Bold />
      </Toggle>
      <Toggle size="sm" aria-label="Italic" pressed={el.italic} onPressedChange={(v) => st((t) => void (t.italic = v))}>
        <Italic />
      </Toggle>
      <Toggle
        size="sm"
        aria-label="Underline"
        pressed={el.underline}
        onPressedChange={(v) => st((t) => void (t.underline = v))}
      >
        <Underline />
      </Toggle>
      <Button
        variant="ghost"
        size="icon"
        aria-label={`Align ${el.align}`}
        title="Alignment"
        onClick={() =>
          st((t) => void (t.align = t.align === 'left' ? 'center' : t.align === 'center' ? 'right' : 'left'))
        }
      >
        {el.align === 'left' ? <AlignLeft /> : el.align === 'center' ? <AlignCenter /> : <AlignRight />}
      </Button>
      <Pop
        label="Box"
        icon={
          <span className={cn('rounded-sm px-1 text-xs font-semibold', el.box ? 'bg-brand-soft text-brand' : 'border')}>
            A
          </span>
        }
      >
        <label className="flex items-center justify-between text-sm">
          A box behind the text
          <input
            type="checkbox"
            className="accent-[var(--brand)]"
            checked={!!el.box}
            onChange={(e) =>
              st(
                (t) =>
                  void (t.box = e.target.checked
                    ? { color: '#fde047', padding: Math.round(t.fontSize * 0.3), radius: Math.round(t.fontSize * 0.2) }
                    : undefined),
              )
            }
          />
        </label>
        {el.box && (
          <>
            <div className="flex items-center gap-2 text-sm">
              <ColorInput
                label="Box colour"
                value={el.box.color}
                onChange={(c) => st((t) => void (t.box!.color = c), 'box-c')}
              />
              Colour
            </div>
            <Slider
              label="Padding"
              value={Math.round(el.box.padding)}
              min={0}
              max={Math.round(el.fontSize * 2)}
              onChange={(v) => st((t) => void (t.box!.padding = v), 'box-p')}
            />
            <Slider
              label="Corner radius"
              value={Math.round(el.box.radius)}
              min={0}
              max={Math.round(el.fontSize * 2)}
              onChange={(v) => st((t) => void (t.box!.radius = v), 'box-r')}
            />
          </>
        )}
      </Pop>
      <Pop label="Spacing" icon={<span className="text-xs font-semibold">A|A</span>}>
        <Slider
          label="Letter spacing"
          value={el.letterSpacing}
          min={-10}
          max={60}
          onChange={(v) => st((t) => void (t.letterSpacing = v), 'ls')}
        />
        <Slider
          label="Line height"
          value={el.lineHeight}
          min={0.6}
          max={3}
          step={0.05}
          format={(v) => v.toFixed(2)}
          onChange={(v) => st((t) => void (t.lineHeight = v), 'lh')}
        />
        <Slider
          label="Outline"
          value={el.stroke?.width ?? 0}
          min={0}
          max={30}
          onChange={(v) => st((t) => void (t.stroke = { color: t.stroke?.color ?? '#000000', width: v }), 'stroke')}
        />
        {!!el.stroke?.width && (
          <div className="flex items-center gap-2 text-sm">
            <ColorInput
              label="Outline colour"
              value={el.stroke.color}
              onChange={(c) => st((t) => void (t.stroke = { width: t.stroke?.width ?? 2, color: c }), 'stroke-c')}
            />
            Outline colour
          </div>
        )}
      </Pop>
    </>
  )
}

function ShapeTools({ el, set }: { el: ShapeEl; set: Setter }) {
  const st = set as (fn: (e: ShapeEl) => void, key?: string) => void
  const line = el.shape === 'line' || el.shape === 'arrow'

  return (
    <>
      {line ? (
        <ColorInput label="Colour" value={el.fill} onChange={(c) => st((s) => void (s.fill = c), 'fill')} />
      ) : (
        <Popover>
          <PopoverTrigger asChild>
            <button
              type="button"
              aria-label="Fill"
              title="Fill: a colour or a gradient"
              className="size-8 shrink-0 rounded-md border shadow-xs pointer-coarse:size-11"
              style={{ background: css(el.gradient ?? { kind: 'color', color: el.fill }) }}
            />
          </PopoverTrigger>
          <PopoverContent className="max-h-[70vh] w-80 overflow-y-auto">
            <FillPicker
              value={el.gradient ?? { kind: 'color', color: el.fill }}
              onChange={(f, key) =>
                st((s) => {
                  if (f.kind === 'color') {
                    s.fill = f.color
                    s.gradient = undefined
                  } else {
                    s.gradient = f
                  }
                }, key)
              }
            />
          </PopoverContent>
        </Popover>
      )}
      {!line && (
        <ColorInput
          label="Border colour"
          value={el.stroke}
          onChange={(c) => st((s) => void (s.stroke = c), 'stroke')}
        />
      )}
      <Pop
        label={line ? 'Thickness' : 'Border'}
        icon={<span className="block size-4 rounded-sm border-2 border-current" />}
      >
        <Slider
          label={line ? 'Thickness' : 'Border width'}
          value={el.strokeWidth}
          min={line ? 1 : 0}
          max={line ? 80 : 60}
          onChange={(v) => st((s) => void (s.strokeWidth = v), 'sw')}
        />
        {el.shape === 'rect' && (
          <Slider
            label="Corner radius"
            value={el.radius}
            min={0}
            max={Math.round(Math.min(el.width, el.height) / 2)}
            onChange={(v) => st((s) => void (s.radius = v), 'radius')}
          />
        )}
      </Pop>
    </>
  )
}

const MASKS: { kind: MaskKind | undefined; label: string; path: string }[] = [
  { kind: undefined, label: 'No shape', path: 'M3 3h18v18H3Z' },
  { kind: 'circle', label: 'Circle', path: 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Z' },
  { kind: 'arch', label: 'Arch', path: 'M3 22V11a9 9 0 0 1 18 0v11Z' },
  { kind: 'hexagon', label: 'Hexagon', path: 'M7 2h10l5 10-5 10H7L2 12Z' },
  { kind: 'triangle', label: 'Triangle', path: 'M12 2l10 20H2Z' },
  { kind: 'star', label: 'Star', path: 'm12 2 3 6.5 7 .8-5.2 4.8 1.4 7L12 17.6 5.8 21l1.4-7L2 9.3l7-.8Z' },
  { kind: 'heart', label: 'Heart', path: 'M12 21C5 15 2 12 2 8a5 5 0 0 1 10-1 5 5 0 0 1 10 1c0 4-3 7-10 13Z' },
]

/** Cuts the background out on the device and puts the result in the library; the page shows it in place. */
async function cutOut(el: ImageEl, onProgress: (share: number) => void): Promise<Media> {
  const { removeBackground } = await import('./background')
  const src = await (await fetch(el.src)).blob()
  const out = await removeBackground(src, onProgress)

  return api.uploadMedia(out, 'Cut out.png')
}

function ImageTools({
  el,
  set,
  ctx,
  cropping,
  onCropping,
}: {
  el: ImageEl
  set: Setter
  ctx: PanelCtx
  cropping: boolean
  onCropping: (on: boolean) => void
}) {
  const st = set as (fn: (e: ImageEl) => void, key?: string) => void
  const f = el.filters
  const any = f.brightness || f.contrast || f.saturation || f.blur || f.grayscale
  const [cutting, setCutting] = useState<number | null>(null)

  const removeBg = () => {
    setCutting(0)
    void cutOut(el, setCutting)
      .then((media) => {
        swapPicture(ctx, el.id, media)
        toast.success('Background removed', {
          description: 'The cut-out is in your library too. Undo brings the photo back.',
        })
      })
      .catch((e: unknown) => toast.error('Could not remove the background', { description: errorMessage(e) }))
      .finally(() => setCutting(null))
  }

  if (cropping) {
    return (
      <>
        <span className="px-2 text-sm text-muted-foreground max-md:hidden">Drag the photo to move it in its frame</span>
        <label className="flex items-center gap-2 px-2 text-sm">
          Zoom
          <input
            type="range"
            aria-label="Zoom"
            min={1}
            max={8}
            step={0.01}
            value={el.crop?.zoom ?? 1}
            onChange={(e) =>
              st(
                (i) => void (i.crop = { cx: i.crop?.cx ?? 0.5, cy: i.crop?.cy ?? 0.5, zoom: +e.target.value }),
                'crop-zoom',
              )
            }
            className="w-28 accent-[var(--brand)]"
          />
        </label>
        <Button variant="ghost" size="sm" onClick={() => st((i) => void (i.crop = undefined))}>
          Reset
        </Button>
        <Button size="sm" onClick={() => onCropping(false)}>
          <Check /> Done
        </Button>
      </>
    )
  }

  return (
    <>
      <Button variant="ghost" size="sm" onClick={() => onCropping(true)} title="Crop (double-click the photo)">
        <Crop /> <span className="max-md:sr-only">Crop</span>
      </Button>
      <Pop label="Shape" icon={<Shapes className={cn(el.mask && 'text-brand')} />}>
        <div className="grid grid-cols-4 gap-2">
          {MASKS.map((m) => (
            <button
              key={m.label}
              type="button"
              aria-label={m.label}
              title={m.label}
              aria-pressed={el.mask === m.kind}
              onClick={() => st((i) => void (i.mask = m.kind))}
              className={cn(
                'flex aspect-square items-center justify-center rounded-md border hover:bg-muted',
                el.mask === m.kind && 'border-brand bg-brand-soft text-brand',
              )}
            >
              <svg viewBox="0 0 24 24" className="size-6 fill-current" aria-hidden>
                <path d={m.path} />
              </svg>
            </button>
          ))}
        </div>
      </Pop>
      <Button
        variant="ghost"
        size="sm"
        onClick={removeBg}
        disabled={cutting !== null}
        title="Remove the background (on this device)"
      >
        {cutting !== null ? <Loader2 className="animate-spin" /> : <Eraser />}
        <span className="max-md:sr-only">
          {cutting === null ? (
            'Remove background'
          ) : cutting > 0 && cutting < 1 ? (
            `Downloading the model ${Math.round(cutting * 100)}%`
          ) : (
            <>
              Cutting out, <Elapsed />
            </>
          )}
        </span>
      </Button>
      <Pop label="Adjust" icon={<SlidersHorizontal className={cn(any && 'text-brand')} />}>
        <div className="grid grid-cols-3 gap-1.5" role="group" aria-label="Looks">
          {LOOKS.map((l) => (
            <button
              key={l.name}
              type="button"
              aria-pressed={JSON.stringify(f) === JSON.stringify(l.filters)}
              onClick={() => st((i) => void (i.filters = { ...l.filters }))}
              className="flex flex-col items-center gap-1 rounded-md border p-1 text-[11px] aria-pressed:ring-2 aria-pressed:ring-brand"
            >
              <img
                src={el.src}
                alt=""
                draggable={false}
                className="aspect-square w-full rounded-sm object-cover"
                style={{ filter: cssFilter(l.filters) }}
              />
              {l.name}
            </button>
          ))}
        </div>
        <Slider
          label="Brightness"
          value={f.brightness}
          min={-0.5}
          max={0.5}
          step={0.01}
          format={(v) => `${Math.round(v * 200)}`}
          onChange={(v) => st((i) => void (i.filters.brightness = v), 'br')}
        />
        <Slider
          label="Contrast"
          value={f.contrast}
          min={-50}
          max={50}
          onChange={(v) => st((i) => void (i.filters.contrast = v), 'ct')}
        />
        <Slider
          label="Saturation"
          value={f.saturation}
          min={-2}
          max={2}
          step={0.05}
          format={(v) => `${Math.round(v * 50)}`}
          onChange={(v) => st((i) => void (i.filters.saturation = v), 'sat')}
        />
        <Slider
          label="Blur"
          value={f.blur}
          min={0}
          max={40}
          onChange={(v) => st((i) => void (i.filters.blur = v), 'blur')}
        />
        <label className="flex items-center justify-between text-sm">
          Black and white
          <input
            type="checkbox"
            className="accent-[var(--brand)]"
            checked={f.grayscale}
            onChange={(e) => st((i) => void (i.filters.grayscale = e.target.checked))}
          />
        </label>
        {!!any && (
          <Button variant="outline" size="sm" onClick={() => st((i) => void (i.filters = { ...NO_FILTERS }))}>
            Reset
          </Button>
        )}
      </Pop>
      <Pop label="Corners" icon={<span className="block size-4 rounded-md border-2 border-current" />}>
        <Slider
          label="Corner radius"
          value={el.radius}
          min={0}
          max={Math.round(Math.min(el.width, el.height) / 2)}
          onChange={(v) => st((i) => void (i.radius = v), 'radius')}
        />
      </Pop>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Flip horizontally"
        title="Flip horizontally"
        onClick={() => st((i) => void (i.flipX = !i.flipX))}
      >
        <FlipHorizontal2 />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Flip vertically"
        title="Flip vertically"
        onClick={() => st((i) => void (i.flipY = !i.flipY))}
      >
        <FlipVertical2 />
      </Button>
    </>
  )
}
