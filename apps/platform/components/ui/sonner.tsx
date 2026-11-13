'use client'

import { CircleCheckIcon, InfoIcon, TriangleAlertIcon, OctagonXIcon, Loader2Icon } from 'lucide-react'
import { useTheme } from 'next-themes'
import { useLayoutEffect } from 'react'
import { Toaster as Sonner, type ToasterProps, useSonner } from 'sonner'

const SHEET_FOOTER = '[data-slot="sheet-footer"]'

function liftToasts() {
  const footers = document.querySelectorAll<HTMLElement>(SHEET_FOOTER)
  const height = footers[footers.length - 1]?.offsetHeight ?? 0

  document.documentElement.style.setProperty('--toast-lift', `${height}px`)
}

/**
 * Toasts sit above the footer of the open drawer (--toast-lift, used in globals.css): at the bottom right a toast
 * lay on Save / Publish now and stayed there while the pointer rested on the button, so it could not be clicked
 * again. Measured only while a toast shows: when it appears, and when a drawer opens, closes or resizes meanwhile.
 */
function useToastLift() {
  const showing = useSonner().toasts.length > 0

  useLayoutEffect(() => {
    if (!showing) {
      return
    }

    liftToasts()
    const changes = new MutationObserver(liftToasts)

    changes.observe(document.body, { childList: true, subtree: true })
    window.addEventListener('resize', liftToasts)

    return () => {
      changes.disconnect()
      window.removeEventListener('resize', liftToasts)
    }
  }, [showing])
}

const Toaster = ({ ...props }: ToasterProps) => {
  const { theme = 'system' } = useTheme()

  useToastLift()

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
          '--width': '480px',
          // Opaque: a light brand tint mixed into the popover colour (--brand-soft is translucent).
          '--success-bg': 'color-mix(in oklch, var(--brand) 14%, var(--popover))',
          '--success-border': 'color-mix(in oklch, var(--brand) 35%, transparent)',
          '--success-text': 'var(--brand-strong)',
          // Sonner's own info blue is navy on near-black in dark mode, too faint to notice; the same tint as success.
          '--info-bg': 'color-mix(in oklch, var(--chart-3) 18%, var(--popover))',
          '--info-border': 'color-mix(in oklch, var(--chart-3) 45%, transparent)',
          '--info-text': 'color-mix(in oklch, var(--chart-3) 55%, var(--popover-foreground))',
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
