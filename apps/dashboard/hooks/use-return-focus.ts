import { useRef } from 'react'

/**
 * Focus goes back to where it was when a dialog or sheet closes. Radix's modal dialogs focus their Trigger on close,
 * and most of ours are opened from state (an inbox row, ⌘K, a menu item) without one, so focus fell to <body> and the
 * next Tab started at the top of the page. The element focused when the dialog opened is remembered and focused
 * again if it is still on the page; otherwise Radix's own behaviour runs (its Trigger, if any).
 */
export function useReturnFocus({
  onOpenAutoFocus,
  onCloseAutoFocus,
}: {
  onOpenAutoFocus?: (e: Event) => void
  onCloseAutoFocus?: (e: Event) => void
}) {
  const returnTo = useRef<HTMLElement | null>(null)

  return {
    onOpenAutoFocus: (e: Event) => {
      // Radix calls this before it moves focus into the content, so this is still the element outside.
      let el = document.activeElement

      // A menu item goes away with its menu: return to the button that opened the menu (through submenus) instead.
      for (let menu = el?.closest('[role=menu]'), i = 0; menu?.id && i < 5; menu = el?.closest('[role=menu]'), i++) {
        el = document.querySelector(`[aria-controls="${CSS.escape(menu.id)}"]`)
      }

      returnTo.current = el instanceof HTMLElement && el !== document.body ? el : null
      onOpenAutoFocus?.(e)
    },
    onCloseAutoFocus: (e: Event) => {
      onCloseAutoFocus?.(e)
      const el = returnTo.current

      returnTo.current = null

      if (!e.defaultPrevented && el?.isConnected) {
        e.preventDefault()
        el.focus({ preventScroll: true })
      }
    },
  }
}
