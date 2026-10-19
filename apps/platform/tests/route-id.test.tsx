import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useRouteId } from '@/hooks/use-route-id'

import { nav } from './navigation'

describe('useRouteId', () => {
  it('decodes the last path segment', () => {
    nav.set('/items/itm%5F1')
    expect(renderHook(() => useRouteId()).result.current).toBe('itm_1')
  })

  it('keeps a malformed segment as it is instead of throwing', () => {
    nav.set('/items/%E0')
    expect(renderHook(() => useRouteId()).result.current).toBe('%E0')
  })
})
