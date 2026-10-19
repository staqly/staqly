import { useCallback, useState } from 'react'

/**
 * Open state for a lazily loaded dialog or sheet (`next/dynamic`): render it only once `used`, so its
 * code loads on first open, and keep it mounted afterwards so closing animates.
 */
export function useLazyDialog() {
  const [state, setState] = useState({ open: false, used: false })
  const setOpen = useCallback((open: boolean) => setState({ open, used: true }), [])
  const toggle = useCallback(() => setState((s) => ({ open: !s.open, used: true })), [])

  return { ...state, setOpen, toggle }
}
