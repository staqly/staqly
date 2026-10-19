/**
 * Coloured text uses the 700 shade in light mode where 600 stays under 4.5:1 (emerald, amber, orange).
 *
 * The one colour system for statuses, badges, timeline icons, inbox tabs and action buttons. Every coloured
 * thing in the dashboard picks a tone here instead of a Tailwind colour, so a status looks the same everywhere.
 * Class names are literal so Tailwind generates them.
 *
 * neutral  slate    draft, expired, archived, edits
 * info     blue     processing, created, an action an automation picks up
 * warning  amber    pending (waiting for a person), escalated, paused
 * success  emerald  approved, completed
 * attention orange  (spare: things to look at that are not errors)
 * danger   red      rejected (soft pill), failed (the one solid badge), deleted, revoked
 * accent   violet   AI-generated (the item summary)
 */
export type Tone = 'neutral' | 'info' | 'warning' | 'success' | 'attention' | 'danger' | 'accent'

interface ToneClasses {
  /** Soft pill: tinted background, coloured text and a hairline ring. */
  badge: string
  /** Small solid dot (counts, tabs). */
  dot: string
  /** Coloured icon or text on a neutral surface. */
  text: string
  /** Round icon chip in timelines: tinted background, coloured icon. */
  chip: string
  /** Selected toggle/tab. */
  on: string
  /** Icons of a button or menu row (an action's colour), the text keeps its own. */
  icon: string
  /** Menu row: a soft tint on hover / focus, the icon keeps its colour (menu items recolour children on focus). */
  menu: string
  /** Callout box (components/common/callout.tsx): tinted border and background, text stays the foreground. */
  callout: string
  /** Meter fill (usage bars). */
  fill: string
  /** Border of a tinted card (e.g. a danger-zone settings card). */
  border: string
}

export const TONES: Record<Tone, ToneClasses> = {
  neutral: {
    badge:
      'bg-slate-500/10 text-slate-700 ring-slate-500/20 dark:bg-slate-400/10 dark:text-slate-300 dark:ring-slate-400/20',
    dot: 'bg-slate-400',
    text: 'text-slate-500 dark:text-slate-400',
    chip: 'bg-slate-500/10 text-slate-600 dark:bg-slate-400/12 dark:text-slate-300',
    on: 'data-[state=on]:bg-slate-500/12 data-[state=on]:text-slate-800 dark:data-[state=on]:text-slate-200',
    icon: '[&_svg]:text-slate-500',
    menu: 'focus:bg-slate-500/10 focus:[&_svg]:text-slate-500!',
    callout: 'border-slate-500/40 bg-slate-500/10',
    fill: 'bg-slate-400',
    border: 'border-slate-500/40',
  },
  info: {
    badge: 'bg-blue-500/10 text-blue-700 ring-blue-600/20 dark:bg-blue-400/10 dark:text-blue-300 dark:ring-blue-400/25',
    dot: 'bg-blue-500',
    text: 'text-blue-600 dark:text-blue-400',
    chip: 'bg-blue-500/10 text-blue-600 dark:bg-blue-400/12 dark:text-blue-300',
    on: 'data-[state=on]:bg-blue-500/12 data-[state=on]:text-blue-800 dark:data-[state=on]:text-blue-300',
    icon: '[&_svg]:text-blue-500',
    menu: 'focus:bg-blue-500/10 focus:[&_svg]:text-blue-500!',
    callout: 'border-blue-500/40 bg-blue-500/10',
    fill: 'bg-blue-500',
    border: 'border-blue-500/40',
  },
  warning: {
    badge:
      'bg-amber-500/12 text-amber-800 ring-amber-600/25 dark:bg-amber-400/10 dark:text-amber-300 dark:ring-amber-400/25',
    dot: 'bg-amber-500',
    text: 'text-amber-700 dark:text-amber-400',
    chip: 'bg-amber-500/12 text-amber-700 dark:bg-amber-400/12 dark:text-amber-300',
    on: 'data-[state=on]:bg-amber-500/15 data-[state=on]:text-amber-900 dark:data-[state=on]:text-amber-300',
    icon: '[&_svg]:text-amber-500',
    menu: 'focus:bg-amber-500/10 focus:[&_svg]:text-amber-500!',
    callout: 'border-amber-500/40 bg-amber-500/10',
    fill: 'bg-amber-500',
    border: 'border-amber-500/40',
  },
  success: {
    badge:
      'bg-emerald-500/10 text-emerald-800 ring-emerald-600/20 dark:bg-emerald-400/10 dark:text-emerald-300 dark:ring-emerald-400/25',
    dot: 'bg-emerald-500',
    text: 'text-emerald-700 dark:text-emerald-400',
    chip: 'bg-emerald-500/10 text-emerald-700 dark:bg-emerald-400/12 dark:text-emerald-300',
    on: 'data-[state=on]:bg-emerald-500/12 data-[state=on]:text-emerald-800 dark:data-[state=on]:text-emerald-300',
    icon: '[&_svg]:text-emerald-500',
    menu: 'focus:bg-emerald-500/10 focus:[&_svg]:text-emerald-500!',
    callout: 'border-emerald-500/40 bg-emerald-500/10',
    fill: 'bg-emerald-500',
    border: 'border-emerald-500/40',
  },
  attention: {
    badge:
      'bg-orange-500/10 text-orange-700 ring-orange-600/20 dark:bg-orange-400/10 dark:text-orange-300 dark:ring-orange-400/25',
    dot: 'bg-orange-500',
    text: 'text-orange-700 dark:text-orange-400',
    chip: 'bg-orange-500/10 text-orange-700 dark:bg-orange-400/12 dark:text-orange-300',
    on: 'data-[state=on]:bg-orange-500/12 data-[state=on]:text-orange-800 dark:data-[state=on]:text-orange-300',
    icon: '[&_svg]:text-orange-500',
    menu: 'focus:bg-orange-500/10 focus:[&_svg]:text-orange-500!',
    callout: 'border-orange-500/40 bg-orange-500/10',
    fill: 'bg-orange-500',
    border: 'border-orange-500/40',
  },
  danger: {
    badge: 'bg-red-500/10 text-red-700 ring-red-600/20 dark:bg-red-400/10 dark:text-red-300 dark:ring-red-400/25',
    dot: 'bg-red-500',
    text: 'text-red-600 dark:text-red-400',
    chip: 'bg-red-500/10 text-red-600 dark:bg-red-400/12 dark:text-red-300',
    on: 'data-[state=on]:bg-red-500/12 data-[state=on]:text-red-800 dark:data-[state=on]:text-red-300',
    icon: '[&_svg]:text-red-500',
    menu: 'focus:bg-red-500/10 focus:[&_svg]:text-red-500!',
    callout: 'border-red-500/40 bg-red-500/10',
    fill: 'bg-red-500',
    border: 'border-red-500/40',
  },
  accent: {
    badge:
      'bg-violet-500/10 text-violet-700 ring-violet-600/20 dark:bg-violet-400/10 dark:text-violet-300 dark:ring-violet-400/25',
    dot: 'bg-violet-500',
    text: 'text-violet-600 dark:text-violet-400',
    chip: 'bg-violet-500/10 text-violet-600 dark:bg-violet-400/12 dark:text-violet-300',
    on: 'data-[state=on]:bg-violet-500/12 data-[state=on]:text-violet-800 dark:data-[state=on]:text-violet-300',
    icon: '[&_svg]:text-violet-500',
    menu: 'focus:bg-violet-500/10 focus:[&_svg]:text-violet-500!',
    callout: 'border-violet-500/40 bg-violet-500/10',
    fill: 'bg-violet-500',
    border: 'border-violet-500/40',
  },
}
