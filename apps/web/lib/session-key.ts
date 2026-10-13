"use client";

import { useSyncExternalStore } from "react";

/**
 * The most recently created API key, kept only in memory for this browser tab
 * so the integration snippets can be pre-filled. Never persisted, and never
 * holds the recovery key.
 */
interface RememberedKey {
  name: string;
  key: string;
}

let current: RememberedKey | null = null;
const listeners = new Set<() => void>();

export function rememberApiKey(value: RememberedKey | null) {
  current = value;
  listeners.forEach((l) => l());
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useRememberedApiKey(): RememberedKey | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
}
