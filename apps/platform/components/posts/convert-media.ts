import { type Dispatch, type SetStateAction, useEffect, useRef } from 'react'
import { toast } from 'sonner'

import { api, type Media, type Provider } from '@/lib/api'
import { pictureTarget } from '@/lib/channel-fit'

/**
 * Draws a picture onto a canvas and saves it as a JPEG (transparency onto white) or PNG named after the original;
 * undefined when the browser can't read it (then the original goes up as it is).
 */
export async function convertPicture(file: File, to: 'image/jpeg' | 'image/png') {
  try {
    const bitmap = await createImageBitmap(file)
    const canvas = document.createElement('canvas')

    canvas.width = bitmap.width
    canvas.height = bitmap.height
    const ctx = canvas.getContext('2d')

    if (!ctx) {
      return undefined
    }

    if (to === 'image/jpeg') {
      ctx.fillStyle = '#fff'
      ctx.fillRect(0, 0, canvas.width, canvas.height)
    }

    ctx.drawImage(bitmap, 0, 0)
    bitmap.close()
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, to, 0.92))
    const name = `${file.name.replace(/\.[^.]+$/, '')}.${to === 'image/jpeg' ? 'jpg' : 'png'}`

    return blob ? new File([blob], name, { type: to }) : undefined
  } catch {
    return undefined
  }
}

/** "staqly_img.png saved as JPEG", and why: which platforms don't take the original type. */
export function toastConverted(name: string, from: string, target: NonNullable<ReturnType<typeof pictureTarget>>) {
  toast.info(`${name} saved as ${target.to === 'image/jpeg' ? 'JPEG' : 'PNG'}`, {
    description: `${target.by.join(' and ')} ${target.by.length === 1 ? "doesn't" : "don't"} take ${from.replace('image/', '').toUpperCase()} pictures.`,
  })
}

/**
 * Swaps every picture of the post a picked platform refuses (picked from the library, or there before the channel
 * was picked) for a converted copy in the library. Each file is tried once: one the browser can't read stays, and
 * the platform's error says what to do.
 */
export function useConvertedMedia(
  media: Media[],
  platforms: Provider[],
  setMedia: Dispatch<SetStateAction<Media[]>>,
  off: boolean,
) {
  const tried = useRef(new Set<string>())

  useEffect(() => {
    if (off) {
      return
    }

    for (const m of media) {
      const target = pictureTarget(platforms, m.mime_type)

      if (!target || tried.current.has(m.id)) {
        continue
      }

      tried.current.add(m.id)
      void (async () => {
        try {
          const blob = await api.mediaBlob(m.id)
          const file = await convertPicture(new File([blob], m.original_name, { type: m.mime_type }), target.to)

          if (!file) {
            return
          }

          const out = await api.uploadMedia(file, undefined, { reuse: true })

          setMedia((ms) => ms.map((x) => (x.id === m.id ? { ...out, alt_text: out.alt_text || m.alt_text } : x)))
          toastConverted(m.original_name, m.mime_type, target)
        } catch {
          // Left as it is: the platform's own error under the post says what to do.
        }
      })()
    }
  }, [media, platforms, setMedia, off])
}
