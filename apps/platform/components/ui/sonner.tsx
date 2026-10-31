'use client'

import { CircleCheckIcon, InfoIcon, TriangleAlertIcon, OctagonXIcon, Loader2Icon } from 'lucide-react'
import { useTheme } from 'next-themes'
import { Toaster as Sonner, type ToasterProps } from 'sonner'

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme = 'system' } = useTheme()

  return (
    <Sonner
      theme={theme as ToasterProps['theme']}
      // pointer-events-auto: an open sheet or dialog sets pointer-events: none on <body>; Undo must stay clickable.
      className="toaster group pointer-events-auto"
      // Success, error, warning and info toasts in their own colours (success in the brand green).
      richColors
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      style={
        {
          '--normal-bg': 'var(--popover)',
          '--normal-text': 'var(--popover-foreground)',
          '--normal-border': 'var(--border)',
          '--border-radius': 'var(--radius)',
          // Sonner's 356 px fits a title and two buttons poorly: a long title wrapped to three lines.
          '--width': '420px',
          // Opaque: a light brand tint mixed into the popover colour (--brand-soft is translucent).
          '--success-bg': 'color-mix(in oklch, var(--brand) 14%, var(--popover))',
          '--success-border': 'color-mix(in oklch, var(--brand) 35%, transparent)',
          '--success-text': 'var(--brand-strong)',
        } as React.CSSProperties
      }
      toastOptions={{
        classNames: {
          toast: 'cn-toast pr-9!',
          // A plain × in the top-right corner instead of Sonner's floating circle on the top-left; on touch screens
          // its 20 px box gets a 40 px hit area.
          closeButton:
            "left-auto! right-0.5! top-2.5! translate-x-0! translate-y-0! border-0! bg-transparent! opacity-60 after:absolute after:-inset-2.5 hover:opacity-100 pointer-coarse:after:content-['']",
        },
      }}
      {...props}
    />
  )
}

/** A click in a toast (Undo...) is outside an open sheet or dialog, but must never close it. */
function inToast(target: EventTarget | null) {
  return target instanceof Element && !!target.closest('[data-sonner-toaster]')
}

export { inToast, Toaster }
