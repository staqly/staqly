import type {
  Channel,
  ChannelInput,
  ChannelPatch,
  ChannelTestResult,
  DeveloperAppInput,
  Provider,
  RequestOptions,
} from '../types.js'
import { APIResource, enc } from './base.js'

export interface ChannelConnectInput extends ChannelInput {
  /** Provider id from `providers.list()` (`custom`, `telegram`, `bluesky`, ...). */
  provider: string
}

/** Connected accounts posts go to. Credentials are stored sealed and never returned. */
export class Channels extends APIResource {
  async list(options?: RequestOptions): Promise<Channel[]> {
    return (await this.http.request<{ data: Channel[] }>({ method: 'GET', path: '/v1/channels', options })).data
  }

  get(id: string, options?: RequestOptions): Promise<Channel> {
    return this.http.request({ method: 'GET', path: `/v1/channels/${enc(id)}`, options })
  }

  /**
   * Connects a channel on a token platform (`fields` carries the provider's `credential_fields`)
   * or your own automation (provider `custom`, no fields). OAuth platforms connect through
   * `oauthStart` instead (422 here). Needs `channels:manage`.
   */
  connect(input: ChannelConnectInput, options?: RequestOptions): Promise<Channel> {
    return this.http.request({ method: 'POST', path: '/v1/channels', body: input, options })
  }

  /** Renames, changes defaults, turns off or on (`status`), or reconnects with fresh `fields`. */
  update(id: string, input: ChannelPatch, options?: RequestOptions): Promise<Channel> {
    return this.http.request({ method: 'PATCH', path: `/v1/channels/${enc(id)}`, body: input, options })
  }

  /** Turns the channel off: targets on it fail without trying. */
  disable(id: string, options?: RequestOptions): Promise<Channel> {
    return this.update(id, { status: 'disabled' }, options)
  }

  enable(id: string, options?: RequestOptions): Promise<Channel> {
    return this.update(id, { status: 'connected' }, options)
  }

  /** Removes the channel; targets on it go with it. */
  async remove(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/channels/${enc(id)}`, options })
  }

  /** Checks the stored credentials against the platform; problems are reported in `{ok, error}`, not thrown. */
  test(id: string, options?: RequestOptions): Promise<ChannelTestResult> {
    return this.http.request({ method: 'POST', path: `/v1/channels/${enc(id)}/test`, options })
  }

  /**
   * Begins connecting an OAuth platform: returns the URL to open in a browser signed in to the
   * platform. `instance` is the server for platforms with many (Mastodon). The browser lands on
   * the dashboard when done.
   */
  async oauthStart(provider: string, params: { instance?: string } = {}, options?: RequestOptions): Promise<string> {
    const res = await this.http.request<{ url: string }>({
      method: 'GET',
      path: `/v1/channels/oauth/${enc(provider)}/start`,
      query: { ...params },
      options,
    })

    return res.url
  }
}

/** The platform catalog: what staqly can publish to and what each platform needs. */
export class Providers extends APIResource {
  async list(options?: RequestOptions): Promise<Provider[]> {
    return (await this.http.request<{ data: Provider[] }>({ method: 'GET', path: '/v1/providers', options })).data
  }

  /** Registers the workspace's own developer app (OAuth client) for a platform. */
  async setApp(id: string, input: Required<DeveloperAppInput>, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'PUT', path: `/v1/providers/${enc(id)}/app`, body: input, options })
  }

  /** Removes the workspace's developer app; the server's `PROVIDER_<ID>_CLIENT_ID` applies again, if set. */
  async deleteApp(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/providers/${enc(id)}/app`, options })
  }
}
