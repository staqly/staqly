'use client'

import { QueryClientContext } from '@tanstack/react-query'
import { cn } from 'cn'
import { XIcon } from 'lucide-react'
import { Dialog as SheetPrimitive } from 'radix-ui'
import * as React from 'react'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { Button } from '@/components/ui/button'
import { inToast } from '@/components/ui/sonner'
import { useReturnFocus } from '@/hooks/use-return-focus'

/**
 * Unsaved changes: the form fields of a sheet are read just before the first key or click inside it; closing it
 * with Esc, the X or a SheetClose after they changed asks first. Saving closes through the caller's own
 * onOpenChange, which this does not see, so a saved form never asks; a sheet that stays open after a save reads its
 * fields again once any save (a successful mutation) finished.
 */
type Guard = { el: HTMLElement | null; before?: string }

const SheetGuard = React.createContext<{ remember: (el: HTMLElement) => void; reset: () => void } | null>(null)

const FIELDS =
  'input:not([type=hidden]):not([type=search]), textarea, select, [contenteditable=true], [role=combobox], [role=switch], [role=checkbox], [aria-pressed]'

function formState(el: HTMLElement) {
  return Array.from(el.querySelectorAll<HTMLElement>(FIELDS), (f) => {
    if (f instanceof HTMLInputElement && (f.type === 'checkbox' || f.type === 'radio')) {
      return String(f.checked)
    }

    if (f instanceof HTMLInputElement || f instanceof HTMLTextAreaElement || f instanceof HTMLSelectElement) {
      return f.value
    }

    return `${f.getAttribute('aria-checked') ?? ''}${f.getAttribute('aria-pressed') ?? ''}${f.textContent ?? ''}`
  }).join('\u0000')
}

function Sheet({ onOpenChange, ...props }: React.ComponentProps<typeof SheetPrimitive.Root>) {
  const guard = React.useRef<Guard>({ el: null })
  const [asking, setAsking] = React.useState(false)
  const ctx = React.useMemo(
    () => ({
      remember: (el: HTMLElement) => {
        if (guard.current.before === undefined) {
          guard.current = { el, before: formState(el) }
        }
      },
      reset: () => {
        guard.current = { el: null }
      },
    }),
    [],
  )

  const client = React.useContext(QueryClientContext)

  // Fields the server changes are not unsaved work: re-read them after a save (any successful mutation), and after
  // fresh data arrives while the person hasn't changed anything yet (a refetch after archiving, say).
  React.useEffect(() => {
    if (!client) {
      return
    }

    const reread = (onlyIfClean: boolean) => {
      const { el, before } = guard.current
      const clean = !el || before === undefined || formState(el) === before

      if (onlyIfClean && !clean) {
        return
      }

      // After the new values reached the fields.
      setTimeout(() => {
        if (guard.current.el) {
          guard.current.before = formState(guard.current.el)
        }
      })
    }
    const offMutations = client.getMutationCache().subscribe((e) => {
      if (e.type === 'updated' && e.action.type === 'success') {
        reread(false)
      }
    })
    const offQueries = client.getQueryCache().subscribe((e) => {
      if (e.type === 'updated' && e.action.type === 'success') {
        reread(true)
      }
    })

    return () => {
      offMutations()
      offQueries()
    }
  }, [client])

  return (
    <SheetGuard.Provider value={ctx}>
      <SheetPrimitive.Root
        data-slot="sheet"
        {...props}
        onOpenChange={(open) => {
          const { el, before } = guard.current

          if (!open && el && before !== undefined && formState(el) !== before) {
            setAsking(true)

            return
          }

          guard.current.before = undefined
          onOpenChange?.(open)
        }}
      />
      <ConfirmDialog
        open={asking}
        onOpenChange={setAsking}
        title="Discard your changes?"
        description="You changed this form without saving. Closing it loses those changes."
        confirmLabel="Discard changes"
        variant="destructive"
        onConfirm={() => {
          guard.current.before = undefined
          onOpenChange?.(false)
        }}
      />
    </SheetGuard.Provider>
  )
}

function SheetTrigger({ ...props }: React.ComponentProps<typeof SheetPrimitive.Trigger>) {
  return <SheetPrimitive.Trigger data-slot="sheet-trigger" {...props} />
}

function SheetClose({ ...props }: React.ComponentProps<typeof SheetPrimitive.Close>) {
  return <SheetPrimitive.Close data-slot="sheet-close" {...props} />
}

function SheetPortal({ ...props }: React.ComponentProps<typeof SheetPrimitive.Portal>) {
  return <SheetPrimitive.Portal data-slot="sheet-portal" {...props} />
}

function SheetOverlay({ className, ...props }: React.ComponentProps<typeof SheetPrimitive.Overlay>) {
  return (
    <SheetPrimitive.Overlay
      data-slot="sheet-overlay"
      className={cn(
        'fixed inset-0 z-50 bg-black/10 duration-100 supports-backdrop-filter:backdrop-blur-xs data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0',
        className,
      )}
      {...props}
    />
  )
}

function SheetContent({
  className,
  children,
  side = 'right',
  showCloseButton = true,
  closeOnOutsideClick = false,
  onInteractOutside,
  ...props
}: React.ComponentProps<typeof SheetPrimitive.Content> & {
  side?: 'top' | 'right' | 'bottom' | 'left'
  showCloseButton?: boolean
  /** Sheets close with Esc or the X only, so a stray click never drops unsaved work. Navigation drawers opt in. */
  closeOnOutsideClick?: boolean
}) {
  // Open on the first control of the body, else the sheet itself: not on an icon button in the header, whose
  // tooltip would then be the first thing Esc closes.
  const returnFocus = useReturnFocus({
    ...props,
    onOpenAutoFocus: (e) => {
      props.onOpenAutoFocus?.(e)

      if (e.defaultPrevented || !(e.currentTarget instanceof HTMLElement)) {
        return
      }

      const el = e.currentTarget
      const first = Array.from(
        el.querySelectorAll<HTMLElement>(
          'input:not([type=hidden]), textarea, select, button, a[href], [contenteditable=true], [tabindex]:not([tabindex="-1"])',
        ),
      ).find(
        (c) =>
          !c.closest('[data-slot=sheet-header], [data-slot=sheet-close], [aria-hidden=true]') &&
          !(c as HTMLButtonElement).disabled &&
          c.offsetParent !== null,
      )

      e.preventDefault()
      ;(first ?? el).focus()
    },
  })
  const guard = React.useContext(SheetGuard)
  const reset = guard?.reset

  // Each opening starts clean, also when the last one closed by saving.
  React.useEffect(() => reset?.(), [reset])

  // The fields as they were before the person touched anything (loaded data included).
  const remember = (e: React.SyntheticEvent<HTMLElement>) => guard?.remember(e.currentTarget)

  return (
    <SheetPortal>
      <SheetOverlay />
      <SheetPrimitive.Content
        data-slot="sheet-content"
        data-side={side}
        onPointerDownCapture={remember}
        onKeyDownCapture={remember}
        onInteractOutside={(e) => {
          if (!closeOnOutsideClick || inToast(e.target)) {
            e.preventDefault()
          }

          onInteractOutside?.(e)
        }}
        className={cn(
          'group/sheet fixed z-50 flex flex-col gap-4 bg-popover bg-clip-padding text-sm text-popover-foreground shadow-lg transition duration-200 ease-in-out data-[side=bottom]:inset-x-0 data-[side=bottom]:bottom-0 data-[side=bottom]:h-auto data-[side=bottom]:max-h-[calc(100svh_-_env(safe-area-inset-top)_-_0.75rem)] data-[side=bottom]:overflow-y-auto data-[side=bottom]:overscroll-contain data-[side=bottom]:border-t data-[side=left]:inset-y-0 data-[side=left]:left-0 data-[side=left]:h-full data-[side=left]:w-3/4 data-[side=left]:border-r data-[side=right]:inset-y-0 data-[side=right]:right-0 data-[side=right]:h-full data-[side=right]:w-3/4 data-[side=right]:border-l data-[side=top]:inset-x-0 data-[side=top]:top-0 data-[side=top]:h-auto data-[side=top]:border-b data-[side=left]:sm:max-w-sm data-[side=right]:sm:max-w-sm data-open:animate-in data-open:fade-in-0 data-[side=bottom]:data-open:slide-in-from-bottom-10 data-[side=left]:data-open:slide-in-from-left-10 data-[side=right]:data-open:slide-in-from-right-10 data-[side=top]:data-open:slide-in-from-top-10 data-closed:animate-out data-closed:fade-out-0 data-[side=bottom]:data-closed:slide-out-to-bottom-10 data-[side=left]:data-closed:slide-out-to-left-10 data-[side=right]:data-closed:slide-out-to-right-10 data-[side=top]:data-closed:slide-out-to-top-10',
          className,
        )}
        {...props}
        {...returnFocus}
      >
        {children}
        {showCloseButton && (
          <SheetPrimitive.Close data-slot="sheet-close" asChild>
            <Button
              variant="ghost"
              className="absolute top-3 right-3 group-data-[side=left]/sheet:top-[max(0.75rem,env(safe-area-inset-top))] group-data-[side=right]/sheet:top-[max(0.75rem,env(safe-area-inset-top))] group-data-[side=top]/sheet:top-[max(0.75rem,env(safe-area-inset-top))]"
              size="icon-sm"
            >
              <XIcon />
              <span className="sr-only">Close</span>
            </Button>
          </SheetPrimitive.Close>
        )}
      </SheetPrimitive.Content>
    </SheetPortal>
  )
}

function SheetHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="sheet-header"
      className={cn(
        // Side and top sheets run under the notch / status bar (viewport-fit=cover).
        // pr-12: title and description stop before the close button (pr-14 for its 40 px touch size).
        'flex flex-col gap-0.5 p-4 pr-12 pointer-coarse:pr-14 group-data-[side=left]/sheet:pt-[max(1rem,env(safe-area-inset-top))] group-data-[side=right]/sheet:pt-[max(1rem,env(safe-area-inset-top))] group-data-[side=top]/sheet:pt-[max(1rem,env(safe-area-inset-top))]',
        className,
      )}
      {...props}
    />
  )
}

function SheetFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="sheet-footer"
      className={cn(
        // Above the home indicator on side and bottom sheets.
        'mt-auto flex flex-col gap-2 p-4 group-data-[side=bottom]/sheet:pb-[max(1rem,env(safe-area-inset-bottom))] group-data-[side=left]/sheet:pb-[max(1rem,env(safe-area-inset-bottom))] group-data-[side=right]/sheet:pb-[max(1rem,env(safe-area-inset-bottom))]',
        className,
      )}
      {...props}
    />
  )
}

function SheetTitle({ className, ...props }: React.ComponentProps<typeof SheetPrimitive.Title>) {
  return (
    <SheetPrimitive.Title
      data-slot="sheet-title"
      className={cn('font-heading text-base font-medium text-foreground', className)}
      {...props}
    />
  )
}

function SheetDescription({ className, ...props }: React.ComponentProps<typeof SheetPrimitive.Description>) {
  return (
    <SheetPrimitive.Description
      data-slot="sheet-description"
      className={cn('text-sm text-muted-foreground', className)}
      {...props}
    />
  )
}

export { Sheet, SheetTrigger, SheetClose, SheetContent, SheetHeader, SheetFooter, SheetTitle, SheetDescription }
