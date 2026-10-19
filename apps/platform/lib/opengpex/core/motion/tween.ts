/**
 * OpenGPEX - An Open-source, Web-based Graphics and Photo editor.
 * Copyright (C) 2026 The OpenGPEX Authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, version 3 of the License.
 *
 * SPDX-License-Identifier: GPL-3.0-only
 */

// staqly: a small tween engine in place of GSAP (whose licence is not open source), covering exactly what the
// editor asks of it through core/motion: to / set / fromTo on elements or plain objects, the transform
// shorthands (x, y, xPercent, yPercent, scale, scaleX, scaleY, rotation) composed like GSAP composes them,
// numeric CSS properties, GSAP-named eases, overwrite, onComplete / onUpdate, timelines with "-=n" / "+=n"
// positions, delayed calls and a requestAnimationFrame ticker with fps, sleep and wake.

export type TweenTarget =
  | Element
  | Record<string, unknown>
  | null
  | undefined
  | (Element | Record<string, unknown> | null | undefined)[]

export interface TweenVars {
  duration?: number
  delay?: number
  ease?: string
  overwrite?: boolean | 'auto'
  onComplete?: () => void
  onUpdate?: () => void
  [prop: string]: unknown
}

export type TimelineVars = TweenVars

const CONTROL = new Set(['duration', 'delay', 'ease', 'overwrite', 'onComplete', 'onUpdate', 'onStart', 'immediateRender'])
const TRANSFORM = new Set(['x', 'y', 'xPercent', 'yPercent', 'scale', 'scaleX', 'scaleY', 'rotation'])
const UNITLESS = new Set(['opacity', 'zIndex', 'fontWeight', 'lineHeight', 'flexGrow', 'flexShrink', 'order'])

// ─── Eases (GSAP names) ──────────────────────────────────────────────────────

type Ease = (t: number) => number

const powerOut = (p: number): Ease => (t) => 1 - (1 - t) ** (p + 1)
const powerIn = (p: number): Ease => (t) => t ** (p + 1)
const inOut = (fIn: Ease): Ease => (t) => (t < 0.5 ? fIn(t * 2) / 2 : 1 - fIn((1 - t) * 2) / 2)

export function parseEase(name: string | undefined): Ease {
  if (!name || name === 'none' || name === 'linear') {
    return name ? (t) => t : powerOut(1)
  }

  const [family, kind = 'out'] = name.split('.')
  const arg = /\(([\d.]+)\)/.exec(name)?.[1]
  const mode = kind.replace(/\(.*$/, '')
  let fIn: Ease

  if (family?.startsWith('power')) {
    fIn = powerIn(Number(family.slice(5)) || 1)
  } else if (family === 'expo') {
    fIn = (t) => (t === 0 ? 0 : 2 ** (10 * (t - 1)))
  } else if (family === 'back') {
    const s = arg ? Number(arg) : 1.70158

    fIn = (t) => t * t * ((s + 1) * t - s)
  } else if (family === 'sine') {
    fIn = (t) => 1 - Math.cos((t * Math.PI) / 2)
  } else {
    fIn = powerIn(1)
  }

  if (mode === 'in') {
    return fIn
  }

  if (mode === 'inOut') {
    return inOut(fIn)
  }

  return (t) => 1 - fIn(1 - t)
}

// ─── Ticker ──────────────────────────────────────────────────────────────────

type TickFn = (time: number, deltaTime: number, frame: number, elapsed: number) => void

const listeners = new Set<TickFn>()
let raf = 0
let start = 0
let last = 0
let frame = 0
let minGap = 0
let asleep = false

function loop(now: number) {
  raf = 0

  if (asleep) {
    return
  }

  if (!start) {
    start = now
    last = now
  }

  if (now - last >= minGap) {
    const delta = now - last

    last = now
    frame++

    for (const fn of [...listeners]) {
      fn((now - start) / 1000, delta, frame, now - start)
    }
  }

  schedule()
}

function schedule() {
  if (!raf && !asleep && listeners.size && typeof requestAnimationFrame !== 'undefined') {
    raf = requestAnimationFrame(loop)
  }
}

export const ticker = {
  add(fn: TickFn) {
    listeners.add(fn)
    schedule()
  },
  remove(fn: TickFn) {
    listeners.delete(fn)
  },
  /** Caps the frame rate (the editor slows to a few frames per second when idle). */
  fps(n: number) {
    minGap = n > 0 ? 1000 / n - 1 : 0
  },
  sleep() {
    asleep = true
  },
  wake() {
    asleep = false
    schedule()
  },
}

// ─── Values ──────────────────────────────────────────────────────────────────

interface TransformState {
  x: number
  y: number
  xPercent: number
  yPercent: number
  scaleX: number
  scaleY: number
  rotation: number
}

const transforms = new WeakMap<Element, TransformState>()

function transformOf(el: Element): TransformState {
  let t = transforms.get(el)

  if (!t) {
    t = { x: 0, y: 0, xPercent: 0, yPercent: 0, scaleX: 1, scaleY: 1, rotation: 0 }
    transforms.set(el, t)
  }

  return t
}

function writeTransform(el: Element) {
  const t = transformOf(el)
  const pct = t.xPercent || t.yPercent ? `translate(${t.xPercent}%, ${t.yPercent}%) ` : ''

  ;(el as HTMLElement).style.transform =
    `${pct}translate(${t.x}px, ${t.y}px) rotate(${t.rotation}deg) scale(${t.scaleX}, ${t.scaleY})`
}

const isElement = (v: unknown): v is Element => typeof Element !== 'undefined' && v instanceof Element

function read(target: Element | Record<string, unknown>, prop: string): unknown {
  if (!isElement(target)) {
    return target[prop]
  }

  if (TRANSFORM.has(prop)) {
    const t = transformOf(target)

    return prop === 'scale' ? t.scaleX : t[prop as keyof TransformState]
  }

  const style = (target as HTMLElement).style as unknown as Record<string, string>
  const inline = style[prop]
  const value = inline !== undefined && inline !== '' ? inline : getComputedStyle(target).getPropertyValue(kebab(prop))

  return value
}

function write(target: Element | Record<string, unknown>, prop: string, value: unknown) {
  if (!isElement(target)) {
    target[prop] = value

    return
  }

  if (TRANSFORM.has(prop)) {
    const t = transformOf(target)
    const n = Number(value)

    if (prop === 'scale') {
      t.scaleX = n
      t.scaleY = n
    } else {
      t[prop as keyof TransformState] = n
    }

    writeTransform(target)

    return
  }

  if (prop === 'transform') {
    // A raw transform replaces the composed one (the shorthands start over from it).
    transforms.delete(target)
  }

  const style = (target as HTMLElement).style as unknown as Record<string, string>

  style[prop] = typeof value === 'number' && !UNITLESS.has(prop) ? `${value}px` : String(value)
}

const kebab = (s: string) => s.replace(/[A-Z]/g, (m) => `-${m.toLowerCase()}`)

function numeric(v: unknown): number | null {
  if (typeof v === 'number') {
    return v
  }

  if (typeof v === 'string' && /^-?[\d.]+(px|deg)?$/.test(v.trim())) {
    return Number.parseFloat(v)
  }

  return null
}

// ─── Tweens ──────────────────────────────────────────────────────────────────

interface Track {
  target: Element | Record<string, unknown>
  prop: string
  from: number
  to: number
}

export interface Tween {
  kill(): void
  readonly targets: (Element | Record<string, unknown>)[]
}

const active = new Set<TweenImpl>()

class TweenImpl implements Tween {
  tracks: Track[] = []
  startAt = 0
  killed = false

  constructor(
    readonly targets: (Element | Record<string, unknown>)[],
    readonly vars: TweenVars,
    readonly ease: Ease,
    readonly duration: number,
    readonly delay: number,
  ) {}

  begin(now: number) {
    this.startAt = now + this.delay * 1000

    for (const target of this.targets) {
      for (const [prop, to] of Object.entries(this.vars)) {
        if (CONTROL.has(prop) || prop.startsWith('on')) {
          continue
        }

        const end = numeric(to)
        const from = numeric(read(target, prop))

        if (end === null || from === null) {
          write(target, prop, to) // not tweenable (display, colours, shadows): set at the start
        } else {
          this.tracks.push({ target, prop, from, to: end })
        }
      }
    }
  }

  step(now: number): boolean {
    if (this.killed) {
      return true
    }

    if (now < this.startAt) {
      return false
    }

    const p = this.duration <= 0 ? 1 : Math.min(1, (now - this.startAt) / (this.duration * 1000))
    const e = this.ease(p)

    for (const t of this.tracks) {
      write(t.target, t.prop, p >= 1 ? t.to : t.from + (t.to - t.from) * e)
    }

    this.vars.onUpdate?.()

    if (p >= 1) {
      this.vars.onComplete?.()

      return true
    }

    return false
  }

  kill() {
    this.killed = true
    active.delete(this)
  }
}

function tick() {
  const now = performance.now()

  for (const tw of [...active]) {
    if (tw.step(now)) {
      active.delete(tw)
    }
  }

  if (!active.size) {
    ticker.remove(tick)
  }
}

function toTargets(target: TweenTarget): (Element | Record<string, unknown>)[] {
  if (!target) {
    return []
  }

  return (Array.isArray(target) ? target : [target]).filter((t): t is Element | Record<string, unknown> => !!t)
}

/** Stops the running tweens of these targets (all of them, or only the ones on these properties). */
export function killTweensOf(target: TweenTarget, props?: string[]) {
  const targets = new Set(toTargets(target))

  for (const tw of active) {
    if (!tw.targets.some((t) => targets.has(t))) {
      continue
    }

    if (!props) {
      tw.kill()
      continue
    }

    const before = tw.tracks.length

    tw.tracks = tw.tracks.filter((tr) => !(targets.has(tr.target) && props.includes(tr.prop)))

    // Like GSAP: a tween left with nothing to animate is gone (no onComplete).
    if (before && !tw.tracks.length) {
      tw.kill()
    }
  }
}

export function to(target: TweenTarget, vars: TweenVars): Tween {
  const targets = toTargets(target)
  const duration = vars.duration ?? 0.5

  if (vars.overwrite) {
    killTweensOf(targets, Object.keys(vars).filter((k) => !CONTROL.has(k)))
  }

  const tw = new TweenImpl(targets, vars, parseEase(vars.ease), duration, vars.delay ?? 0)

  tw.begin(performance.now())

  if (duration <= 0 && !vars.delay) {
    tw.step(Number.POSITIVE_INFINITY)

    return tw
  }

  active.add(tw)
  ticker.add(tick)

  return tw
}

export function set(target: TweenTarget, vars: TweenVars): Tween {
  return to(target, { ...vars, duration: 0, delay: 0 })
}

export function fromTo(target: TweenTarget, from: TweenVars, toVars: TweenVars): Tween {
  set(target, { ...from, overwrite: toVars.overwrite })

  return to(target, toVars)
}

export function delayedCall(delay: number, callback: (...args: unknown[]) => void): Tween {
  const id = setTimeout(callback, delay * 1000)

  return { targets: [], kill: () => clearTimeout(id) }
}

/** A sequence: each step starts after the previous one, or at "-=s" / "+=s" from its end. */
export function timeline(vars: TimelineVars = {}) {
  let cursor = vars.delay ?? 0
  const at = (position?: string | number) => {
    if (typeof position === 'number') {
      return position
    }

    const m = position ? /^([+-])=([\d.]+)$/.exec(position) : null

    return m ? cursor + (m[1] === '-' ? -1 : 1) * Number(m[2]) : cursor
  }
  const tl = {
    to(target: TweenTarget, v: TweenVars, position?: string | number) {
      const startAt = at(position)

      to(target, { ...v, delay: startAt + (v.delay ?? 0) })
      cursor = startAt + (v.delay ?? 0) + (v.duration ?? 0.5)

      return tl
    },
    fromTo(target: TweenTarget, from: TweenVars, v: TweenVars, position?: string | number) {
      const startAt = at(position)

      if (startAt <= 0) {
        set(target, from)
        to(target, { ...v, delay: v.delay ?? 0 })
      } else {
        delayedCall(startAt, () => {
          set(target, from)
          to(target, v)
        })
      }

      cursor = startAt + (v.delay ?? 0) + (v.duration ?? 0.5)

      return tl
    },
    set(target: TweenTarget, v: TweenVars, position?: string | number) {
      return tl.to(target, { ...v, duration: 0 }, position)
    },
  }

  return tl
}
