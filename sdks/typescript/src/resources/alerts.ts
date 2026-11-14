import type { Alert, AlertDelivery, AlertInput, AlertTestResult, RequestOptions } from '../types.js'
import { APIResource, enc } from './base.js'

export interface AlertCreateInput extends AlertInput {
  name: string
  type: NonNullable<AlertInput['type']>
}

/**
 * Alerts push a message to Telegram, Slack, Discord, ntfy or email when a post is published or
 * fails, or a channel needs reconnecting. Needs `alerts:manage`; secrets in `config` are
 * write-only (returned masked).
 */
export class Alerts extends APIResource {
  async list(options?: RequestOptions): Promise<Alert[]> {
    return (await this.http.request<{ data: Alert[] }>({ method: 'GET', path: '/v1/alerts', options })).data
  }

  get(id: string, options?: RequestOptions): Promise<Alert> {
    return this.http.request({ method: 'GET', path: `/v1/alerts/${enc(id)}`, options })
  }

  /** Creates an alert channel. Plan limit `alerts` (402) applies in cloud mode. */
  create(input: AlertCreateInput, options?: RequestOptions): Promise<Alert> {
    return this.http.request({ method: 'POST', path: '/v1/alerts', body: input, options })
  }

  /** Updates an alert. `config` keys are merged (null removes one; a masked value keeps the stored secret). */
  update(id: string, input: AlertInput, options?: RequestOptions): Promise<Alert> {
    return this.http.request({ method: 'PATCH', path: `/v1/alerts/${enc(id)}`, body: input, options })
  }

  /** Turns the alert on (also resets the failure counter). */
  enable(id: string, options?: RequestOptions): Promise<Alert> {
    return this.update(id, { enabled: true }, options)
  }

  disable(id: string, options?: RequestOptions): Promise<Alert> {
    return this.update(id, { enabled: false }, options)
  }

  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: 'DELETE', path: `/v1/alerts/${enc(id)}`, options })
  }

  /** Sends a test message now; delivery problems are reported in `{ok, error}`, not thrown. */
  test(id: string, options?: RequestOptions): Promise<AlertTestResult> {
    return this.http.request({ method: 'POST', path: `/v1/alerts/${enc(id)}/test`, options })
  }

  /** Recent outbox rows of an alert channel. */
  async deliveries(id: string, params: { limit?: number } = {}, options?: RequestOptions): Promise<AlertDelivery[]> {
    return (
      await this.http.request<{ data: AlertDelivery[] }>({
        method: 'GET',
        path: `/v1/alerts/${enc(id)}/deliveries`,
        query: { ...params },
        options,
      })
    ).data
  }
}
