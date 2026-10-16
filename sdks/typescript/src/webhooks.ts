// Webhook signature verification. Web Crypto only, so it runs in Node ≥20,
// Bun, Deno, Cloudflare Workers, Vercel/Next.js edge and browsers. Importable
// on its own as `@staqly/sdk/webhooks`.
import { WebhookVerificationError } from './errors.js'
import type { WebhookEvent } from './types.js'

export { WebhookVerificationError } from './errors.js'
export type { ItemEventData, WebhookEvent } from './types.js'

export const SIGNATURE_HEADER = 'x-staqly-signature'
export const TIMESTAMP_HEADER = 'x-staqly-timestamp'
export const EVENT_HEADER = 'x-staqly-event'
export const EVENT_ID_HEADER = 'x-staqly-event-id'
export const DELIVERY_HEADER = 'x-staqly-delivery'
/** Default maximum age (and clock skew) of a delivery, in seconds. */
export const DEFAULT_TOLERANCE_SECONDS = 300

/** Headers as a Fetch `Headers`, a Node `IncomingHttpHeaders`, or a plain object. */
export type HeadersLike =
  | Headers
  | { get(name: string): string | null }
  | Record<string, string | string[] | undefined | null>

export interface VerifyOptions {
  /** Reject deliveries whose timestamp is further than this from now (seconds, default 300; 0 disables). */
  tolerance?: number
  /** Current time in seconds (for tests). */
  now?: number
}

const encoder = new TextEncoder()

function header(headers: HeadersLike, name: string): string | undefined {
  if (typeof (headers as Headers).get === 'function') {
    return (headers as Headers).get(name) ?? undefined
  }

  const rec = headers as Record<string, string | string[] | undefined | null>

  for (const [k, v] of Object.entries(rec)) {
    if (k.toLowerCase() === name) {
      return (Array.isArray(v) ? v[0] : v) ?? undefined
    }
  }

  return undefined
}

function toBytes(body: string | ArrayBuffer | ArrayBufferView): Uint8Array<ArrayBuffer> {
  if (typeof body === 'string') {
    return encoder.encode(body)
  }

  if (ArrayBuffer.isView(body)) {
    return new Uint8Array(body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength) as ArrayBuffer)
  }

  return new Uint8Array(body.slice(0))
}

function hexToBytes(hex: string): Uint8Array<ArrayBuffer> | undefined {
  if (hex.length % 2 !== 0 || !/^[0-9a-fA-F]*$/.test(hex)) {
    return undefined
  }

  const out = new Uint8Array(hex.length / 2)

  for (let i = 0; i < out.length; i++) {
    out[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16)
  }

  return out
}

function hmacKey(secret: string, usage: 'sign' | 'verify'): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', encoder.encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, [usage])
}

function signedMessage(timestamp: string, body: Uint8Array): Uint8Array<ArrayBuffer> {
  const prefix = encoder.encode(`${timestamp}.`)
  const msg = new Uint8Array(prefix.length + body.length)

  msg.set(prefix)
  msg.set(body, prefix.length)

  return msg
}

/**
 * Computes the `X-Staqly-Signature` value for a body:
 * `sha256=` + hex(HMAC-SHA256(secret, `${timestamp}.${rawBody}`)).
 * Useful for tests of your webhook handler.
 */
export async function computeWebhookSignature(
  secret: string,
  timestamp: number | string,
  rawBody: string | ArrayBuffer | ArrayBufferView,
): Promise<string> {
  const key = await hmacKey(secret, 'sign')
  const mac = new Uint8Array(await crypto.subtle.sign('HMAC', key, signedMessage(String(timestamp), toBytes(rawBody))))

  return `sha256=${Array.from(mac, (b) => b.toString(16).padStart(2, '0')).join('')}`
}

/**
 * Verifies a staqly webhook delivery and returns the parsed event.
 *
 * Pass the **raw** request body exactly as received (string or bytes) —
 * re-serialised JSON will not match. Throws `WebhookVerificationError` when the
 * signature is missing/invalid or the timestamp is outside `tolerance`.
 * The comparison is constant-time (`crypto.subtle.verify`).
 */
export async function verifyWebhook<D = Record<string, unknown>>(
  secret: string,
  rawBody: string | ArrayBuffer | ArrayBufferView,
  headers: HeadersLike,
  options: VerifyOptions = {},
): Promise<WebhookEvent<D>> {
  if (!secret) {
    throw new WebhookVerificationError('Webhook secret is empty')
  }

  const signature = header(headers, SIGNATURE_HEADER)?.trim()
  const timestamp = header(headers, TIMESTAMP_HEADER)?.trim()

  if (!signature) {
    throw new WebhookVerificationError('Missing X-Staqly-Signature header')
  }

  if (!timestamp || !/^\d+$/.test(timestamp)) {
    throw new WebhookVerificationError('Missing or invalid X-Staqly-Timestamp header')
  }

  const tolerance = options.tolerance ?? DEFAULT_TOLERANCE_SECONDS
  const now = options.now ?? Math.floor(Date.now() / 1000)

  if (tolerance > 0 && Math.abs(now - Number(timestamp)) > tolerance) {
    throw new WebhookVerificationError(`Webhook timestamp is outside the ${tolerance}s tolerance`)
  }

  const body = toBytes(rawBody)
  const candidates = signature
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.startsWith('sha256='))
    .map((s) => hexToBytes(s.slice('sha256='.length)))
    .filter((b): b is Uint8Array<ArrayBuffer> => b !== undefined && b.length === 32)

  if (candidates.length === 0) {
    throw new WebhookVerificationError('Malformed X-Staqly-Signature header')
  }

  const key = await hmacKey(secret, 'verify')
  const msg = signedMessage(timestamp, body)
  let ok = false

  for (const sig of candidates) {
    ok = (await crypto.subtle.verify('HMAC', key, sig, msg)) || ok
  }

  if (!ok) {
    throw new WebhookVerificationError('Webhook signature does not match')
  }

  try {
    return JSON.parse(new TextDecoder().decode(body)) as WebhookEvent<D>
  } catch (err) {
    throw new WebhookVerificationError('Webhook body is not valid JSON', { cause: err })
  }
}

/** Boolean form of `verifyWebhook` (signature + timestamp only). */
export async function isValidWebhook(
  secret: string,
  rawBody: string | ArrayBuffer | ArrayBufferView,
  headers: HeadersLike,
  options: VerifyOptions = {},
): Promise<boolean> {
  try {
    await verifyWebhook(secret, rawBody, headers, options)

    return true
  } catch {
    return false
  }
}
