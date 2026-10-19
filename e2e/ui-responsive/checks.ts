// Layout checks that run inside the page (page.evaluate). Plain DOM code, no imports: Playwright
// serialises the function into the browser.
/* oxlint-disable unicorn/consistent-function-scoping -- layoutReport is serialised into the page on its own, so its helpers must live inside it */

export interface Offender {
  selector: string
  text: string
  rect: { x: number; y: number; w: number; h: number }
  detail?: string
}

export interface LayoutReport {
  url: string
  viewport: { w: number; h: number }
  /** documentElement.scrollWidth - clientWidth (> 0: the page scrolls sideways) */
  overflowX: number
  /** visible elements that stick out of the viewport sideways without a scrolling ancestor */
  outside: Offender[]
  /** interactive elements that overlap each other (neither contains the other) */
  overlaps: Offender[]
  /** interactive elements under 40 px on a touch screen */
  smallTargets: Offender[]
  /** ellipsised/clamped text with no title, aria-label or tooltip to read it in full */
  truncated: Offender[]
  /** content that ends under a fixed bar when scrolled to the bottom */
  covered: Offender[]
  /** outermost elements wider than their parent: where an overflow starts */
  culprits: Offender[]
  /** dialogs, sheets, menus and the sidebar whose content is wider than they are (they scroll or clip sideways) */
  sideways: Offender[]
}

export interface CheckOptions {
  touch: boolean
  /** smallest touch target, px */
  minTarget?: number
}

/** Runs in the browser. */
export function layoutReport(opts: CheckOptions): LayoutReport {
  const vw = document.documentElement.clientWidth
  const vh = window.innerHeight
  const minTarget = opts.minTarget ?? 40

  const describe = (el: Element): string => {
    const parts: string[] = []
    let e: Element | null = el

    for (let i = 0; e && i < 4; i++, e = e.parentElement) {
      let s = e.tagName.toLowerCase()
      const testid = e.getAttribute('data-testid')
      const slot = e.getAttribute('data-slot')

      if (testid) {
        s += `[data-testid=${testid}]`
      } else if (slot) {
        s += `[data-slot=${slot}]`
      } else if (e.id && !/^(radix|_r_)/.test(e.id)) {
        s += `#${e.id}`
      } else if (typeof e.className === 'string' && e.className) {
        s += '.' + e.className.trim().split(/\s+/).slice(0, 3).join('.')
      }

      parts.unshift(s)

      if (testid || slot) {
        break
      }
    }

    return parts.join(' > ')
  }

  const rectOf = (el: Element) => {
    const r = el.getBoundingClientRect()

    return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }
  }

  const text = (el: Element) => (el.textContent ?? '').replace(/\s+/g, ' ').trim().slice(0, 60)

  const offender = (el: Element, detail?: string): Offender => ({
    selector: describe(el),
    text: text(el) || el.getAttribute('aria-label') || '',
    rect: rectOf(el),
    detail,
  })

  const hiddenByAncestor = (el: Element) =>
    !!el.closest('[aria-hidden="true"], [inert], .sr-only, [data-state="closed"], [hidden]')

  const visible = (el: Element) => {
    if (hiddenByAncestor(el)) {
      return false
    }

    if (!el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) {
      return false
    }

    const r = el.getBoundingClientRect()

    return r.width > 0 && r.height > 0
  }

  // Ancestor that clips or scrolls horizontally: anything inside it can't widen the page.
  const clippedX = (el: Element) => {
    for (let e = el.parentElement; e && e !== document.body; e = e.parentElement) {
      const ox = getComputedStyle(e).overflowX

      if (ox !== 'visible') {
        return true
      }
    }

    return false
  }

  const outside: Offender[] = []
  const all = Array.from(document.body.querySelectorAll('*'))

  for (const el of all) {
    if (el instanceof SVGElement && !(el instanceof SVGSVGElement)) {
      continue
    }

    const r = el.getBoundingClientRect()

    if ((r.right > vw + 1 || r.left < -1) && r.width > 0 && visible(el) && !clippedX(el)) {
      const pos = getComputedStyle(el).position

      // fully off-screen fixed elements (closed drawers) are not overflow
      if (pos === 'fixed' && (r.right <= 0 || r.left >= vw)) {
        continue
      }

      // report the outermost offender only
      if (!outside.some((o) => o.selector && el.parentElement && describe(el.parentElement) === o.selector)) {
        outside.push(offender(el, `left ${Math.round(r.left)} right ${Math.round(r.right)} > ${vw}`))
      }
    }
  }

  // Where the widening starts: the outermost elements wider than their parent's content box.
  const culprits: Offender[] = []

  if (outside.length) {
    for (const el of all) {
      const parent = el.parentElement

      if (!parent || !visible(el) || clippedX(el) || ['fixed', 'absolute'].includes(getComputedStyle(el).position)) {
        continue
      }

      const pcs = getComputedStyle(parent)
      const inner = parent.clientWidth - parseFloat(pcs.paddingLeft) - parseFloat(pcs.paddingRight)
      const w = el.getBoundingClientRect().width

      if (w > inner + 1 && w > 40 && !culprits.some((c) => c.selector === describe(el))) {
        // skip if an ancestor is already a culprit
        let nested = false

        for (let a: Element | null = parent; a; a = a.parentElement) {
          if (culprits.some((c) => (c as Offender & { el?: Element }).el === a)) {
            nested = true
            break
          }
        }

        if (!nested) {
          const o = offender(el, `${Math.round(w)} > parent ${Math.round(inner)}`) as Offender & { el?: Element }

          Object.defineProperty(o, 'el', { value: el, enumerable: false })
          culprits.push(o)
        }
      }
    }
  }

  // Overlays and the sidebar scroll vertically; content wider than them is cut off or scrolls sideways.
  const sideways: Offender[] = []

  for (const el of document.querySelectorAll(
    '[data-slot=dialog-content], [data-slot=sheet-content], [data-slot=alert-dialog-content], [data-slot=popover-content], [data-slot=dropdown-menu-content], [data-slot=select-content], [data-slot=command-list], [data-sidebar=content]',
  )) {
    // overflow-x: hidden is a deliberate clip (the collapsed icon rail, cmdk lists); auto/scroll scrolls sideways
    const ox = getComputedStyle(el).overflowX

    if (visible(el) && (ox === 'auto' || ox === 'scroll') && el.scrollWidth > el.clientWidth + 1) {
      let widest: Element = el

      for (const child of el.querySelectorAll('*')) {
        if (
          child.getBoundingClientRect().right > widest.getBoundingClientRect().right ||
          child.getBoundingClientRect().left < el.getBoundingClientRect().left - 1
        ) {
          widest = child
        }
      }

      sideways.push(offender(el, `content ${el.scrollWidth} wide in ${el.clientWidth}; widest: ${describe(widest)}`))
    }
  }

  // Unbreakable text spilling out of its own box (a long URL in a paragraph) widens the page too.
  if (document.documentElement.scrollWidth > vw) {
    for (const el of all) {
      const hasText = Array.from(el.childNodes).some((n) => n.nodeType === Node.TEXT_NODE && n.textContent?.trim())

      if (
        hasText &&
        el.scrollWidth > el.clientWidth + 1 &&
        getComputedStyle(el).overflowX === 'visible' &&
        el.getBoundingClientRect().left + el.scrollWidth > vw + 1 &&
        visible(el) &&
        !clippedX(el)
      ) {
        culprits.push(offender(el, `text ${el.scrollWidth} wide in a ${el.clientWidth} box`))
      }
    }
  }

  const interactive = Array.from(
    document.querySelectorAll<HTMLElement>(
      'a[href], button, input:not([type=hidden]), select, textarea, [role=button], [role=tab], [role=checkbox], [role=switch], [role=radio], [role=menuitem], [role=option], [role=combobox], [tabindex="0"]',
    ),
  ).filter((el) => visible(el) && !el.closest('[data-ui-check-ignore]'))

  // fixed and sticky chrome may cover scrolling content; that is checked by `covered`
  const floating = (el: Element) => {
    for (let e: Element | null = el; e && e !== document.body; e = e.parentElement) {
      const pos = getComputedStyle(e).position

      if (pos === 'fixed' || pos === 'sticky') {
        return true
      }
    }

    return false
  }

  // overlaps
  const overlaps: Offender[] = []
  // Content boxes, cut to what scrolling or clipping ancestors show: padding that only enlarges a hit area
  // (pointer-coarse:py-3 -my-3) is not a visual overlap, nor is a tab scrolled out of its strip.
  const contentBox = (el: Element) => {
    const r = el.getBoundingClientRect()
    const cs = getComputedStyle(el)
    const pt = parseFloat(cs.paddingTop) || 0
    const pb = parseFloat(cs.paddingBottom) || 0
    const mt = Math.max(0, -(parseFloat(cs.marginTop) || 0))
    const mb = Math.max(0, -(parseFloat(cs.marginBottom) || 0))
    let top = r.top + Math.min(pt, mt)
    let bottom = r.bottom - Math.min(pb, mb)
    let left = r.left
    let right = r.right

    for (let a = el.parentElement; a && a !== document.body; a = a.parentElement) {
      if (getComputedStyle(a).overflowX !== 'visible') {
        const c = a.getBoundingClientRect()

        left = Math.max(left, c.left)
        right = Math.min(right, c.right)
        top = Math.max(top, c.top)
        bottom = Math.min(bottom, c.bottom)
      }
    }

    return { left, right, top, bottom }
  }
  const boxes = interactive.map((el) => ({ el, r: contentBox(el) }))

  for (let i = 0; i < boxes.length; i++) {
    for (let j = i + 1; j < boxes.length; j++) {
      const a = boxes[i]!
      const b = boxes[j]!

      if (a.el.contains(b.el) || b.el.contains(a.el) || floating(a.el) || floating(b.el)) {
        continue
      }

      const w = Math.min(a.r.right, b.r.right) - Math.max(a.r.left, b.r.left)
      const h = Math.min(a.r.bottom, b.r.bottom) - Math.max(a.r.top, b.r.top)

      if (w > 4 && h > 4) {
        // Only if b is really painted on top of a at the overlap's center (not just a box overlap of a
        // transparent wrapper), or vice versa.
        const cx = Math.max(a.r.left, b.r.left) + w / 2
        const cy = Math.max(a.r.top, b.r.top) + h / 2

        if (cx < 0 || cy < 0 || cx > vw || cy > vh) {
          continue
        }

        const top = document.elementFromPoint(cx, cy)

        if (top && (a.el.contains(top) || b.el.contains(top))) {
          const hidden = a.el.contains(top) ? b.el : a.el

          overlaps.push(
            offender(hidden, `under ${describe(a.el.contains(top) ? a.el : b.el)} (${Math.round(w)}x${Math.round(h)})`),
          )
        }
      }
    }
  }

  // touch targets
  const smallTargets: Offender[] = []

  if (opts.touch) {
    for (const el of interactive) {
      const r = el.getBoundingClientRect()

      if (r.width >= minTarget && r.height >= minTarget) {
        continue
      }

      // inline links in running text are exempt (WCAG 2.5.8 inline exception)
      if (
        el.tagName === 'A' &&
        getComputedStyle(el).display === 'inline' &&
        Array.from(el.parentElement?.childNodes ?? []).some(
          (n) => n.nodeType === Node.TEXT_NODE && n.textContent?.trim(),
        )
      ) {
        continue
      }

      // A hit area grown by an absolute pseudo-element counts: after:-inset-2 on the control itself, or
      // after:inset-0 stretched over a positioned ancestor (a whole table row or card that is the link).
      const hit = (s: CSSStyleDeclaration) => {
        if (s.content === 'none' || s.position !== 'absolute') {
          return null
        }

        let box: Element = el

        if (getComputedStyle(el).position === 'static') {
          for (let a = el.parentElement; a; a = a.parentElement) {
            if (getComputedStyle(a).position !== 'static') {
              box = a
              break
            }
          }
        }

        const b = box.getBoundingClientRect()
        const px = (v: string) => (v === 'auto' ? 0 : parseFloat(v) || 0)

        return {
          w: b.width - px(s.left) - px(s.right),
          h: b.height - px(s.top) - px(s.bottom),
        }
      }
      const grown = [hit(getComputedStyle(el, '::after')), hit(getComputedStyle(el, '::before'))].filter((x) => !!x)

      if (grown.some((g) => Math.max(g.w, r.width) >= minTarget && Math.max(g.h, r.height) >= minTarget)) {
        continue
      }

      // padding-block with an equal negative margin (pointer-coarse:py-3 -my-3) is already in the rect

      // inputs inside a larger clickable label / input group, and the keyboard path into a row that is itself
      // clickable (a table row with cursor: pointer), count as the larger box
      let holder: Element | null = el.closest('label, [data-slot=input-group], [data-slot=chips-input]')

      for (let a = el.parentElement, i = 0; !holder && a && i < 4; a = a.parentElement, i++) {
        const ar = a.getBoundingClientRect()

        if (getComputedStyle(a).cursor === 'pointer' && ar.width >= minTarget && ar.height >= minTarget) {
          holder = a
        }
      }

      if (holder) {
        const hr = holder.getBoundingClientRect()

        if (hr.width >= minTarget && hr.height >= minTarget) {
          continue
        }
      }

      smallTargets.push(offender(el, `${Math.round(r.width)}x${Math.round(r.height)}`))
    }
  }

  // truncated text without a way to read it
  const truncated: Offender[] = []

  for (const el of all) {
    if (!(el instanceof HTMLElement) || !visible(el)) {
      continue
    }

    const cs = getComputedStyle(el)
    const ellipsis = cs.textOverflow === 'ellipsis' && el.scrollWidth > el.clientWidth + 1
    const clamped = cs.webkitLineClamp !== 'none' && cs.webkitLineClamp !== '' && el.scrollHeight > el.clientHeight + 1

    if (!ellipsis && !clamped) {
      continue
    }

    let readable = false

    for (let e: HTMLElement | null = el, i = 0; e && i < 5; e = e.parentElement, i++) {
      if (
        e.title ||
        e.getAttribute('aria-label') ||
        e.getAttribute('data-full-text') !== null ||
        e.getAttribute('data-state') === 'delayed-open' ||
        (e.hasAttribute('data-slot') && e.getAttribute('data-slot') === 'tooltip-trigger')
      ) {
        readable = true
        break
      }
    }

    // a link or button that opens the full thing (row → detail) is a way to read it
    if (!readable && el.closest('a[href], button, [role=button], [role=option], tr[tabindex], [data-opens-detail]')) {
      readable = true
    }

    if (!readable) {
      truncated.push(offender(el, ellipsis ? 'ellipsis' : 'line-clamp'))
    }
  }

  // content under fixed bars, at the bottom of the page
  const covered: Offender[] = []
  const bars = Array.from(document.querySelectorAll<HTMLElement>('body *')).filter((el) => {
    const cs = getComputedStyle(el)

    if (cs.position !== 'fixed' || !visible(el)) {
      return false
    }

    const r = el.getBoundingClientRect()

    return r.width > vw * 0.6 && r.bottom >= vh - 1 && r.top > vh / 2
  })

  if (bars.length) {
    const barTop = Math.min(...bars.map((b) => b.getBoundingClientRect().top))
    const scroller = document.scrollingElement ?? document.documentElement
    const atBottom = scroller.scrollTop + vh >= scroller.scrollHeight - 2

    if (atBottom) {
      for (const el of interactive) {
        if (bars.some((b) => b.contains(el)) || getComputedStyle(el).position === 'fixed') {
          continue
        }

        const r = el.getBoundingClientRect()

        if (r.bottom > barTop + 2 && r.top < vh && !el.closest('[role=dialog], [data-slot=sidebar]')) {
          const top = document.elementFromPoint(r.left + r.width / 2, Math.min(r.bottom - 2, vh - 1))

          if (top && bars.some((b) => b.contains(top))) {
            covered.push(offender(el, `bottom ${Math.round(r.bottom)} > bar top ${Math.round(barTop)}`))
          }
        }
      }
    }
  }

  return {
    url: location.pathname + location.search,
    viewport: { w: vw, h: vh },
    // On a phone, content wider than the screen inside a fixed dialog or sheet doesn't widen the document but
    // widens the layout viewport (the browser zooms out): innerWidth grows past the device width.
    overflowX: Math.max(
      document.documentElement.scrollWidth - document.documentElement.clientWidth,
      opts.touch ? window.innerWidth - document.documentElement.clientWidth : 0,
    ),
    outside: outside.slice(0, 20),
    overlaps: overlaps.slice(0, 20),
    smallTargets: smallTargets.slice(0, 40),
    truncated: truncated.slice(0, 20),
    covered: covered.slice(0, 10),
    culprits: culprits.slice(0, 10),
    sideways: sideways.slice(0, 10),
  }
}
