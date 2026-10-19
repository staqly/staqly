import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ColorPicker } from '@/components/collections/color-picker'
import { contrastText, hexToHsv, hsvToHex, isHex, normalizeHex } from '@/lib/color'

describe('colour maths', () => {
  it('normalizes and validates hex', () => {
    expect(normalizeHex('#3B82F6')).toBe('#3b82f6')
    expect(normalizeHex('abc')).toBe('#aabbcc')
    expect(normalizeHex(' #fff ')).toBe('#ffffff')
    expect(normalizeHex('#12345')).toBeNull()
    expect(normalizeHex('blue')).toBeNull()
    expect(isHex('#000000')).toBe(true)
    expect(isHex('#00000g')).toBe(false)
  })

  it('round-trips hex through HSV', () => {
    for (const hex of ['#3b82f6', '#22c55e', '#ef4444', '#000000', '#ffffff', '#808080']) {
      const hsv = hexToHsv(hex)!
      const back = hexToHsv(hsvToHex(hsv))!

      expect(Math.abs(back.s - hsv.s)).toBeLessThanOrEqual(1)
      expect(Math.abs(back.v - hsv.v)).toBeLessThanOrEqual(1)
    }

    expect(hexToHsv('#ff0000')).toEqual({ h: 0, s: 100, v: 100 })
    expect(hsvToHex({ h: 120, s: 100, v: 100 })).toBe('#00ff00')
    expect(contrastText('#eab308')).toBe('black')
    expect(contrastText('#1e3a8a')).toBe('white')
  })
})

describe('colour picker', () => {
  it('offers the presets plus a Custom swatch; a custom hex becomes the selected swatch', async () => {
    const onChange = vi.fn<(c: string) => void>()
    const user = userEvent.setup()

    render(<ColorPicker value="" onChange={onChange} />)
    expect(screen.getByRole('radio', { name: 'No colour' })).toHaveAttribute('aria-checked', 'true')
    await user.click(screen.getByRole('radio', { name: 'Blue' }))
    expect(onChange).toHaveBeenLastCalledWith('#3b82f6')

    await user.click(screen.getByTestId('color-custom'))
    const hexField = await screen.findByRole('textbox', { name: 'Hex colour' })

    await user.clear(hexField)
    await user.type(hexField, 'zzz')
    expect(screen.getByText('Use a hex colour such as #3b82f6')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'OK' })).toBeDisabled()
    await user.clear(hexField)
    await user.type(hexField, '#123456')
    expect(screen.getByRole('button', { name: 'OK' })).toBeEnabled()
    // The arrow keys move the square: brightness up twice by 2 %.
    const square = screen.getByTestId('color-square')

    square.focus()
    await user.keyboard('{ArrowUp}{ArrowUp}')
    const before = hexToHsv('#123456')!

    expect(square).toHaveAttribute('aria-valuenow', String(before.v + 4))
    await user.click(screen.getByRole('button', { name: 'OK' }))
    expect(onChange).toHaveBeenLastCalledWith(expect.stringMatching(/^#[0-9a-f]{6}$/))
  })

  it('shows a stored custom colour as the chosen swatch', () => {
    render(<ColorPicker value="#123456" onChange={() => {}} />)
    expect(screen.getByRole('button', { name: 'Custom colour #123456' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('radio', { name: 'Blue' })).toHaveAttribute('aria-checked', 'false')
  })
})
