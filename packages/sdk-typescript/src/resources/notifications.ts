import type {
  NotificationChannel,
  NotificationChannelInput,
  NotificationDelivery,
  NotificationTestResult,
  RequestOptions,
} from "../types.js";
import { APIResource, enc } from "./base.js";

export interface NotificationChannelCreateInput extends NotificationChannelInput {
  name: string;
  type: NonNullable<NotificationChannelInput["type"]>;
}

/**
 * Notification channels push review requests to Telegram, Slack, Discord, ntfy
 * or email (with approve/reject buttons or one-tap links). Needs the
 * `channels:manage` scope; secrets in `config` are write-only (returned masked).
 */
export class NotificationChannels extends APIResource {
  async list(options?: RequestOptions): Promise<NotificationChannel[]> {
    return (
      await this.http.request<{ data: NotificationChannel[] }>({
        method: "GET",
        path: "/v1/notification-channels",
        options,
      })
    ).data;
  }

  get(id: string, options?: RequestOptions): Promise<NotificationChannel> {
    return this.http.request({ method: "GET", path: `/v1/notification-channels/${enc(id)}`, options });
  }

  /** Creates a channel. Plan limit `notification_channels` (402) applies in cloud mode. */
  create(input: NotificationChannelCreateInput, options?: RequestOptions): Promise<NotificationChannel> {
    return this.http.request({ method: "POST", path: "/v1/notification-channels", body: input, options });
  }

  /**
   * Updates a channel. `config` keys are merged (null removes one; a masked
   * value keeps the stored secret). `enabled: true` resets the failure counter.
   */
  update(id: string, input: NotificationChannelInput, options?: RequestOptions): Promise<NotificationChannel> {
    return this.http.request({ method: "PATCH", path: `/v1/notification-channels/${enc(id)}`, body: input, options });
  }

  enable(id: string, options?: RequestOptions): Promise<NotificationChannel> {
    return this.update(id, { enabled: true }, options);
  }

  disable(id: string, options?: RequestOptions): Promise<NotificationChannel> {
    return this.update(id, { enabled: false }, options);
  }

  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: "DELETE", path: `/v1/notification-channels/${enc(id)}`, options });
  }

  /** Sends a test message now; delivery problems are reported in `{ok, error}`, not thrown. */
  test(id: string, options?: RequestOptions): Promise<NotificationTestResult> {
    return this.http.request({ method: "POST", path: `/v1/notification-channels/${enc(id)}/test`, options });
  }

  /** Recent notification outbox rows for a channel. */
  async deliveries(
    id: string,
    params: { limit?: number } = {},
    options?: RequestOptions,
  ): Promise<NotificationDelivery[]> {
    return (
      await this.http.request<{ data: NotificationDelivery[] }>({
        method: "GET",
        path: `/v1/notification-channels/${enc(id)}/deliveries`,
        query: { ...params },
        options,
      })
    ).data;
  }
}
