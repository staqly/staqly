'use client'

// The design editor and an open post: when the editor saves a picture the post uses, the post takes the edit
// (same browser, any tab). Edit on an attachment opens the editor in a new tab, so the draft stays as it is.
import { useEffect } from 'react'

import { api, type Media } from '@/lib/api'

const CHANNEL = 'staqly-media-edited'

export interface EditedMessage {
  from: string
  to: string
  /** A new design made for one post (its Design button's token): only that post takes it. */
  for?: string
}

/** The editor tells open posts that a picture was saved as another file (a new file, or the replaced original). */
export function announceEdited(msg: EditedMessage) {
  if (typeof BroadcastChannel === 'undefined') {
    return
  }

  const ch = new BroadcastChannel(CHANNEL)

  // oxlint-disable-next-line unicorn/require-post-message-target-origin -- a BroadcastChannel is same-origin only
  ch.postMessage(msg)
  ch.close()
}

/** In a post: swaps an attachment for its edit when the editor saves it. */
export function useEditedMedia(setMedia: (update: (ms: Media[]) => Media[]) => void) {
  useEffect(() => {
    if (typeof BroadcastChannel === 'undefined') {
      return
    }

    const ch = new BroadcastChannel(CHANNEL)

    ch.addEventListener('message', (e: MessageEvent<EditedMessage>) => {
      const { from, to, for: token } = e.data ?? {}

      if (!from || !to || token) {
        return
      }

      void api
        .media(to)
        .then((edited) =>
          setMedia((ms) => (ms.some((m) => m.id === from) ? ms.map((m) => (m.id === from ? edited : m)) : ms)),
        )
        .catch(() => undefined)
    })

    return () => ch.close()
  }, [setMedia])
}

/** In a post: adds the design the design editor saved for this post (opened with ?for=token). */
export function useDesignedMedia(token: string, onAdd: (m: Media) => void) {
  useEffect(() => {
    if (typeof BroadcastChannel === 'undefined') {
      return
    }

    const ch = new BroadcastChannel(CHANNEL)

    ch.addEventListener('message', (e: MessageEvent<EditedMessage>) => {
      if (e.data?.for === token && e.data.to && !e.data.from) {
        void api
          .media(e.data.to)
          .then(onAdd)
          .catch(() => undefined)
      }
    })

    return () => ch.close()
  }, [token, onAdd])
}
