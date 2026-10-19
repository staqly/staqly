import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useIsPast } from '@/hooks/use-is-past'

afterEach(() => {
  vi.useRealTimers()
})

describe('useIsPast', () => {
  it('flips once the date passes, follows a new date and is false without one', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 9, 21, 10, 0, 0))
    const soon = new Date(2026, 9, 21, 10, 0, 30)
    const { result, rerender } = renderHook(({ at }: { at: Date | null }) => useIsPast(at), {
      initialProps: { at: soon as Date | null },
    })

    expect(result.current).toBe(false)
    act(() => {
      vi.advanceTimersByTime(30_000)
    })
    expect(result.current).toBe(true)

    rerender({ at: new Date(2026, 9, 21, 11, 0, 0) })
    expect(result.current).toBe(false)
    rerender({ at: new Date(2026, 9, 21, 9, 0, 0) })
    expect(result.current).toBe(true)
    rerender({ at: null })
    expect(result.current).toBe(false)
  })
})
