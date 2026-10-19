import type {
  Agent,
  APIKey,
  APIKeyCreateInput,
  Member,
  Plan,
  RecoveryKey,
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

  /**
   * People in the workspace and their roles (cloud accounts; needs `admin`). Inviting and changing
   * roles is done by a person in the dashboard, never with an API key.
   */
  async members(options?: RequestOptions): Promise<Member[]> {
    return (await this.http.request<{ data: Member[] }>({ method: 'GET', path: '/v1/members', options })).data
  }

  /** Agents (MCP clients) and API connections seen in this workspace, most recently active first (admin). */
  async agents(options?: RequestOptions): Promise<Agent[]> {
    return (await this.http.request<{ data: Agent[] }>({ method: 'GET', path: '/v1/agents', options })).data
  }

  /** Removes an agent from the list; it comes back if it talks to the workspace again (admin). */
  async forgetAgent(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/agents/${enc(id)}`, options })
  }

  /**
   * Issues a new recovery key (shown once) and signs out every other session.
   * Dashboard sessions only: API keys get `PermissionDeniedError` (403).
   */
  rotateRecoveryKey(options?: RequestOptions): Promise<RecoveryKey> {
    return this.http.request({ method: 'POST', path: '/v1/recovery-key/rotate', options })
  }

  /**
   * Permanently deletes the workspace and all its data (cloud mode).
   * `confirmName` must equal the workspace name. Needs a dashboard session:
   * API keys get 403, local mode answers 409 (delete the data volume instead).
   */
  async delete(confirmName: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: '/v1/workspace', body: { confirm: confirmName }, options })
  }
}

/** Cloud signup. */
export class Workspaces extends APIResource {
  /**
   * Creates a new cloud workspace (on a free 7-day trial) and returns its first API
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
