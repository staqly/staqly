import { delayedCall, fromTo, killTweensOf, parseEase, set, timeline, to } from '@opengpex/editor/core/motion/tween'
import { describe, expect, it } from 'vitest'

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))

describe('the editor tween engine (GSAP stand-in)', () => {
  it('eases like GSAP names them', () => {
    for (const name of ['power2.out', 'power4.inOut', 'expo.out', 'back.out(1.7)', 'none', 'sine.in']) {
      const e = parseEase(name)

      expect(e(0)).toBeCloseTo(0, 5)
      expect(e(1)).toBeCloseTo(1, 5)
    }

    expect(parseEase('power2.out')(0.5)).toBeGreaterThan(0.5)
    expect(parseEase('power2.in')(0.5)).toBeLessThan(0.5)
    expect(parseEase('back.out(2)')(0.7)).toBeGreaterThan(1) // overshoots
  })

  it('sets the transform shorthands composed in one transform, and plain CSS', () => {
    const el = document.createElement('div')

    set(el, {
      x: 10,
      y: 20,
      scale: 2,
      rotation: 45,
      xPercent: -50,
      yPercent: -50,
      opacity: 0.5,
      width: 30,
      display: 'none',
    })

    expect(el.style.transform).toBe('translate(-50%, -50%) translate(10px, 20px) rotate(45deg) scale(2, 2)')
    expect(el.style.opacity).toBe('0.5')
    expect(el.style.width).toBe('30px')
    expect(el.style.display).toBe('none')

    set(el, { transform: 'matrix(1, 0, 0, 1, 5, 5)' })
    expect(el.style.transform).toBe('matrix(1, 0, 0, 1, 5, 5)')
  })

  it('tweens to the end values and calls onComplete; overwrite and kill stop running ones', async () => {
    const el = document.createElement('div')
    const done: string[] = []

    fromTo(el, { opacity: 0 }, { opacity: 1, duration: 0.05, ease: 'none', onComplete: () => done.push('a') })
    await wait(150)
    expect(el.style.opacity).toBe('1')
    expect(done).toEqual(['a'])

    to(el, { x: 100, duration: 0.2, onComplete: () => done.push('killed') })
    to(el, { x: 5, duration: 0.02, overwrite: 'auto' })
    await wait(300)
    expect(el.style.transform).toContain('translate(5px, 0px)')

    to(el, { opacity: 0, duration: 0.2, onComplete: () => done.push('stopped') })
    killTweensOf(el)
    await wait(300)
    expect(done).toEqual(['a'])
  })

  it('tweens plain objects, runs timelines in order and delayed calls', async () => {
    const obj: Record<string, unknown> = { v: 0 }
    const order: string[] = []

    to(obj, { v: 10, duration: 0.02 })
    timeline()
      .to({}, { duration: 0.03, onComplete: () => order.push('first') })
      .to({}, { duration: 0.02, onComplete: () => order.push('second') }, '-=0.01')
    delayedCall(0.01, () => order.push('call'))
    await wait(200)
    expect(obj.v).toBe(10)
    expect(order).toEqual(['call', 'first', 'second'])
  })
})
