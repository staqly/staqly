import type {
  APIKey,
  APIKeyCreateInput,
  Plan,
  RequestOptions,
  Workspace,
  WorkspaceCreated,
  WorkspaceUsage,
} from '../types.js'
import { APIResource, enc } from './base.js'

/** The workspace the client is authenticated to. */
export class WorkspaceResource extends APIResource {
  get(options?: RequestOptions): Promise<Workspace> {
    return this.http.request({ method: 'GET', path: '/v1/workspace', options })
  }

  /** Renames the workspace (needs the `admin` scope). */
  rename(name: string, options?: RequestOptions): Promise<Workspace> {
    return this.http.request({ method: 'PATCH', path: '/v1/workspace', body: { name }, options })
  }

  /** Current plan, billing period and usage (local installs report the `unlocked` plan). */
  usage(options?: RequestOptions): Promise<WorkspaceUsage> {
    return this.http.request({ method: 'GET', path: '/v1/workspace/usage', options })
  }
}

/** Cloud signup. */
export class Workspaces extends APIResource {
  /**
   * Creates a new cloud workspace on the Free plan and returns its first API
   * key (`api_key.key`) and recovery key — shown only in this response.
   * Rate limited to 5 per hour per IP. Sent without credentials.
   */
  create(input: { name?: string; setup_token?: string } = {}, options?: RequestOptions): Promise<WorkspaceCreated> {
    return this.http.request({ method: 'POST', path: '/v1/workspaces', body: input, noAuth: true, options })
  }
}

export class Plans extends APIResource {
  /** Cloud plans and their limits (`-1` = unlimited). */
  async list(options?: RequestOptions): Promise<Plan[]> {
    return (await this.http.request<{ data: Plan[] }>({ method: 'GET', path: '/v1/plans', options })).data
  }
}

/** Scoped API keys (cloud mode; needs the `admin` scope). */
export class APIKeys extends APIResource {
  async list(options?: RequestOptions): Promise<APIKey[]> {
    return (await this.http.request<{ data: APIKey[] }>({ method: 'GET', path: '/v1/api-keys', options })).data
  }

  /** Creates a key; the raw secret is in `key` and is returned only once. */
  create(input: APIKeyCreateInput, options?: RequestOptions): Promise<APIKey> {
    return this.http.request({ method: 'POST', path: '/v1/api-keys', body: input, options })
  }

  async revoke(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/api-keys/${enc(id)}`, options })
  }

  /** Revokes a key and issues a replacement with the same name and scopes. */
  rotate(id: string, options?: RequestOptions): Promise<APIKey> {
    return this.http.request({ method: 'POST', path: `/v1/api-keys/${enc(id)}/rotate`, options })
  }
}
