/**
 * Passkeys in the browser: the server sends WebAuthn options as JSON (binary fields base64url) and takes the
 * credential back as JSON. Browsers with PublicKeyCredential.parse*FromJSON and toJSON() do the conversion;
 * the fallbacks below cover the ones without (the few fields that are binary).
 */

type Json = Record<string, unknown>

const fromB64 = (s: string) => {
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/')
  const bin = atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4))

  return Uint8Array.from(bin, (c) => c.charCodeAt(0)).buffer
}

const toB64 = (buf: ArrayBuffer) =>
  btoa(String.fromCharCode(...new Uint8Array(buf)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

type Descriptor = { id: string; type: string; transports?: string[] }

const descriptors = (list?: Descriptor[]) => list?.map((d) => ({ ...d, id: fromB64(d.id) }))

// The JSON helpers are newer than the TypeScript DOM types this project builds with.
type PKC = typeof PublicKeyCredential & {
  parseCreationOptionsFromJSON?: (o: Json) => PublicKeyCredentialCreationOptions
  parseRequestOptionsFromJSON?: (o: Json) => PublicKeyCredentialRequestOptions
}

const PKCtor = () => (typeof window === 'undefined' ? undefined : (window.PublicKeyCredential as PKC | undefined))

/** This browser can create and use passkeys. */
export function passkeysSupported() {
  return !!PKCtor() && typeof navigator.credentials?.create === 'function'
}

function creationOptions(o: Json): PublicKeyCredentialCreationOptions {
  const P = PKCtor()

  if (P?.parseCreationOptionsFromJSON) {
    return P.parseCreationOptionsFromJSON(o)
  }

  const user = o.user as Json

  return {
    ...(o as unknown as PublicKeyCredentialCreationOptions),
    challenge: fromB64(o.challenge as string),
    user: { ...(user as unknown as PublicKeyCredentialUserEntity), id: fromB64(user.id as string) },
    excludeCredentials: descriptors(o.excludeCredentials as Descriptor[]) as PublicKeyCredentialDescriptor[],
  }
}

function requestOptions(o: Json): PublicKeyCredentialRequestOptions {
  const P = PKCtor()

  if (P?.parseRequestOptionsFromJSON) {
    return P.parseRequestOptionsFromJSON(o)
  }

  return {
    ...(o as unknown as PublicKeyCredentialRequestOptions),
    challenge: fromB64(o.challenge as string),
    allowCredentials: descriptors(o.allowCredentials as Descriptor[]) as PublicKeyCredentialDescriptor[],
  }
}

function credentialJSON(c: PublicKeyCredential): Json {
  // Older browsers lack toJSON() although the DOM types declare it.
  const withJSON = c as { toJSON?: () => unknown }

  if (typeof withJSON.toJSON === 'function') {
    return withJSON.toJSON() as Json
  }

  const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse
  const response: Json = { clientDataJSON: toB64(r.clientDataJSON) }

  if ('attestationObject' in r && r.attestationObject) {
    response.attestationObject = toB64(r.attestationObject)
    response.transports = r.getTransports?.() ?? []
  } else {
    response.authenticatorData = toB64(r.authenticatorData)
    response.signature = toB64(r.signature)

    if (r.userHandle) {
      response.userHandle = toB64(r.userHandle)
    }
  }

  return {
    id: c.id,
    rawId: toB64(c.rawId),
    type: c.type,
    response,
    clientExtensionResults: c.getClientExtensionResults(),
  }
}

/** navigator.credentials.create with the server's options; the credential as JSON for the server. */
export async function createPasskey(options: Json) {
  const c = (await navigator.credentials.create({ publicKey: creationOptions(options) })) as PublicKeyCredential | null

  if (!c) {
    throw new DOMException('No passkey was created', 'NotAllowedError')
  }

  return credentialJSON(c)
}

/** navigator.credentials.get: a prompt, or the address field's autofill with mediation "conditional". */
export async function getPasskey(
  options: Json,
  init?: { mediation?: CredentialMediationRequirement; signal?: AbortSignal },
) {
  const c = (await navigator.credentials.get({
    publicKey: requestOptions(options),
    ...init,
  })) as PublicKeyCredential | null

  if (!c) {
    throw new DOMException('No passkey was chosen', 'NotAllowedError')
  }

  return credentialJSON(c)
}

/** Autofill of passkeys in the address field (conditional mediation) works in this browser. */
export async function passkeyAutofillAvailable() {
  const P = PKCtor() as (PKC & { isConditionalMediationAvailable?: () => Promise<boolean> }) | undefined

  return !!(await P?.isConditionalMediationAvailable?.().catch(() => false))
}

/** The person cancelled the browser's prompt (or it timed out): nothing to report. */
export function passkeyCancelled(e: unknown) {
  return e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')
}

/** A name for a new passkey from the device: "MacBook", "iPhone", "Windows PC". */
export function deviceName() {
  const ua = typeof navigator === 'undefined' ? '' : navigator.userAgent

  if (/iPhone/.test(ua)) {
    return 'iPhone'
  }

  if (/iPad/.test(ua)) {
    return 'iPad'
  }

  if (/Android/.test(ua)) {
    return 'Android phone'
  }

  if (/Macintosh|Mac OS X/.test(ua)) {
    return 'Mac'
  }

  if (/Windows/.test(ua)) {
    return 'Windows PC'
  }

  if (/Linux/.test(ua)) {
    return 'Linux computer'
  }

  return 'Passkey'
}
