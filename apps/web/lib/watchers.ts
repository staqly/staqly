import type { Item, Plan, WatcherRun } from "./api";
import { formatInterval, pluralize } from "./format";

// Helpers for watchers: interval choices (limited by the plan), the order of
// preset categories and the "Check now" result message.

export interface IntervalOption {
  /** Go duration string sent to the API. */
  every: string;
  seconds: number;
  label: string;
}

export const INTERVAL_OPTIONS: IntervalOption[] = [
  { every: "1m", seconds: 60, label: "Every minute" },
  { every: "5m", seconds: 300, label: "Every 5 minutes" },
  { every: "15m", seconds: 900, label: "Every 15 minutes" },
  { every: "30m", seconds: 1800, label: "Every 30 minutes" },
  { every: "1h", seconds: 3600, label: "Every hour" },
  { every: "6h", seconds: 6 * 3600, label: "Every 6 hours" },
  { every: "12h", seconds: 12 * 3600, label: "Every 12 hours" },
  { every: "24h", seconds: 24 * 3600, label: "Once a day" },
  { every: "168h", seconds: 168 * 3600, label: "Once a week" },
];

/** Parse a Go duration ("30m", "1h0m0s", "1h30m") into seconds; NaN when invalid. */
export function parseDuration(s: string): number {
  const re = /(\d+(?:\.\d+)?)(h|m|s)/g;
  if (!/^(\d+(?:\.\d+)?(h|m|s))+$/.test(s.trim())) return NaN;
  let total = 0;
  for (const m of s.trim().matchAll(re)) total += Number(m[1]) * (m[2] === "h" ? 3600 : m[2] === "m" ? 60 : 1);
  return total;
}

/** Option for a number of seconds (falls back to a synthetic option for odd intervals). */
export function intervalOption(seconds: number): IntervalOption {
  return (
    INTERVAL_OPTIONS.find((o) => o.seconds === seconds) ?? {
      every: `${seconds}s`,
      seconds,
      label: `Every ${formatInterval(seconds)}`,
    }
  );
}

/** Default interval for a preset, bumped to the plan minimum when needed. */
export function defaultInterval(presetEvery: string | undefined, minSeconds: number): IntervalOption {
  const want = presetEvery ? parseDuration(presetEvery) : 3600;
  const secs = Number.isFinite(want) ? Math.max(want, minSeconds) : Math.max(3600, minSeconds);
  return INTERVAL_OPTIONS.find((o) => o.seconds >= secs) ?? intervalOption(secs);
}

/** Cheapest plan that allows checking this often (for the "Indie plan" hint). */
export function planForInterval(seconds: number, plans: Plan[] | undefined): Plan | undefined {
  return (plans ?? [])
    .toSorted((a, b) => a.price_usd_month - b.price_usd_month)
    .find((p) => p.limits.min_watcher_interval_seconds <= seconds);
}

export const CATEGORY_ORDER = ["Community", "Inspiration", "Content", "Developer", "Research", "News", "Monitoring"];

function categoryRank(c: string) {
  const i = CATEGORY_ORDER.indexOf(c);
  return i < 0 ? CATEGORY_ORDER.length : i;
}

export function sortCategories(categories: string[]) {
  return [...new Set(categories)].toSorted((a, b) => categoryRank(a) - categoryRank(b) || a.localeCompare(b));
}

/** Toast copy for POST /v1/watchers/{id}/run. */
export function runResultMessage(r: WatcherRun): {
  tone: "success" | "info" | "error";
  title: string;
  description?: string;
} {
  if (r.status === "error")
    return { tone: "error", title: "Check failed", description: r.error || "The source could not be read." };
  if (r.baseline) {
    return {
      tone: "success",
      title: "Baseline recorded — new posts will appear from now on",
      description: r.entries_seen
        ? `${pluralize(r.entries_seen, "existing entry", "existing entries")} noted.`
        : undefined,
    };
  }
  if (r.new_items) {
    return {
      tone: "success",
      title: `${pluralize(r.new_items, "new item")}`,
      description: "Waiting for review in the Inbox.",
    };
  }
  return {
    tone: "info",
    title: "No new entries",
    description: r.entries_seen ? `Checked ${pluralize(r.entries_seen, "entry", "entries")}.` : undefined,
  };
}

export interface WatcherMeta {
  id?: string;
  name?: string;
  kind?: string;
  source_url?: string;
}

/** Items created by watchers carry metadata.untrusted = true: fetched content is untrusted. */
export function isUntrusted(item: Pick<Item, "metadata"> | null | undefined): boolean {
  return (item?.metadata as Record<string, unknown> | undefined)?.untrusted === true;
}

export function watcherMeta(item: Pick<Item, "metadata">): WatcherMeta | undefined {
  const w = (item.metadata as Record<string, unknown> | undefined)?.watcher;
  return w && typeof w === "object" ? (w as WatcherMeta) : undefined;
}

/** rel for links to external content; always nofollow for untrusted items. */
export const EXTERNAL_REL = "noopener noreferrer nofollow";
