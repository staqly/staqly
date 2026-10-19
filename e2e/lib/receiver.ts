// The receiver service (e2e/receiver/server.mjs): records webhook and ntfy
// POSTs, fails or delays them on request, and can be taken off the network.
import { http } from './http.js'
import { docker, RECEIVER, waitForHttp } from './stack.js'

export interface Received {
  path: string
  status: number
  headers: Record<string, string>
  body: string
  at: number
}

export const RECEIVER_CONTAINER = 'staqly-e2e-receiver'

/** Recorded POSTs, optionally only those to one path. */
export async function received(path?: string): Promise<Received[]> {
  const all = (await http(`${RECEIVER}/requests`)).json<Received[]>()

  return path === undefined ? all : all.filter((r) => r.path === path)
}

/** Forgets recorded requests, answers at once and fails the next `n` POSTs (0: accept all). */
export async function resetReceiver(n = 0): Promise<void> {
  await http(`${RECEIVER}/reset`, { method: 'POST' })
  await http(`${RECEIVER}/fail?n=${n}`, { method: 'POST' })
}

export async function failNext(n: number): Promise<void> {
  await http(`${RECEIVER}/fail?n=${n}`, { method: 'POST' })
}

/** Answers every later POST after `ms` milliseconds (0: at once). */
export async function delayReceiver(ms: number): Promise<void> {
  await http(`${RECEIVER}/delay?ms=${ms}`, { method: 'POST' })
}

/** Takes the receiver off the network: deliveries fail to connect. */
export async function stopReceiver(): Promise<void> {
  await docker(['stop', '-t', '1', RECEIVER_CONTAINER])
}

/** Starts it again; it comes back with fresh state, set to accept everything. */
export async function startReceiver(): Promise<void> {
  await docker(['start', RECEIVER_CONTAINER])
  await waitForHttp(`${RECEIVER}/requests`)
  await resetReceiver(0)
}
