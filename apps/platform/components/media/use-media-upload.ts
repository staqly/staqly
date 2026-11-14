'use client'

import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'

import { insertMedia, refreshMediaCounts } from '@/components/media/media-cache'
import { api, errorMessage, uploadMediaWithProgress, UNLIMITED, type Media } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { measureVideo } from '@/lib/media'
import { useUsage } from '@/lib/queries'

export interface UploadItem {
  key: string
  name: string
  progress: number
  error?: string
  media?: Media
  /** The file, kept to retry it. */
  file?: File
}

/** What the server takes (it checks the content again; SVG and HTML are refused there too). */
const ACCEPTED =
  /^(image\/(png|jpeg|gif|webp|avif)|video\/(mp4|webm|quicktime)|audio\/(mpeg|mp3|wav|wave|x-wav|ogg|mp4|x-m4a))/

export const MEDIA_ACCEPT =
  'image/png,image/jpeg,image/gif,image/webp,image/avif,video/mp4,video/webm,video/quicktime,audio/mpeg,audio/wav,audio/ogg,audio/mp4,.mp3,.wav,.ogg,.m4a'

/** Uploads one file; a video or audio file then gets its size and length, measured here, stored with it. */
async function send(file: File, onProgress: (share: number) => void, signal: AbortSignal) {
  const m = await uploadMediaWithProgress(file, onProgress, signal)

  if (m.mime_type.startsWith('video/') || m.mime_type.startsWith('audio/')) {
    const size = await measureVideo(file)

    if (size) {
      return api.updateMedia(m.id, size).catch(() => m)
    }
  }

  return m
}

/**
 * Uploads to the media library with progress per file: checks size and type against the plan first and says so
 * in plain words, measures a video (size and length) or audio (length) in the browser and stores that with the file.
 * Progress reaches state at most once per frame; each file that went up joins the cached library lists, and the
 * folder counts and plan usage reload once, when the last upload of a batch ends.
 */
export function useMediaUpload(onUploaded?: (m: Media) => void) {
  const qc = useQueryClient()
  const usage = useUsage()
  const [items, setItems] = useState<UploadItem[]>([])
  const maxBytes = usage.data?.plan?.limits?.max_upload_bytes ?? UNLIMITED
  const aborts = useRef(new Map<string, AbortController>())
  const shares = useRef(new Map<string, number>())
  const frame = useRef(0)
  const running = useRef({ count: 0, uploaded: false })

  useEffect(() => () => cancelAnimationFrame(frame.current), [])

  const add = useCallback(
    (files: File[], retryKey?: string) => {
      const patch = (key: string, p: Partial<UploadItem>) =>
        setItems((xs) => xs.map((x) => (x.key === key ? { ...x, ...p } : x)))
      // Progress events come faster than frames: keep the latest share per file and apply them in one update.
      const progress = (key: string) => (share: number) => {
        shares.current.set(key, share)

        if (frame.current) {
          return
        }

        frame.current = requestAnimationFrame(() => {
          const latest = new Map(shares.current)

          frame.current = 0
          shares.current.clear()
          setItems((xs) =>
            xs.map((x) => {
              const last = latest.get(x.key)

              return last === undefined || x.media || x.error ? x : { ...x, progress: last }
            }),
          )
        })
      }

      for (const file of files) {
        const key = retryKey ?? crypto.randomUUID()
        let error: string | undefined

        if (file.type && !ACCEPTED.test(file.type)) {
          error = `This file type can't be uploaded. Pictures, videos and audio can.`
        } else if (maxBytes !== UNLIMITED && maxBytes > 0 && file.size > maxBytes) {
          error = `Too big: ${formatBytes(file.size)}. Your plan takes files up to ${formatBytes(maxBytes)}.`
        }

        const item = { key, name: file.name, progress: 0, error, file }

        setItems((xs) => (retryKey ? xs.map((x) => (x.key === key ? item : x)) : [...xs, item]))

        if (error) {
          continue
        }

        const abort = new AbortController()

        aborts.current.set(key, abort)
        running.current.count++
        void send(file, progress(key), abort.signal)
          .then(
            (m) => {
              patch(key, { progress: 1, media: m })
              insertMedia(qc, [{ ...m, uses: 0 }])
              running.current.uploaded = true
              onUploaded?.(m)
            },
            (e) => {
              if (abort.signal.aborted) {
                setItems((xs) => xs.filter((x) => x.key !== key))
              } else {
                patch(key, { error: errorMessage(e) })
              }
            },
          )
          .then(() => {
            aborts.current.delete(key)
            running.current.count--

            if (running.current.count === 0 && running.current.uploaded) {
              running.current.uploaded = false
              refreshMediaCounts(qc)
            }
          })
      }
    },
    [maxBytes, onUploaded, qc],
  )

  const clearDone = useCallback(() => setItems((xs) => xs.filter((x) => !x.media && !x.error)), [])
  const dismiss = useCallback((key: string) => setItems((xs) => xs.filter((x) => x.key !== key)), [])
  const cancel = useCallback((key: string) => aborts.current.get(key)?.abort(), [])
  const retry = (key: string) => {
    const x = items.find((i) => i.key === key)

    if (x?.file) {
      add([x.file], key)
    }
  }

  return { items, add, clearDone, dismiss, cancel, retry }
}
