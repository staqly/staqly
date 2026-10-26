'use client'

import Konva from 'konva'
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react'
import {
  Arrow,
  Ellipse,
  Group,
  Image as KImage,
  Label,
  Layer,
  Line,
  Rect,
  Stage,
  Tag,
  Text,
  TextPath,
  Transformer,
} from 'react-konva'

import {
  cropRect,
  curvePath,
  type Design,
  type El,
  type Fade,
  type Fill,
  type ImageEl,
  type MaskKind,
  type Page,
  type ShapeEl,
  type TextEl,
} from './doc'
import { loadFont } from './fonts'
import { snap } from './snap'

const PAD = 48
const SNAP_PX = 6
// The brand green (--brand in light mode) for the selection; guides take magenta, which shows on any photo.
const SELECT = '#1f8a4c'
const GUIDE = '#e11d74'

// Pictures load once per address and stay loaded: switching pages, or saving every page, draws them at once.
const images = new Map<string, Promise<HTMLImageElement>>()
const ready = new Map<string, HTMLImageElement>()

export function loadImage(src: string): Promise<HTMLImageElement> {
  let p = images.get(src)

  if (!p) {
    p = new Promise((resolve, reject) => {
      const i = new window.Image()

      // Library files are same-origin and Unsplash's CDN allows any origin: the canvas stays exportable.
      i.crossOrigin = 'anonymous'
      i.addEventListener('load', () => {
        ready.set(src, i)
        resolve(i)
      })
      i.addEventListener('error', () => {
        images.delete(src)
        reject(new Error('The picture did not load'))
      })
      i.src = src
    })
    images.set(src, p)
  }

  return p
}

/** Everything a page needs to draw: its pictures and fonts. */
export function preloadPage(page: Page) {
  return Promise.all(
    page.elements.map((e) =>
      e.type === 'image'
        ? loadImage(e.src).catch(() => undefined)
        : e.type === 'text'
          ? loadFont(e.fontFamily, e.bold)
          : undefined,
    ),
  )
}

function useImage(src: string) {
  // In state, not read from the cache at render: the React Compiler would keep the first (empty) read.
  const [state, setState] = useState(() => ({ src, img: ready.get(src) ?? null }))

  useEffect(() => {
    let live = true

    void loadImage(src).then(
      (img) => live && setState({ src, img }),
      () => live && setState({ src, img: null }),
    )

    return () => {
      live = false
    }
  }, [src])

  return state.src === src ? state.img : null
}

/** Konva's paint for a fill over a w x h box whose top left corner is at (x, y) in the node's own coordinates. */
export function fillProps(fill: Fill, w: number, h: number, x = 0, y = 0) {
  if (fill.kind === 'color') {
    return { fill: fill.color }
  }

  const stops = fill.via ? [0, fill.from, 0.5, fill.via, 1, fill.to] : [0, fill.from, 1, fill.to]
  const centre = { x: x + w / 2, y: y + h / 2 }

  if (fill.radial) {
    return {
      fillRadialGradientStartPoint: centre,
      fillRadialGradientEndPoint: centre,
      fillRadialGradientStartRadius: 0,
      fillRadialGradientEndRadius: Math.hypot(w, h) / 2,
      fillRadialGradientColorStops: stops,
    }
  }

  const a = (fill.angle * Math.PI) / 180
  const dx = (Math.cos(a) * w) / 2
  const dy = (Math.sin(a) * h) / 2

  return {
    fillLinearGradientStartPoint: { x: centre.x - dx, y: centre.y - dy },
    fillLinearGradientEndPoint: { x: centre.x + dx, y: centre.y + dy },
    fillLinearGradientColorStops: stops,
  }
}

/** A Konva filter: the pixels turn see-through towards the node's `fade` side. */
function fadeFilter(this: Konva.Node, d: ImageData) {
  const f = this.getAttr('fade') as Fade | undefined

  if (!f) {
    return
  }

  const { width: w, height: h, data } = d
  const reach = Math.max(1, (f.side === 'left' || f.side === 'right' ? w : h) * f.size)

  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const t = f.side === 'left' ? x : f.side === 'right' ? w - 1 - x : f.side === 'top' ? y : h - 1 - y

      if (t < reach) {
        data[(y * w + x) * 4 + 3]! *= t / reach
      }
    }
  }
}

/**
 * Draws its children faded (see fadeFilter): a filter needs the group cached, and cached again whenever what it
 * draws changes (`look`).
 */
function Faded({ fade, look, children }: { fade?: Fade; look: string; children: ReactNode }) {
  const ref = useRef<Konva.Group>(null)

  useEffect(() => {
    const g = ref.current

    if (!g || !fade) {
      return
    }

    g.setAttr('look', look)
    g.cache()

    return () => void g.clearCache()
  }, [fade, look])

  return fade ? (
    <Group ref={ref} filters={[fadeFilter]} fade={fade}>
      {children}
    </Group>
  ) : (
    children
  )
}

function shadowProps(el: El) {
  return el.shadow
    ? {
        shadowColor: el.shadow.color,
        shadowBlur: el.shadow.blur,
        shadowOffsetX: el.shadow.x,
        shadowOffsetY: el.shadow.y,
        shadowOpacity: el.shadow.opacity,
      }
    : {}
}

function starPoints(w: number, h: number) {
  const pts: number[] = []

  for (let i = 0; i < 10; i++) {
    const r = i % 2 ? 0.4 : 1
    const a = (Math.PI / 5) * i - Math.PI / 2

    pts.push(w / 2 + (Math.cos(a) * r * w) / 2, h / 2 + (Math.sin(a) * r * h) / 2 + h * 0.05)
  }

  return pts
}

function polygon(ctx: Konva.Context, pts: number[]) {
  ctx.moveTo(pts[0]!, pts[1]!)

  for (let i = 2; i < pts.length; i += 2) {
    ctx.lineTo(pts[i]!, pts[i + 1]!)
  }

  ctx.closePath()
}

/** Draws a mask's outline in a w x h box, for clipping. */
function maskPath(ctx: Konva.Context, kind: MaskKind, w: number, h: number) {
  ctx.beginPath()

  switch (kind) {
    case 'circle':
      ctx.ellipse(w / 2, h / 2, w / 2, h / 2, 0, 0, Math.PI * 2)
      break
    case 'triangle':
      polygon(ctx, [w / 2, 0, w, h, 0, h])
      break
    case 'star':
      polygon(ctx, starPoints(w, h))
      break
    case 'hexagon':
      polygon(ctx, [w / 4, 0, (3 * w) / 4, 0, w, h / 2, (3 * w) / 4, h, w / 4, h, 0, h / 2])
      break
    case 'heart':
      ctx.moveTo(w / 2, h * 0.28)
      ctx.bezierCurveTo(w * 0.5, h * 0.05, w * 0.05, h * -0.02, w * 0.02, h * 0.3)
      ctx.bezierCurveTo(0, h * 0.55, w * 0.3, h * 0.75, w / 2, h)
      ctx.bezierCurveTo(w * 0.7, h * 0.75, w, h * 0.55, w * 0.98, h * 0.3)
      ctx.bezierCurveTo(w * 0.95, h * -0.02, w * 0.5, h * 0.05, w / 2, h * 0.28)
      ctx.closePath()
      break
    case 'arch':
      ctx.moveTo(0, h)
      ctx.lineTo(0, w / 2)
      ctx.arc(w / 2, w / 2, w / 2, Math.PI, 0)
      ctx.lineTo(w, h)
      ctx.closePath()
      break
  }
}

function ImageNode({ el }: { el: ImageEl }) {
  const img = useImage(el.src)
  const ref = useRef<Konva.Image>(null)
  const f = el.filters
  const filtered = !!(f.brightness || f.contrast || f.saturation || f.blur || f.grayscale)

  // A cached image keeps its old pixels: it is cached again for every look it is drawn with.
  const look = [filtered, f.brightness, f.contrast, f.saturation, f.blur, f.grayscale, el.width, el.height, el.radius]
    .concat(el.crop ? [el.crop.cx, el.crop.cy, el.crop.zoom] : [])
    .join('|')

  useEffect(() => {
    const node = ref.current

    if (!node || !img) {
      return
    }

    node.setAttr('look', look)

    if (filtered) {
      node.cache()
    } else {
      node.clearCache()
    }
  }, [img, filtered, look])

  if (!img) {
    return <Rect width={el.width} height={el.height} fill="#e5e7eb" cornerRadius={el.radius} />
  }

  const picture = (
    <KImage
      ref={ref}
      image={img}
      crop={cropRect(img.naturalWidth, img.naturalHeight, el.width, el.height, el.crop)}
      width={el.width}
      height={el.height}
      cornerRadius={el.mask ? 0 : el.radius}
      scaleX={el.flipX ? -1 : 1}
      scaleY={el.flipY ? -1 : 1}
      x={el.flipX ? el.width : 0}
      y={el.flipY ? el.height : 0}
      filters={
        filtered
          ? [
              Konva.Filters.Brighten,
              Konva.Filters.Contrast,
              Konva.Filters.HSL,
              Konva.Filters.Blur,
              ...(f.grayscale ? [Konva.Filters.Grayscale] : []),
            ]
          : []
      }
      brightness={f.brightness}
      contrast={f.contrast}
      saturation={f.saturation}
      blurRadius={f.blur}
      {...(el.mask ? {} : shadowProps(el))}
    />
  )

  return (
    <Faded fade={el.fade} look={`${look}|${el.mask}|${el.flipX}|${el.flipY}|${img.src}`}>
      {el.mask ? (
        <Group clipFunc={(ctx) => maskPath(ctx, el.mask!, el.width, el.height)}>
          {/* The box the transformer and clicks use: the masked picture alone has holes. */}
          <Rect width={el.width} height={el.height} fill="transparent" />
          {picture}
        </Group>
      ) : (
        picture
      )}
    </Faded>
  )
}

function ShapeNode({ el }: { el: ShapeEl }) {
  const line = el.shape === 'line' || el.shape === 'arrow'

  return line ? (
    <ShapeDraw el={el} />
  ) : (
    <Faded fade={el.fade} look={JSON.stringify(el)}>
      <ShapeDraw el={el} />
    </Faded>
  )
}

function ShapeDraw({ el }: { el: ShapeEl }) {
  const { width: w, height: h } = el
  const paint = (x = 0, y = 0) => (el.gradient ? fillProps(el.gradient, w, h, x, y) : { fill: el.fill })
  const common = {
    ...paint(),
    stroke: el.strokeWidth ? el.stroke : undefined,
    strokeWidth: el.strokeWidth,
    ...shadowProps(el),
  }

  switch (el.shape) {
    case 'rect':
      return <Rect width={w} height={h} cornerRadius={el.radius} {...common} />
    case 'ellipse':
      return <Ellipse x={w / 2} y={h / 2} radiusX={w / 2} radiusY={h / 2} {...common} {...paint(-w / 2, -h / 2)} />
    case 'triangle':
      return <Line points={[w / 2, 0, w, h, 0, h]} closed lineJoin="round" {...common} />
    case 'star':
      return <Line points={starPoints(w, h)} closed lineJoin="round" {...common} />
    case 'line':
    case 'arrow': {
      const sw = Math.max(1, el.strokeWidth)
      const line = {
        points: [0, h / 2, w, h / 2],
        stroke: el.fill,
        strokeWidth: sw,
        lineCap: 'round' as const,
        ...shadowProps(el),
      }

      return (
        <>
          {/* The box the transformer and clicks use: the line alone is too thin to grab. */}
          <Rect width={w} height={h} fill="transparent" />
          {el.shape === 'arrow' ? (
            <Arrow {...line} fill={el.fill} pointerLength={sw * 3} pointerWidth={sw * 3} />
          ) : (
            <Line {...line} />
          )}
        </>
      )
    }
  }
}

function TextNode({ el }: { el: TextEl }) {
  const paint = el.gradient ? fillProps(el.gradient, el.width, el.fontSize * el.lineHeight) : { fill: el.fill }

  if (el.curve) {
    return (
      <>
        {/* The box the transformer and clicks use: the letters alone are hard to grab. */}
        <Rect width={el.width} height={el.height} fill="transparent" />
        <TextPath
          text={el.text.replaceAll('\n', ' ')}
          data={curvePath(el.width, el.curve, el.fontSize)}
          fontFamily={`"${el.fontFamily}"`}
          fontSize={el.fontSize}
          fontStyle={`${el.italic ? 'italic ' : ''}${el.bold ? 'bold' : 'normal'}`}
          textDecoration={el.underline ? 'underline' : ''}
          align="center"
          letterSpacing={el.letterSpacing}
          stroke={el.stroke?.width ? el.stroke.color : undefined}
          strokeWidth={el.stroke?.width ?? 0}
          fillAfterStrokeEnabled
          {...paint}
          {...shadowProps(el)}
        />
      </>
    )
  }

  const text = (
    <Text
      text={el.text}
      width={el.width}
      padding={el.box?.padding ?? 0}
      fontFamily={`"${el.fontFamily}"`}
      fontSize={el.fontSize}
      fontStyle={`${el.italic ? 'italic ' : ''}${el.bold ? 'bold' : 'normal'}`}
      textDecoration={el.underline ? 'underline' : ''}
      {...paint}
      align={el.align}
      lineHeight={el.lineHeight}
      letterSpacing={el.letterSpacing}
      stroke={el.stroke?.width ? el.stroke.color : undefined}
      strokeWidth={el.stroke?.width ?? 0}
      fillAfterStrokeEnabled
      {...(el.box ? {} : shadowProps(el))}
    />
  )

  // A box behind the text: Konva's label sizes its tag to the text and its padding.
  return el.box ? (
    <Label>
      <Tag fill={el.box.color} cornerRadius={el.box.radius} {...shadowProps(el)} />
      {text}
    </Label>
  ) : (
    text
  )
}

function ElementNode({ el }: { el: El }) {
  return el.type === 'text' ? <TextNode el={el} /> : el.type === 'image' ? <ImageNode el={el} /> : <ShapeNode el={el} />
}

/** Draws a page without any editing: template and page previews. */
export function DesignView({
  design,
  page,
  scale,
  stageRef,
}: {
  design: Pick<Design, 'width' | 'height'>
  page: Page
  scale: number
  stageRef?: RefObject<Konva.Stage | null>
}) {
  return (
    <Stage ref={stageRef} width={design.width * scale} height={design.height * scale} listening={false}>
      <Layer scaleX={scale} scaleY={scale}>
        <Rect
          width={design.width}
          height={design.height}
          {...fillProps(page.background, design.width, design.height)}
        />
        {page.elements.map((el) =>
          el.hidden ? null : (
            <Group key={el.id} x={el.x} y={el.y} rotation={el.rotation} opacity={el.opacity}>
              <ElementNode el={el} />
            </Group>
          ),
        )}
      </Layer>
    </Stage>
  )
}

interface Props {
  design: Design
  page: Page
  updatePage: (fn: (p: Page) => void, coalesce?: string) => void
  selected: string[]
  onSelect: (ids: string[]) => void
  /** The picture whose crop is being moved (dragging pans it inside its frame), or null. */
  cropping: string | null
  onCropping: (id: string | null) => void
  zoom: number | 'fit'
  onFitScale: (scale: number) => void
  /** Elements dragged in from the side panel, already placed where they were dropped. */
  onDropEls: (els: El[]) => void
}

/** What the side panel is dragging onto the page: a native drag carries strings, not elements. */
export const dragged: { make: (() => El | El[]) | null } = { make: null }

export function DesignCanvas({
  design,
  page,
  updatePage,
  selected,
  onSelect,
  cropping,
  onCropping,
  zoom,
  onFitScale,
  onDropEls,
}: Props) {
  const box = useRef<HTMLDivElement>(null)
  const stage = useRef<Konva.Stage>(null)
  const pageNode = useRef<Konva.Group>(null)
  const tr = useRef<Konva.Transformer>(null)
  const [view, setView] = useState({ w: 800, h: 600 })
  const [guides, setGuides] = useState<{ v: number[]; h: number[] }>({ v: [], h: [] })
  const [editing, setEditing] = useState<string | null>(null)
  const [, setFontTick] = useState(0)
  const elements = page.elements

  useLayoutEffect(() => {
    const el = box.current

    if (!el) {
      return
    }

    const ro = new ResizeObserver(() => setView({ w: el.clientWidth, h: el.clientHeight }))

    ro.observe(el)

    return () => ro.disconnect()
  }, [])

  const fit = Math.max(0.02, Math.min((view.w - PAD * 2) / design.width, (view.h - PAD * 2) / design.height))
  const scale = zoom === 'fit' ? fit : zoom
  const stageW = Math.max(view.w, design.width * scale + PAD * 2)
  const stageH = Math.max(view.h, design.height * scale + PAD * 2)
  const ox = (stageW - design.width * scale) / 2
  const oy = (stageH - design.height * scale) / 2

  useEffect(() => onFitScale(fit), [fit, onFitScale])

  // Canvas text measures with whatever font is there: draw again once each font of the page has loaded.
  const familyKey = [
    ...new Set(elements.flatMap((e) => (e.type === 'text' ? [`${e.fontFamily}|${e.bold}`] : []))),
  ].join(',')

  useEffect(() => {
    void Promise.all(
      familyKey
        .split(',')
        .filter(Boolean)
        .map((f) => loadFont(f.split('|')[0]!, f.endsWith('true'))),
    ).then(() => setFontTick((n) => n + 1))
  }, [familyKey])

  useEffect(() => {
    const t = tr.current

    if (!t || !stage.current) {
      return
    }

    const nodes = selected
      .filter((id) => id !== editing && id !== cropping)
      .map((id) => stage.current!.findOne(`#${id}`))
      .filter((n): n is Konva.Node => !!n)

    t.nodes(nodes)
    t.getLayer()?.batchDraw()
    // oxlint-disable-next-line react/exhaustive-effect-dependencies -- a changed page can remount the selected nodes
  }, [selected, page, editing, cropping])

  const byId = (id: string) => elements.find((e) => e.id === id)
  const one = selected.length === 1 ? byId(selected[0]!) : undefined
  // Corners keep the shape of texts; pictures and shapes stretch (a picture crops to its new frame).
  const keepRatio = selected.some((id) => byId(id)?.type === 'text')

  const select = (id: string, e: Konva.KonvaEventObject<MouseEvent | TouchEvent>) => {
    const multi = e.evt.shiftKey || e.evt.metaKey || e.evt.ctrlKey

    onSelect(multi ? (selected.includes(id) ? selected.filter((s) => s !== id) : [...selected, id]) : [id])
  }

  /** Dragging in crop mode moves the picture inside its frame; the frame stays. */
  const panCrop = (node: Konva.Node, el: ImageEl) => {
    const img = ready.get(el.src)
    const dx = node.x() - el.x
    const dy = node.y() - el.y

    node.position({ x: el.x, y: el.y })

    if (!img) {
      return
    }

    const r = cropRect(img.naturalWidth, img.naturalHeight, el.width, el.height, el.crop)
    const k = r.width / el.width
    const cx = (r.x + r.width / 2 - dx * k * (el.flipX ? -1 : 1)) / img.naturalWidth
    const cy = (r.y + r.height / 2 - dy * k * (el.flipY ? -1 : 1)) / img.naturalHeight
    // Kept where the picture still covers the frame.
    const hw = r.width / 2 / img.naturalWidth
    const hh = r.height / 2 / img.naturalHeight

    updatePage((p) => {
      const t = p.elements.find((x) => x.id === el.id)

      if (t?.type === 'image') {
        t.crop = {
          zoom: t.crop?.zoom ?? 1,
          cx: Math.min(1 - hw, Math.max(hw, cx)),
          cy: Math.min(1 - hh, Math.max(hh, cy)),
        }
      }
    }, 'crop')
  }

  const onDragMove = (e: Konva.KonvaEventObject<DragEvent>) => {
    const node = e.target
    const el = byId(node.id())

    if (el?.type === 'image' && cropping === el.id) {
      panCrop(node, el)

      return
    }

    if (selected.length > 1 || !pageNode.current) {
      return
    }

    const rect = node.getClientRect({ relativeTo: pageNode.current })
    const others = elements
      .filter((o) => o.id !== node.id() && !o.hidden)
      .map((o) => stage.current!.findOne(`#${o.id}`)?.getClientRect({ relativeTo: pageNode.current! }))
      .filter((r): r is NonNullable<typeof r> => !!r)
    const g = snap(rect, others, design, SNAP_PX / scale)

    node.x(node.x() + g.x - rect.x)
    node.y(node.y() + g.y - rect.y)
    setGuides({ v: g.v, h: g.h })
  }

  const commitNodes = (nodes: Konva.Node[]) => {
    updatePage((p) => {
      for (const node of nodes) {
        const el = p.elements.find((x) => x.id === node.id())

        if (!el) {
          continue
        }

        const sx = node.scaleX()
        const sy = node.scaleY()

        el.x = node.x()
        el.y = node.y()
        el.rotation = node.rotation()

        if (el.type === 'text') {
          el.width = Math.max(20, el.width * sx)

          if (Math.abs(sy - 1) > 1e-6) {
            el.fontSize = Math.max(4, Math.round(el.fontSize * sy * 10) / 10)

            if (el.box) {
              el.box.padding *= sy
            }
          }
        } else {
          el.width = Math.max(4, el.width * sx)
          el.height = Math.max(4, el.height * sy)
        }

        node.scaleX(1)
        node.scaleY(1)
      }
    })
  }

  const editingEl = editing ? byId(editing) : undefined
  const croppingEl = cropping ? byId(cropping) : undefined

  return (
    <div
      ref={box}
      className="relative size-full overflow-auto bg-muted/60 [background-image:radial-gradient(var(--border)_1px,transparent_1px)] [background-size:16px_16px]"
      data-testid="design-canvas"
      onDragOver={(e) => {
        if (dragged.make) {
          e.preventDefault()
          e.dataTransfer.dropEffect = 'copy'
        }
      }}
      onDrop={(e) => {
        const make = dragged.make
        const st = stage.current

        if (!make || !st) {
          return
        }

        e.preventDefault()
        e.stopPropagation()
        dragged.make = null
        st.setPointersPositions(e.nativeEvent)

        const at = st.getPointerPosition()!
        const els = [make()].flat()
        // Centred where it was dropped (several, a sticker: their box is).
        const left = Math.min(...els.map((x) => x.x))
        const top = Math.min(...els.map((x) => x.y))
        const w = Math.max(...els.map((x) => x.x + x.width)) - left
        const h = Math.max(...els.map((x) => x.y + x.height)) - top
        const dx = (at.x - ox) / scale - w / 2 - left
        const dy = (at.y - oy) / scale - h / 2 - top

        onDropEls(els.map((x) => ({ ...x, x: x.x + dx, y: x.y + dy })))
      }}
      onWheel={(e) => {
        // In crop mode the wheel zooms the picture inside its frame.
        if (croppingEl?.type === 'image') {
          e.preventDefault()
          updatePage((p) => {
            const t = p.elements.find((x) => x.id === croppingEl.id)

            if (t?.type === 'image') {
              const z = Math.min(8, Math.max(1, (t.crop?.zoom ?? 1) * (e.deltaY < 0 ? 1.08 : 1 / 1.08)))

              t.crop = { cx: t.crop?.cx ?? 0.5, cy: t.crop?.cy ?? 0.5, zoom: z }
            }
          }, 'crop-zoom')
        }
      }}
    >
      <Stage
        ref={stage}
        width={stageW}
        height={stageH}
        onMouseDown={(e) => {
          if (e.target === e.target.getStage() || e.target.name() === 'background') {
            onSelect([])
            onCropping(null)
          }
        }}
        onTouchStart={(e) => {
          if (e.target === e.target.getStage() || e.target.name() === 'background') {
            onSelect([])
            onCropping(null)
          }
        }}
      >
        <Layer>
          <Group
            ref={pageNode}
            x={ox}
            y={oy}
            scaleX={scale}
            scaleY={scale}
            clipX={0}
            clipY={0}
            clipWidth={design.width}
            clipHeight={design.height}
          >
            <Rect
              name="background"
              width={design.width}
              height={design.height}
              {...fillProps(page.background, design.width, design.height)}
            />
            {elements.map((el) =>
              el.hidden ? null : (
                <Group
                  key={el.id}
                  id={el.id}
                  x={el.x}
                  y={el.y}
                  rotation={el.rotation}
                  opacity={el.opacity}
                  visible={el.id !== editing}
                  draggable={!el.locked}
                  onMouseDown={(e) => {
                    e.cancelBubble = true

                    if (!selected.includes(el.id)) {
                      select(el.id, e)
                    }
                  }}
                  onTap={(e) => select(el.id, e)}
                  onClick={(e) => {
                    if (selected.includes(el.id) && (e.evt.shiftKey || e.evt.metaKey || e.evt.ctrlKey)) {
                      select(el.id, e)
                    }
                  }}
                  onDblClick={() => {
                    if (el.type === 'text' && !el.locked) {
                      setEditing(el.id)
                    }

                    if (el.type === 'image' && !el.locked) {
                      onCropping(el.id)
                    }
                  }}
                  onDblTap={() => {
                    if (el.type === 'text' && !el.locked) {
                      setEditing(el.id)
                    }

                    if (el.type === 'image' && !el.locked) {
                      onCropping(el.id)
                    }
                  }}
                  onDragMove={onDragMove}
                  onDragEnd={(e) => {
                    setGuides({ v: [], h: [] })

                    if (cropping !== el.id) {
                      commitNodes(selected.includes(el.id) ? (tr.current?.nodes() ?? [e.target]) : [e.target])
                    }
                  }}
                  onTransform={(e) => {
                    // Side handles on a text change its width (the text reflows), corners scale it.
                    const node = e.target
                    const anchor = tr.current?.getActiveAnchor() ?? ''

                    if (el.type === 'text' && (anchor === 'middle-left' || anchor === 'middle-right')) {
                      const text = (node as unknown as Konva.Group).findOne<Konva.Text>('Text')

                      if (text) {
                        text.width(Math.max(20, text.width() * node.scaleX()))
                        node.scaleX(1)
                      }
                    }
                  }}
                >
                  <ElementNode el={el} />
                </Group>
              ),
            )}
          </Group>
          {/* The page's outline over everything: the eye sees where it ends even on a white background. */}
          <Rect
            x={ox}
            y={oy}
            width={design.width * scale}
            height={design.height * scale}
            stroke="rgba(0,0,0,0.12)"
            strokeWidth={1}
            listening={false}
          />
        </Layer>
        <Layer>
          <Group x={ox} y={oy} scaleX={scale} scaleY={scale} listening={false}>
            {guides.v.map((x) => (
              <Line
                key={`v${x}`}
                points={[x, 0, x, design.height]}
                stroke={GUIDE}
                strokeWidth={1 / scale}
                dash={[4 / scale, 4 / scale]}
              />
            ))}
            {guides.h.map((y) => (
              <Line
                key={`h${y}`}
                points={[0, y, design.width, y]}
                stroke={GUIDE}
                strokeWidth={1 / scale}
                dash={[4 / scale, 4 / scale]}
              />
            ))}
            {croppingEl && (
              <Rect
                x={croppingEl.x}
                y={croppingEl.y}
                width={croppingEl.width}
                height={croppingEl.height}
                rotation={croppingEl.rotation}
                stroke={SELECT}
                strokeWidth={2 / scale}
                dash={[6 / scale, 4 / scale]}
              />
            )}
          </Group>
          <Transformer
            ref={tr}
            rotationSnaps={[0, 45, 90, 135, 180, 225, 270, 315]}
            rotationSnapTolerance={4}
            keepRatio={keepRatio}
            enabledAnchors={
              one?.type === 'text'
                ? ['top-left', 'top-right', 'bottom-left', 'bottom-right', 'middle-left', 'middle-right']
                : one?.locked
                  ? []
                  : undefined
            }
            rotateEnabled={!one?.locked}
            anchorSize={10}
            anchorCornerRadius={5}
            anchorStroke={SELECT}
            borderStroke={SELECT}
            borderStrokeWidth={1.5}
            padding={2}
            onTransformEnd={() => {
              const nodes = tr.current?.nodes() ?? []

              for (const node of nodes) {
                const el = byId(node.id())
                const text = el?.type === 'text' ? (node as unknown as Konva.Group).findOne<Konva.Text>('Text') : null

                // A side handle already set the text's width live: carry it over as a scale for commitNodes.
                if (el && text && node.scaleY() === 1) {
                  node.scaleX(text.width() / el.width)
                }
              }

              commitNodes(nodes)
            }}
            boundBoxFunc={(old, next) => (Math.abs(next.width) < 8 || Math.abs(next.height) < 8 ? old : next)}
          />
        </Layer>
      </Stage>
      {editingEl?.type === 'text' && (
        <TextEditor
          el={editingEl}
          left={ox + editingEl.x * scale}
          top={oy + editingEl.y * scale}
          scale={scale}
          onDone={(text) => {
            setEditing(null)
            updatePage((p) => {
              const t = p.elements.find((x) => x.id === editingEl.id)

              if (t?.type === 'text') {
                t.text = text
              }
            })
          }}
        />
      )}
    </div>
  )
}

/** Typing into a text in place: a textarea laid over it with the same font, size and angle. */
function TextEditor({
  el,
  left,
  top,
  scale,
  onDone,
}: {
  el: TextEl
  left: number
  top: number
  scale: number
  onDone: (text: string) => void
}) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const [value, setValue] = useState(el.text)
  const pad = (el.box?.padding ?? 0) * scale

  useEffect(() => {
    const t = ref.current

    t?.focus()
    t?.select()
  }, [])

  useLayoutEffect(() => {
    const t = ref.current

    if (t) {
      t.style.height = 'auto'
      t.style.height = `${t.scrollHeight}px`
    }
    // oxlint-disable-next-line react/exhaustive-effect-dependencies -- the field grows with what is typed
  }, [value])

  return (
    <textarea
      ref={ref}
      aria-label="Text"
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onBlur={() => onDone(value)}
      onKeyDown={(e) => {
        e.stopPropagation()

        if (e.key === 'Escape' || (e.key === 'Enter' && (e.metaKey || e.ctrlKey))) {
          onDone(value)
        }
      }}
      className="absolute resize-none overflow-hidden rounded-sm border-0 p-0 outline-2 outline-offset-2 outline-brand"
      style={{
        left,
        top,
        width: el.width * scale,
        padding: pad,
        background: el.box?.color ?? 'transparent',
        borderRadius: (el.box?.radius ?? 0) * scale,
        fontFamily: `"${el.fontFamily}"`,
        fontSize: el.fontSize * scale,
        fontWeight: el.bold ? 700 : 400,
        fontStyle: el.italic ? 'italic' : 'normal',
        textDecoration: el.underline ? 'underline' : 'none',
        lineHeight: el.lineHeight,
        letterSpacing: el.letterSpacing * scale,
        textAlign: el.align,
        color: el.fill,
        transform: `rotate(${el.rotation}deg)`,
        transformOrigin: 'top left',
      }}
    />
  )
}
