import { useEffect, type RefObject } from 'react'

/**
 * Keeps the selected element of a sideways-scrolling row (step tabs, chips) in view, also when the selection comes
 * from a link. `selector` names the selected element (e.g. `[data-step="archived"]`), so it changes with the
 * selection. Only the nearest horizontally scrolling ancestor moves, never the page.
 */
export function useKeepInView(root: RefObject<HTMLElement | null>, selector: string) {
  useEffect(() => {
    const el = root.current?.querySelector<HTMLElement>(selector)
    let row = el?.parentElement

    while (row && row.scrollWidth <= row.clientWidth) {
      row = row.parentElement
    }

    if (!el || !row || row === document.body || row === document.documentElement) {
      return
    }

    const r = row.getBoundingClientRect()
    const c = el.getBoundingClientRect()

    if (c.left < r.left || c.right > r.right) {
      row.scrollLeft += c.left - r.left - 16
    }
  }, [root, selector])
}
