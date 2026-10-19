'use client'

// The media editor and an open post: when the editor saves a picture the post uses, the post takes the edit
// (same browser, any tab). Edit on an attachment opens the editor in a new tab, so the draft stays as it is.
import { useEffect } from 'react'

import { api, type Media } from '@/lib/api'

const CHANNEL = 'staqly-media-edited'

export interface EditedMessage {
  from: string
  to: string
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
      const { from, to } = e.data ?? {}

      if (!from || !to) {
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
