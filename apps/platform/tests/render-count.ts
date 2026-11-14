/**
 * Counts how often each function component renders, the way React DevTools' "highlight updates" does: a
 * minimal DevTools global hook receives every commit and walks the fibers that took part in it. Loaded as a
 * setup file because react-dom looks for the hook once, when it is first imported.
 */

interface Fiber {
  tag: number
  type: unknown
  flags: number
  alternate: Fiber | null
  child: Fiber | null
  sibling: Fiber | null
}

const FUNCTION = 0
const FORWARD_REF = 11
const SIMPLE_MEMO = 15
const PERFORMED_WORK = 1

const counts = new Map<string, number>()

function nameOf(fiber: Fiber): string | null {
  if (fiber.tag !== FUNCTION && fiber.tag !== FORWARD_REF && fiber.tag !== SIMPLE_MEMO) {
    return null
  }

  const type = (fiber.tag === FORWARD_REF ? (fiber.type as { render?: unknown }).render : fiber.type) as
    | { displayName?: string; name?: string }
    | undefined

  return type?.displayName || type?.name || 'Anonymous'
}

function hit(fiber: Fiber) {
  const name = nameOf(fiber)

  if (name) {
    counts.set(name, (counts.get(name) ?? 0) + 1)
  }
}

function mount(fiber: Fiber) {
  hit(fiber)

  for (let c = fiber.child; c; c = c.sibling) {
    mount(c)
  }
}

function update(next: Fiber, prev: Fiber) {
  if ((next.flags & PERFORMED_WORK) === PERFORMED_WORK) {
    hit(next)
  }

  // Same child list: React skipped the whole subtree (its fibers still carry flags from older commits).
  if (next.child === prev.child) {
    return
  }

  for (let c = next.child; c; c = c.sibling) {
    if (c.alternate) {
      update(c, c.alternate)
    } else {
      mount(c)
    }
  }
}

const hook = {
  supportsFiber: true,
  renderers: new Map<number, unknown>(),
  inject(internals: unknown) {
    const id = hook.renderers.size + 1

    hook.renderers.set(id, internals)

    return id
  },
  onCommitFiberRoot(_id: number, root: { current: Fiber }) {
    const next = root.current
    const prev = next.alternate

    if (prev) {
      update(next, prev)
    } else {
      mount(next)
    }
  },
  onPostCommitFiberRoot() {},
  onCommitFiberUnmount() {},
  checkDCE() {},
  onScheduleFiberRoot() {},
  setStrictMode() {},
}

const g = globalThis as Record<string, unknown>

// The name React looks for; react-dom calls hook.inject() when it loads.
g['__REACT_DEVTOOLS_GLOBAL_HOOK__'] ??= hook

export const renders = {
  reset: () => counts.clear(),
  of: (name: string) => counts.get(name) ?? 0,
  all: () => Object.fromEntries([...counts].toSorted((a, b) => b[1] - a[1])),
}
