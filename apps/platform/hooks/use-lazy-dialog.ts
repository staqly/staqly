import { useCallback, useState } from 'react'

/**
 * Open state for a lazily loaded dialog or sheet (`next/dynamic`): render it only once `used`, so its
 * code loads on first open, and keep it mounted afterwards so closing animates. `initiallyOpen` serves deep links
 * such as the getting-started checklist's (`?add=1`, `?compose=1`).
 */
export function useLazyDialog(initiallyOpen = false) {
  const [state, setState] = useState({ open: initiallyOpen, used: initiallyOpen })
  const setOpen = useCallback((open: boolean) => setState({ open, used: true }), [])
  const toggle = useCallback(() => setState((s) => ({ open: !s.open, used: true })), [])

  return { ...state, setOpen, toggle }
}
