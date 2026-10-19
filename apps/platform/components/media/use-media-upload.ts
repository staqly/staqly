'use client'

import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useRef, useState } from 'react'

import { api, errorMessage, uploadMediaWithProgress, UNLIMITED, type Media } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { measureVideo } from '@/lib/media'
import { qk, useUsage } from '@/lib/queries'

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

/**
 * Uploads to the media library with progress per file: checks size and type against the plan first and says so
 * in plain words, measures a video (size and length) or audio (length) in the browser and stores that with the file.
 */
export function useMediaUpload(onUploaded?: (m: Media) => void) {
  const qc = useQueryClient()
  const usage = useUsage()
  const [items, setItems] = useState<UploadItem[]>([])
  const patch = (key: string, p: Partial<UploadItem>) =>
    setItems((xs) => xs.map((x) => (x.key === key ? { ...x, ...p } : x)))

  const maxBytes = usage.data?.plan?.limits?.max_upload_bytes ?? UNLIMITED

  const aborts = useRef(new Map<string, AbortController>())

  const add = useCallback(
    (files: File[], retryKey?: string) => {
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

        void (async () => {
          const abort = new AbortController()

          aborts.current.set(key, abort)

          try {
            let m = await uploadMediaWithProgress(file, (share) => patch(key, { progress: share }), abort.signal)

            if (m.mime_type.startsWith('video/') || m.mime_type.startsWith('audio/')) {
              const size = await measureVideo(file)

              if (size) {
                m = await api.updateMedia(m.id, size).catch(() => m)
              }
            }

            patch(key, { progress: 1, media: m })
            onUploaded?.(m)
            void qc.invalidateQueries({ queryKey: ['media'] })
            void qc.invalidateQueries({ queryKey: qk.usage })
          } catch (e) {
            if (abort.signal.aborted) {
              setItems((xs) => xs.filter((x) => x.key !== key))
            } else {
              patch(key, { error: errorMessage(e) })
            }
          } finally {
            aborts.current.delete(key)
          }
        })()
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [maxBytes, onUploaded, qc],
  )

  const clearDone = () => setItems((xs) => xs.filter((x) => !x.media && !x.error))
  const dismiss = (key: string) => setItems((xs) => xs.filter((x) => x.key !== key))
  const cancel = (key: string) => aborts.current.get(key)?.abort()
  const retry = (key: string) => {
    const x = items.find((i) => i.key === key)

    if (x?.file) {
      add([x.file], key)
    }
  }

  return { items, add, clearDone, dismiss, cancel, retry }
}
