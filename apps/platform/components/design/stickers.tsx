'use client'

import {
  Award,
  Bell,
  Calendar,
  Camera,
  Check,
  Clock,
  Coffee,
  Crown,
  Flame,
  Gift,
  Globe,
  Heart,
  Leaf,
  Lightbulb,
  Mail,
  MapPin,
  MessageCircle,
  Moon,
  Music,
  PartyPopper,
  Percent,
  Phone,
  Quote,
  Rocket,
  ShoppingBag,
  Smile,
  Sparkles,
  Star,
  Sun,
  Tag,
  Target,
  ThumbsUp,
  TrendingUp,
  Trophy,
  X,
  Zap,
  type LucideIcon,
} from 'lucide-react'

import { iconSrc, newShape, newText, uid, type El, type ImageEl, type ShapeKind, type Size } from './doc'
import { dragProps, Section, type PanelCtx } from './panels'

const ICONS: [string, LucideIcon][] = Object.entries({
  Heart,
  Star,
  Sparkles,
  Zap,
  Flame,
  ThumbsUp,
  Smile,
  PartyPopper,
  Gift,
  Rocket,
  Trophy,
  Award,
  Crown,
  Check,
  X,
  Quote,
  MessageCircle,
  Bell,
  Calendar,
  Clock,
  MapPin,
  Phone,
  Mail,
  Globe,
  ShoppingBag,
  Tag,
  Percent,
  Camera,
  Music,
  Coffee,
  Sun,
  Moon,
  Leaf,
  Lightbulb,
  Target,
  TrendingUp,
})

/** The icon as drawn in the panel, as a picture of 512 px that keeps its colour in `icon`. */
function iconEl(d: Size, name: string, color = '#111827'): ImageEl | null {
  const svg = document.querySelector(`[data-icon="${name}"] svg`)

  if (!svg) {
    return null
  }

  const markup = svg.outerHTML
    .replace(/ class="[^"]*"/, '')
    .replace(/ width="[^"]*"/, ' width="512"')
    .replace(/ height="[^"]*"/, ' height="512"')
  const s = Math.min(d.width, d.height) * 0.2

  return {
    id: uid(),
    type: 'image',
    src: iconSrc(markup, color),
    icon: { svg: markup, color },
    x: (d.width - s) / 2,
    y: (d.height - s) / 2,
    width: s,
    height: s,
    rotation: 0,
    opacity: 1,
    radius: 0,
    filters: { brightness: 0, contrast: 0, saturation: 0, blur: 0, grayscale: false },
  }
}

interface Sticker {
  label: string
  shape: ShapeKind
  fill: string
  color: string
  font: string
  wide?: boolean
}

const STICKERS: Sticker[] = [
  { label: 'NEW', shape: 'star', fill: '#facc15', color: '#111827', font: 'Anton' },
  { label: 'SALE', shape: 'rect', fill: '#ef4444', color: '#ffffff', font: 'Anton', wide: true },
  { label: '-20%', shape: 'ellipse', fill: '#111827', color: '#ffffff', font: 'Anton' },
  { label: 'FREE', shape: 'ellipse', fill: '#22c55e', color: '#ffffff', font: 'Anton' },
  { label: 'LIMITED', shape: 'rect', fill: '#111827', color: '#facc15', font: 'Bebas Neue', wide: true },
  { label: 'Swipe →', shape: 'rect', fill: '#ffffff', color: '#111827', font: 'Poppins', wide: true },
]

/** A badge: a shape with a word on it, grouped so they move together. */
function stickerEls(d: Size, k: Sticker): El[] {
  const shape = newShape(d, k.shape)
  const group = uid()
  const w = k.wide ? shape.width * 1.4 : shape.width
  const h = k.wide ? shape.height * 0.45 : shape.height
  const x = (d.width - w) / 2
  const y = (d.height - h) / 2
  const size = h * (k.shape === 'star' ? 0.22 : 0.36)
  const text = newText(d, 'heading')

  return [
    { ...shape, x, y, width: w, height: h, fill: k.fill, radius: k.wide ? h / 2 : 0, group },
    {
      ...text,
      text: k.label,
      fontFamily: k.font,
      fontSize: size,
      bold: k.font === 'Poppins',
      fill: k.color,
      align: 'center',
      x,
      width: w,
      y: y + h / 2 - (size * text.lineHeight) / 2 + (k.shape === 'star' ? h * 0.06 : 0),
      height: size * text.lineHeight,
      group,
    },
  ]
}

export function IconsAndStickers({ ctx }: { ctx: PanelCtx }) {
  const d = ctx.store.design

  return (
    <>
      <Section title="Stickers">
        <div className="grid grid-cols-3 gap-2">
          {STICKERS.map((k) => (
            <button
              key={k.label}
              type="button"
              aria-label={`Sticker ${k.label}`}
              className="flex h-14 items-center justify-center rounded-lg border bg-background hover:ring-2 hover:ring-brand"
              onClick={() => ctx.add(...stickerEls(d, k))}
              {...dragProps(() => stickerEls(d, k))}
            >
              <span
                className="px-2 py-0.5 text-sm"
                style={{
                  background: k.fill,
                  color: k.color,
                  fontFamily: `"${k.font}"`,
                  borderRadius: k.wide || k.shape === 'ellipse' ? 999 : 4,
                }}
              >
                {k.label}
              </span>
            </button>
          ))}
        </div>
      </Section>
      <Section title="Icons">
        <div className="grid grid-cols-6 gap-1.5">
          {ICONS.map(([name, Icon]) => (
            <button
              key={name}
              type="button"
              data-icon={name}
              aria-label={`Icon ${name}`}
              title={name}
              className="flex aspect-square items-center justify-center rounded-md border bg-background hover:ring-2 hover:ring-brand"
              onClick={() => {
                const el = iconEl(d, name)

                if (el) {
                  ctx.add(el)
                }
              }}
              {...dragProps(() => iconEl(d, name) ?? [])}
            >
              <Icon className="size-5" />
            </button>
          ))}
        </div>
      </Section>
    </>
  )
}
