import type { Delivery, DeliveryListParams, RequestOptions, Webhook, WebhookInput } from "../types.js";
import { APIResource, enc } from "./base.js";

export interface WebhookCreateInput extends Omit<WebhookInput, "rotate_secret"> {
  url: string;
}

/** Delivery history of outgoing webhooks. */
export class Deliveries extends APIResource {
  async list(params: DeliveryListParams = {}, options?: RequestOptions): Promise<Delivery[]> {
    const res = await this.http.request<{ data: Delivery[] }>({
      method: "GET",
      path: "/v1/webhook-deliveries",
      query: { ...params },
      options,
    });
    return res.data;
  }

  /** Re-queues a finished (succeeded or failed) delivery. */
  retry(id: string, options?: RequestOptions): Promise<Delivery> {
    return this.http.request({ method: "POST", path: `/v1/webhook-deliveries/${enc(id)}/retry`, options });
  }
}

/** Signed HTTP callbacks for events (at-least-once). Verify them with `verifyWebhook`. */
export class Webhooks extends APIResource {
  readonly deliveries: Deliveries;

  constructor(http: ConstructorParameters<typeof APIResource>[0]) {
    super(http);
    this.deliveries = new Deliveries(http);
  }

  async list(options?: RequestOptions): Promise<Webhook[]> {
    return (await this.http.request<{ data: Webhook[] }>({ method: "GET", path: "/v1/webhooks", options })).data;
  }

  get(id: string, options?: RequestOptions): Promise<Webhook> {
    return this.http.request({ method: "GET", path: `/v1/webhooks/${enc(id)}`, options });
  }

  /** Creates a webhook; the response includes the generated signing `secret`. */
  create(input: WebhookCreateInput, options?: RequestOptions): Promise<Webhook> {
    return this.http.request({ method: "POST", path: "/v1/webhooks", body: input, options });
  }

  update(id: string, input: WebhookInput, options?: RequestOptions): Promise<Webhook> {
    return this.http.request({ method: "PATCH", path: `/v1/webhooks/${enc(id)}`, body: input, options });
  }

  /** Issues a new signing secret (the old one stops working immediately). */
  rotateSecret(id: string, options?: RequestOptions): Promise<Webhook> {
    return this.update(id, { rotate_secret: true }, options);
  }

  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: "DELETE", path: `/v1/webhooks/${enc(id)}`, options });
  }

  /** Queues a `webhook.test` delivery to the endpoint. */
  test(id: string, options?: RequestOptions): Promise<Delivery> {
    return this.http.request({ method: "POST", path: `/v1/webhooks/${enc(id)}/test`, options });
  }
}
