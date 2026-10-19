import type {
  Channel,
  ChannelGroup,
  ChannelGroupInput,
  ChannelLink,
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
  /** The channels with their `group_id` and `activity` (last and next post); `group` keeps one channel group. */
  async list(params: { group?: string } = {}, options?: RequestOptions): Promise<Channel[]> {
    return (
      await this.http.request<{ data: Channel[] }>({
        method: 'GET',
        path: '/v1/channels',
        query: { ...params },
        options,
      })
    ).data
  }

  /** Channel groups (Default first) with how many channels each holds. */
  async listGroups(options?: RequestOptions): Promise<ChannelGroup[]> {
    return (await this.http.request<{ data: ChannelGroup[] }>({ method: 'GET', path: '/v1/channel-groups', options }))
      .data
  }

  /** Create a channel group (`name` unique in the workspace). Needs `channels:manage`. */
  createGroup(input: ChannelGroupInput & { name: string }, options?: RequestOptions): Promise<ChannelGroup> {
    return this.http.request({ method: 'POST', path: '/v1/channel-groups', body: input, options })
  }

  /** Rename a channel group or change its colour or position. */
  updateGroup(id: string, input: ChannelGroupInput, options?: RequestOptions): Promise<ChannelGroup> {
    return this.http.request({ method: 'PATCH', path: `/v1/channel-groups/${enc(id)}`, body: input, options })
  }

  /** Delete a channel group; its channels move to Default, which can't be deleted. */
  async deleteGroup(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/channel-groups/${enc(id)}`, options })
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

  /**
   * A connect link: someone without access to Staqly opens it and connects a channel to this workspace. `url` is
   * returned only now. Empty `providers` offers every available platform; `group_id` is where channels land.
   */
  createLink(
    params: {
      note?: string
      providers?: string[]
      group_id?: string
      expires_in_hours?: number
      max_uses?: number
    } = {},
    options?: RequestOptions,
  ): Promise<ChannelLink> {
    return this.http.request({ method: 'POST', path: '/v1/channel-links', body: params, options })
  }

  /** The workspace's connect links (without their tokens), newest first. */
  async links(options?: RequestOptions): Promise<ChannelLink[]> {
    return (await this.http.request<{ data: ChannelLink[] }>({ method: 'GET', path: '/v1/channel-links', options }))
      .data
  }

  /** Stops a connect link from working; channels already connected through it stay. */
  revokeLink(linkId: string, options?: RequestOptions): Promise<void> {
    return this.http.request({ method: 'DELETE', path: `/v1/channel-links/${enc(linkId)}`, options })
  }
}
