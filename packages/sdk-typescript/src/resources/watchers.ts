import type {
  RequestOptions,
  Watcher,
  WatcherFromPresetInput,
  WatcherInput,
  WatcherPreset,
  WatcherRunResult,
} from "../types.js";
import { APIResource, enc } from "./base.js";

export interface WatcherCreateInput extends Omit<WatcherInput, "status"> {
  name: string;
  kind: NonNullable<WatcherInput["kind"]>;
  url: string;
  /** Collection ID or name that receives new entries as items. */
  collection: string;
}

/** Watchers turn RSS feeds and web pages into items on a schedule. */
export class Watchers extends APIResource {
  async list(options?: RequestOptions): Promise<Watcher[]> {
    return (await this.http.request<{ data: Watcher[] }>({ method: "GET", path: "/v1/watchers", options })).data;
  }

  get(id: string, options?: RequestOptions): Promise<Watcher> {
    return this.http.request({ method: "GET", path: `/v1/watchers/${enc(id)}`, options });
  }

  create(input: WatcherCreateInput, options?: RequestOptions): Promise<Watcher> {
    return this.http.request({ method: "POST", path: "/v1/watchers", body: input, options });
  }

  update(id: string, input: WatcherInput, options?: RequestOptions): Promise<Watcher> {
    return this.http.request({ method: "PATCH", path: `/v1/watchers/${enc(id)}`, body: input, options });
  }

  pause(id: string, options?: RequestOptions): Promise<Watcher> {
    return this.update(id, { status: "paused" }, options);
  }

  resume(id: string, options?: RequestOptions): Promise<Watcher> {
    return this.update(id, { status: "active" }, options);
  }

  async delete(id: string, options?: RequestOptions): Promise<void> {
    await this.http.request({ method: "DELETE", path: `/v1/watchers/${enc(id)}`, options });
  }

  /** Checks the source now instead of waiting for the schedule. */
  run(id: string, options?: RequestOptions): Promise<WatcherRunResult> {
    return this.http.request({ method: "POST", path: `/v1/watchers/${enc(id)}/run`, options });
  }

  /** Catalog of ready-made sources (Hacker News, GitHub releases, Reddit, …). */
  async listPresets(options?: RequestOptions): Promise<WatcherPreset[]> {
    return (await this.http.request<{ data: WatcherPreset[] }>({ method: "GET", path: "/v1/watcher-presets", options }))
      .data;
  }

  /** Creates a watcher from a preset, filling its `params`. */
  createFromPreset(input: WatcherFromPresetInput, options?: RequestOptions): Promise<Watcher> {
    return this.http.request({ method: "POST", path: "/v1/watchers/from-preset", body: input, options });
  }
}
