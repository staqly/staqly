import type { RequestOptions, Rule, RuleInput } from "../types.js";
import { APIResource, enc } from "./base.js";

export interface RuleCreateInput extends RuleInput {
  name: string;
  action: NonNullable<RuleInput["action"]>;
}

/**
 * Approval rules run on new pending items (lowest `priority` first, first
 * match wins): auto-approve, auto-reject, set a status, expire or escalate.
 */
export class Rules extends APIResource {
  async list(options?: RequestOptions): Promise<Rule[]> {
    return (await this.http.request<{ data: Rule[] }>({ method: "GET", path: "/v1/rules", options })).data;
  }

  get(id: string, options?: RequestOptions): Promise<Rule> {
    return this.http.request({ method: "GET", path: `/v1/rules/${enc(id)}`, options });
  }

  create(input: RuleCreateInput, options?: RequestOptions): Promise<Rule> {
    return this.http.request({ method: "POST", path: "/v1/rules", body: input, options });
  }

  update(id: string, input: RuleInput, options?: RequestOptions): Promise<Rule> {
    return this.http.request({ method: "PATCH", path: `/v1/rules/${enc(id)}`, body: input, options });
  }

  enable(id: string, options?: RequestOptions): Promise<Rule> {
    return this.update(id, { enabled: true }, options);
  }

  disable(id: string, options?: RequestOptions): Promise<Rule> {
    return this.update(id, { enabled: false }, options);
  }

  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: "DELETE", path: `/v1/rules/${enc(id)}`, options });
  }
}
