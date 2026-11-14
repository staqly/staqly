import { useEffect, useState, type RefObject } from 'react'

type Overflow = { left: boolean; right: boolean }

/** Which ends of a sideways-scrolling strip hide content. */
export function stripOverflow(el: { scrollLeft: number; scrollWidth: number; clientWidth: number }): Overflow {
  return { left: el.scrollLeft > 1, right: el.scrollLeft + el.clientWidth < el.scrollWidth - 1 }
}

const FADE = {
  left: '[mask-image:linear-gradient(to_right,transparent,black_2.5rem)]',
  right: '[mask-image:linear-gradient(to_left,transparent,black_2.5rem)]',
  both: '[mask-image:linear-gradient(to_right,transparent,black_2.5rem,black_calc(100%-2.5rem),transparent)]',
} as const

export type Fade = keyof typeof FADE

/**
 * For a strip that scrolls sideways (tabs, step chips on a phone): the end that hides more content fades out,
 * so the strip reads as scrollable. Returns the mask class (empty when everything fits) and which side fades.
 */
export function useScrollFade(ref: RefObject<HTMLElement | null>): { className: string; fade: Fade | null } {
  const [overflow, setOverflow] = useState<Overflow>({ left: false, right: false })

  useEffect(() => {
    const el = ref.current

    if (!el) {
      return
    }

    const update = () => {
      const next = stripOverflow(el)

      setOverflow((prev) => (prev.left === next.left && prev.right === next.right ? prev : next))
    }

    update()
    el.addEventListener('scroll', update, { passive: true })
    const ro = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(update)

    ro?.observe(el)

    return () => {
      el.removeEventListener('scroll', update)
      ro?.disconnect()
    }
  }, [ref])

  const fade: Fade | null =
    overflow.left && overflow.right ? 'both' : overflow.left ? 'left' : overflow.right ? 'right' : null

  return { className: fade ? FADE[fade] : '', fade }
}
