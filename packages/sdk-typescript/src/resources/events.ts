import type { EventListParams, EventPage, RequestOptions, Event as StaqlyEvent } from "../types.js";
import { APIResource, paginate } from "./base.js";

/** The workspace activity log (newest first). */
export class Events extends APIResource {
  list(params: EventListParams = {}, options?: RequestOptions): Promise<EventPage> {
    return this.http.request({ method: "GET", path: "/v1/events", query: { ...params }, options });
  }

  /** Iterates over every matching event across pages (`for await`). */
  iterate(params: EventListParams = {}, options?: RequestOptions): AsyncGenerator<StaqlyEvent, void, undefined> {
    return paginate((p) => this.list(p, options), params);
  }
}
