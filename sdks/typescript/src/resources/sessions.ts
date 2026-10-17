import type {
  DecisionResult,
  DeviceLink,
  RequestOptions,
  Session,
  SessionCreated,
  SessionRecovered,
  Whoami,
} from '../types.js'
import { APIResource, enc } from './base.js'

/**
 * Browser sessions. Sessions are cookies: `signInWithApiKey` and `recover` set one on the response,
 * which only a cookie-aware `fetch` (a browser with credentials, or a custom
 * `fetch` with a cookie jar) keeps. Listing and revoking work with an `admin` API key.
 */
export class Sessions extends APIResource {
  /** Server mode, workspace, the calling principal with its scopes and the CSRF token (same as `whoami()`). */
  current(options?: RequestOptions): Promise<Whoami> {
    return this.http.request({ method: 'GET', path: '/v1/session', options })
  }

  /** Active browser sessions of the workspace (needs `admin`). */
  async list(options?: RequestOptions): Promise<Session[]> {
    return (await this.http.request<{ data: Session[] }>({ method: 'GET', path: '/v1/sessions', options })).data
  }

  /** Revokes a browser session (needs `admin`). */
  async revoke(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/sessions/${enc(id)}`, options })
  }

  /** Logs out: revokes the current session (the cookie the request carried). */
  async logout(options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: '/v1/session', options })
  }

  /**
   * Cloud mode: exchanges an `admin` API key (the key sign-up or `recover` returns) for a
   * browser session. Sent without credentials (the key goes in the body); other sessions
   * stay signed in. Keys without `admin` throw `PermissionDeniedError` (403), unknown,
   * revoked or expired keys `AuthenticationError` (401), local mode `ConflictError` (409).
   */
  signInWithApiKey(apiKey: string, options?: RequestOptions): Promise<SessionCreated> {
    return this.http.request({
      method: 'POST',
      path: '/v1/sessions/api-key',
      body: { api_key: apiKey },
      noAuth: true,
      options,
    })
  }

  /**
   * Exchanges the recovery key (single use) for a new browser session, a new admin
   * API key and the next recovery key; the old recovery key stops working and every
   * other session is signed out. Existing API keys keep working, except on Free, where
   * the new admin key replaces the old admin keys. Sent without
   * credentials; the session is set as a cookie. Store `api_key.key` and
   * `recovery_key` now: they are shown only once.
   */
  recover(recoveryKey: string, options?: RequestOptions): Promise<SessionRecovered> {
    return this.http.request({
      method: 'POST',
      path: '/v1/sessions/recover',
      body: { recovery_key: recoveryKey },
      noAuth: true,
      options,
    })
  }
}

/** One-time "connect a device" links (the dashboard's QR code). */
export class DeviceLinks extends APIResource {
  /**
   * Creates a sign-in link, valid for 5 minutes and single use (the token is
   * in the URL fragment). Needs a dashboard session or the local principal;
   * API keys get `PermissionDeniedError` (403).
   */
  create(options?: RequestOptions): Promise<DeviceLink> {
    return this.http.request({ method: 'POST', path: '/v1/device-links', options })
  }

  /**
   * Exchanges a device-link token (the part after `#` in the link) for a
   * session cookie. Sent without credentials. Used or expired links throw `GoneError` (410).
   */
  redeem(token: string, options?: RequestOptions): Promise<SessionCreated> {
    return this.http.request({
      method: 'POST',
      path: '/v1/device-links/redeem',
      body: { token },
      noAuth: true,
      options,
    })
  }
}

/** One-tap decision links sent by notification channels (`/v1/decide/{token}`). */
export class DecisionLinks extends APIResource {
  /**
   * Performs the link's action (approve or reject; `feedback` applies to
   * reject only) and consumes the link. Public: the token is the credential,
   * so no API key is sent. Not retried, because a link is single use.
   */
  decide(token: string, input: { feedback?: string } = {}, options?: RequestOptions): Promise<DecisionResult> {
    return this.http.request({
      method: 'POST',
      path: `/v1/decide/${enc(token)}`,
      body: input,
      noAuth: true,
      options: { maxRetries: 0, ...options },
    })
  }
}
