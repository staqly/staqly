'use client'

import { useEffect, useRef } from 'react'

/**
 * A picture of a local blob. The object URL is made in an effect and revoked in its cleanup, so StrictMode's
 * second run (and every new blob) gets a URL of its own and none outlives the picture.
 */
export function BlobImage({
  blob,
  alt,
  className,
  onLoad,
}: {
  blob: Blob
  alt: string
  className?: string
  onLoad?: React.ReactEventHandler<HTMLImageElement>
}) {
  const img = useRef<HTMLImageElement>(null)

  useEffect(() => {
    const url = URL.createObjectURL(blob)

    if (img.current) {
      img.current.src = url
    }

    return () => URL.revokeObjectURL(url)
  }, [blob])

  return <img ref={img} alt={alt} className={className} onLoad={onLoad} />
}
