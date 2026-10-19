export interface WaitOptions {
  /** Give up after this many milliseconds (default 30 s). */
  timeout?: number
  /** Pause between attempts in milliseconds (default 500 ms). */
  interval?: number
  /** Error message on timeout. */
  message?: string
}

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/** Calls `check` until it returns a truthy value and returns that value. */
export async function waitFor<T>(check: () => Promise<T> | T, options: WaitOptions = {}): Promise<NonNullable<T>> {
  const { timeout = 30_000, interval = 500, message = 'condition not met' } = options
  const deadline = Date.now() + timeout
  let last: T | undefined

  while (Date.now() < deadline) {
    last = await check()

    if (last) {
      return last
    }

    await sleep(interval)
  }

  throw new Error(`timeout after ${timeout} ms: ${message} (last value: ${JSON.stringify(last)})`)
}
